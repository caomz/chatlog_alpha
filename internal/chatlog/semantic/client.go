package semantic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sjzar/chatlog/internal/chatlog/conf"
)

const (
	maxEmbeddingBatch         = 64
	maxEmbeddingInputTokens   = 3072
	maxRerankTotalChars       = 30000 // GLM rerank limits query+documents to 32k chars
	maxRerankDocs             = 80    // cap docs sent to reranker
	maxOllamaRerankDocs       = 20    // local generation-based rerank is much slower than hosted rerank APIs
	maxRerankDocChars         = 400   // per-doc char ceiling for reranker
	defaultMiniMaxBaseURL     = "https://api.minimax.io/v1"
	defaultMiniMaxCNBaseURL   = "https://api.minimaxi.com/v1"
	maxMiniMaxChatAttempts    = 3
	maxMiniMaxTimeoutAttempts = 5
	miniMaxRetryBaseDelay     = 1500 * time.Millisecond
	miniMaxAcquirePollDelay   = 10 * time.Millisecond
)

var ollamaScheduler = &ollamaModelScheduler{}
var miniMaxGlobalKeyPool = &miniMaxAPIKeyPool{}
var miniMaxSecretRe = regexp.MustCompile(`sk-[A-Za-z0-9_-]+`)

type ollamaModelScheduler struct {
	mu          sync.Mutex
	client      *Client
	base        string
	model       string
	phase       string
	lastTouched time.Time
}

func (s *ollamaModelScheduler) Begin(ctx context.Context, c *Client, base, model, phase string) func() {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = conf.DefaultOllamaBaseURL
	}
	model = strings.TrimSpace(model)
	phase = strings.TrimSpace(phase)
	s.mu.Lock()
	if s.model != "" && (s.base != base || s.model != model || s.phase != phase) {
		s.client.unloadOllamaModel(ctx, s.base, s.model)
	}
	s.client = c
	s.base = base
	s.model = model
	s.phase = phase
	s.lastTouched = time.Now()
	return func() {
		s.lastTouched = time.Now()
		s.mu.Unlock()
	}
}

func (s *ollamaModelScheduler) Release(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil || s.model == "" {
		return
	}
	s.client.unloadOllamaModel(ctx, s.base, s.model)
	s.client = nil
	s.base = ""
	s.model = ""
	s.phase = ""
	s.lastTouched = time.Time{}
}

type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 45 * time.Second},
	}
}

func (c *Client) Test(ctx context.Context, cfg conf.SemanticConfig) error {
	cfg = conf.NormalizeSemanticConfig(cfg)
	if _, err := c.Embed(ctx, cfg, []string{"连通性测试"}); err != nil {
		return err
	}
	if cfg.EnableRerank {
		if _, err := c.Rerank(ctx, cfg, "连通性测试", []string{"连接正常", "无关内容"}, 1); err != nil {
			ollamaScheduler.Release(context.Background())
			return err
		}
	}
	if conf.SemanticChatReady(cfg) {
		_, err := c.Chat(ctx, cfg, []ChatMessage{
			{Role: "user", Content: "请用一句话回复：连接正常。"},
		})
		ollamaScheduler.Release(context.Background())
		return err
	}
	ollamaScheduler.Release(context.Background())
	return nil
}

func (c *Client) Embed(ctx context.Context, cfg conf.SemanticConfig, inputs []string) ([][]float64, error) {
	cfg = conf.NormalizeSemanticConfig(cfg)
	inputs = sanitizeInputs(inputs)
	if len(inputs) == 0 {
		return nil, nil
	}
	if cfg.EmbeddingProvider == conf.ProviderOllama {
		return c.embedOllama(ctx, cfg, inputs)
	}
	out := make([][]float64, 0, len(inputs))
	for i := 0; i < len(inputs); i += maxEmbeddingBatch {
		end := i + maxEmbeddingBatch
		if end > len(inputs) {
			end = len(inputs)
		}
		vecs, err := c.embedBatch(ctx, cfg, inputs[i:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func (c *Client) embedBatch(ctx context.Context, cfg conf.SemanticConfig, inputs []string) ([][]float64, error) {
	cfg = conf.NormalizeSemanticConfig(cfg)
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = conf.DefaultGLMBaseURL
	}
	payload := map[string]any{
		"model": cfg.EmbeddingModel,
		"input": inputs,
	}
	if cfg.EmbeddingDimension > 0 {
		payload["dimensions"] = cfg.EmbeddingDimension
	}
	var resp struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
		Error map[string]any `json:"error"`
	}
	if err := c.doJSON(ctx, cfg.APIKey, base+"/embeddings", payload, &resp); err != nil {
		return nil, err
	}
	if len(resp.Error) > 0 {
		return nil, fmt.Errorf("embedding error: %v", resp.Error)
	}
	out := make([][]float64, len(inputs))
	for _, item := range resp.Data {
		if item.Index >= 0 && item.Index < len(out) {
			out[item.Index] = item.Embedding
		}
	}
	for i := range out {
		if len(out[i]) == 0 {
			return nil, fmt.Errorf("embedding missing vector at index %d", i)
		}
	}
	return out, nil
}

func (c *Client) embedOllama(ctx context.Context, cfg conf.SemanticConfig, inputs []string) ([][]float64, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.OllamaBaseURL), "/")
	if base == "" {
		base = conf.DefaultOllamaBaseURL
	}
	done := ollamaScheduler.Begin(ctx, c, base, cfg.EmbeddingModel, "embedding")
	defer done()
	out := make([][]float64, 0, len(inputs))
	for i := 0; i < len(inputs); i += maxEmbeddingBatch {
		end := i + maxEmbeddingBatch
		if end > len(inputs) {
			end = len(inputs)
		}
		payload := map[string]any{
			"model":      cfg.EmbeddingModel,
			"input":      inputs[i:end],
			"keep_alive": "30s",
		}
		var resp struct {
			Embeddings [][]float64 `json:"embeddings"`
			Embedding  []float64   `json:"embedding"`
			Error      string      `json:"error"`
			Data       []struct {
				Embedding []float64 `json:"embedding"`
				Index     int       `json:"index"`
			} `json:"data"`
		}
		if err := c.doJSONNoAuth(ctx, base+"/api/embed", payload, &resp); err != nil {
			return nil, err
		}
		if resp.Error != "" {
			return nil, fmt.Errorf("ollama embedding error: %s", resp.Error)
		}
		vecs, err := normalizeEmbeddingResponse(resp.Embeddings, resp.Embedding, resp.Data, len(inputs[i:end]))
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func normalizeEmbeddingResponse(embeddings [][]float64, embedding []float64, data []struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}, count int) ([][]float64, error) {
	if len(embeddings) == count {
		return embeddings, nil
	}
	if count == 1 && len(embedding) > 0 {
		return [][]float64{embedding}, nil
	}
	if len(data) > 0 {
		out := make([][]float64, count)
		for _, item := range data {
			if item.Index >= 0 && item.Index < len(out) {
				out[item.Index] = item.Embedding
			}
		}
		for i := range out {
			if len(out[i]) == 0 {
				return nil, fmt.Errorf("ollama embedding missing vector at index %d", i)
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("ollama embedding returned %d vectors for %d inputs", len(embeddings), count)
}

type RerankItem struct {
	Index int
	Score float64
}

func (c *Client) Rerank(ctx context.Context, cfg conf.SemanticConfig, query string, docs []string, topN int) ([]RerankItem, error) {
	cfg = conf.NormalizeSemanticConfig(cfg)
	query = strings.TrimSpace(query)
	docs = sanitizeInputs(docs)
	if query == "" || len(docs) == 0 {
		return nil, nil
	}
	if cfg.RerankProvider == conf.ProviderOllama {
		return c.rerankOllama(ctx, cfg, query, docs, topN)
	}
	// Enforce GLM's 32k-char limit on query+documents by capping doc count,
	// truncating each doc, and ensuring the total fits the budget.
	if len(docs) > maxRerankDocs {
		docs = docs[:maxRerankDocs]
		if topN > maxRerankDocs {
			topN = maxRerankDocs
		}
	}
	queryChars := len([]rune(query))
	perDocBudget := (maxRerankTotalChars - queryChars) / len(docs)
	if perDocBudget < 80 {
		perDocBudget = 80
	}
	if perDocBudget > maxRerankDocChars {
		perDocBudget = maxRerankDocChars
	}
	for i := range docs {
		runes := []rune(docs[i])
		if len(runes) > perDocBudget {
			docs[i] = string(runes[:perDocBudget])
		}
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = conf.DefaultGLMBaseURL
	}
	if topN <= 0 || topN > len(docs) {
		topN = len(docs)
	}
	payload := map[string]any{
		"model":            cfg.RerankModel,
		"query":            query,
		"documents":        docs,
		"top_n":            topN,
		"return_documents": false,
	}
	var resp struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
		Error map[string]any `json:"error"`
	}
	if err := c.doJSON(ctx, cfg.APIKey, base+"/rerank", payload, &resp); err != nil {
		return nil, err
	}
	if len(resp.Error) > 0 {
		return nil, fmt.Errorf("rerank error: %v", resp.Error)
	}
	out := make([]RerankItem, 0, len(resp.Results))
	for _, item := range resp.Results {
		out = append(out, RerankItem{
			Index: item.Index,
			Score: item.RelevanceScore,
		})
	}
	return out, nil
}

func (c *Client) rerankOllama(ctx context.Context, cfg conf.SemanticConfig, query string, docs []string, topN int) ([]RerankItem, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.OllamaBaseURL), "/")
	if base == "" {
		base = conf.DefaultOllamaBaseURL
	}
	done := ollamaScheduler.Begin(ctx, c, base, cfg.RerankModel, "rerank")
	defer done()
	if len(docs) > maxOllamaRerankDocs {
		docs = docs[:maxOllamaRerankDocs]
	}
	if topN <= 0 || topN > len(docs) {
		topN = len(docs)
	}
	type scored struct {
		Index int
		Score float64
	}
	scoredItems := make([]scored, 0, len(docs))
	for i, doc := range docs {
		score, err := c.ollamaRerankScore(ctx, cfg, base, query, doc)
		if err != nil {
			return nil, err
		}
		scoredItems = append(scoredItems, scored{Index: i, Score: score})
	}
	sort.SliceStable(scoredItems, func(i, j int) bool {
		return scoredItems[i].Score > scoredItems[j].Score
	})
	out := make([]RerankItem, 0, topN)
	for i := 0; i < topN && i < len(scoredItems); i++ {
		out = append(out, RerankItem{Index: scoredItems[i].Index, Score: scoredItems[i].Score})
	}
	return out, nil
}

func (c *Client) ollamaRerankScore(ctx context.Context, cfg conf.SemanticConfig, base, query, doc string) (float64, error) {
	if len([]rune(doc)) > maxRerankDocChars {
		doc = string([]rune(doc)[:maxRerankDocChars])
	}
	prompt := fmt.Sprintf("请判断文档与查询的相关性，只输出 0 到 1 之间的分数，不要解释。\n查询：%s\n文档：%s\n分数：", query, doc)
	payload := map[string]any{
		"model":      cfg.RerankModel,
		"prompt":     prompt,
		"stream":     false,
		"keep_alive": "30s",
		"options": map[string]any{
			"temperature": 0,
			"num_predict": 8,
		},
	}
	var resp struct {
		Response string `json:"response"`
		Error    string `json:"error"`
	}
	if err := c.doJSONNoAuth(ctx, base+"/api/generate", payload, &resp); err != nil {
		return 0, err
	}
	if resp.Error != "" {
		return 0, fmt.Errorf("ollama rerank error: %s", resp.Error)
	}
	score, ok := parseFirstFloat(resp.Response)
	if !ok {
		return 0, fmt.Errorf("ollama rerank returned non-score response: %s", trimSnippet([]byte(resp.Response), 120))
	}
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score, nil
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type miniMaxChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error    map[string]any `json:"error"`
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
}

type miniMaxVisionResponse struct {
	Content  string         `json:"content"`
	Error    map[string]any `json:"error"`
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
}

type miniMaxHTTPConfig struct {
	APIKeys []string
	BaseURL string
}

type miniMaxAPIKeyPool struct {
	mu        sync.Mutex
	signature string
	slots     []*miniMaxAPIKeySlot
	next      int
}

type miniMaxAPIKeySlot struct {
	key   string
	label string
	token chan struct{}
}

type miniMaxAPIKeyLease struct {
	apiKey  string
	baseURL string
	label   string
	slot    *miniMaxAPIKeySlot
	once    sync.Once
}

func (l *miniMaxAPIKeyLease) Release() {
	if l == nil || l.slot == nil {
		return
	}
	l.once.Do(func() {
		select {
		case l.slot.token <- struct{}{}:
		default:
		}
	})
}

func (p *miniMaxAPIKeyPool) Acquire(ctx context.Context, exclude map[string]bool) (*miniMaxAPIKeyLease, int, error) {
	cfg, err := loadMiniMaxHTTPConfig()
	if err != nil {
		return nil, 0, err
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = defaultMiniMaxBaseURL
	}
	for {
		lease, total, done, err := p.tryAcquire(cfg.APIKeys, base, exclude)
		if err != nil || lease != nil || done {
			return lease, total, err
		}
		timer := time.NewTimer(miniMaxAcquirePollDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, total, ctx.Err()
		case <-timer.C:
		}
	}
}

func (p *miniMaxAPIKeyPool) tryAcquire(keys []string, baseURL string, exclude map[string]bool) (*miniMaxAPIKeyLease, int, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureLocked(keys)
	total := len(p.slots)
	if total == 0 {
		return nil, 0, true, fmt.Errorf("minimax api key is not configured")
	}
	eligible := 0
	for _, slot := range p.slots {
		if exclude == nil || !exclude[slot.label] {
			eligible++
		}
	}
	if eligible == 0 {
		return nil, total, true, nil
	}
	for i := 0; i < total; i++ {
		idx := (p.next + i) % total
		slot := p.slots[idx]
		if exclude != nil && exclude[slot.label] {
			continue
		}
		select {
		case <-slot.token:
			p.next = (idx + 1) % total
			return &miniMaxAPIKeyLease{
				apiKey:  slot.key,
				baseURL: baseURL,
				label:   slot.label,
				slot:    slot,
			}, total, false, nil
		default:
		}
	}
	return nil, total, false, nil
}

func (p *miniMaxAPIKeyPool) ensureLocked(keys []string) {
	signature := strings.Join(keys, "\x00")
	if p.signature == signature && len(p.slots) == len(keys) {
		return
	}
	p.signature = signature
	p.next = 0
	p.slots = make([]*miniMaxAPIKeySlot, 0, len(keys))
	for _, key := range keys {
		slot := &miniMaxAPIKeySlot{
			key:   key,
			label: miniMaxKeyLabel(key),
			token: make(chan struct{}, 1),
		}
		slot.token <- struct{}{}
		p.slots = append(p.slots, slot)
	}
}

func (c *Client) Chat(ctx context.Context, cfg conf.SemanticConfig, messages []ChatMessage) (string, error) {
	cfg = conf.NormalizeSemanticConfig(cfg)
	clean := make([]ChatMessage, 0, len(messages))
	for _, msg := range messages {
		role := strings.TrimSpace(msg.Role)
		content := strings.TrimSpace(msg.Content)
		if role == "" || content == "" {
			continue
		}
		clean = append(clean, ChatMessage{Role: role, Content: content})
	}
	if len(clean) == 0 {
		return "", nil
	}
	if cfg.ChatProvider == conf.ProviderOllama {
		return c.chatOllama(ctx, cfg, clean)
	}
	if cfg.ChatProvider == conf.ProviderDeepSeek {
		return c.chatOpenAICompatible(ctx, cfg.DeepSeekAPIKey, cfg.DeepSeekBaseURL, cfg.ChatModel, clean, cfg.ChatThinking, cfg.ChatMaxTokens, cfg.ChatTemperature)
	}
	if cfg.ChatProvider == conf.ProviderMMX {
		return c.chatMMX(ctx, cfg, clean)
	}
	if !conf.SemanticChatReady(cfg) {
		return "", fmt.Errorf("chat model is not configured")
	}
	return c.chatOpenAICompatible(ctx, cfg.APIKey, cfg.BaseURL, cfg.ChatModel, clean, cfg.ChatThinking, cfg.ChatMaxTokens, cfg.ChatTemperature)
}

func (c *Client) AnalyzeImage(ctx context.Context, cfg conf.SemanticConfig, prompt string, imageData []byte, mimeType string) (string, error) {
	cfg = conf.NormalizeSemanticConfig(cfg)
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "请简洁描述图片中的可见文字、界面信息和与聊天上下文有关的事实；不要猜测图片外的信息。"
	}
	if len(imageData) == 0 {
		return "", fmt.Errorf("image data is empty")
	}
	if strings.TrimSpace(mimeType) == "" {
		mimeType = http.DetectContentType(imageData)
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		if ext, _ := mime.ExtensionsByType(mimeType); len(ext) == 0 {
			return "", fmt.Errorf("unsupported image mime type: %s", mimeType)
		}
	}
	content := []map[string]any{
		{"type": "text", "text": prompt},
		{"type": "image_url", "image_url": map[string]any{
			"url": "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(imageData),
		}},
	}
	messages := []map[string]any{{"role": "user", "content": content}}
	switch cfg.ChatProvider {
	case conf.ProviderMMX:
		return c.analyzeMiniMaxImage(ctx, prompt, imageData, mimeType)
	case conf.ProviderDeepSeek:
		return c.chatOpenAICompatibleRaw(ctx, cfg.DeepSeekAPIKey, cfg.DeepSeekBaseURL, cfg.ChatModel, messages, cfg.ChatThinking, cfg.ChatMaxTokens, cfg.ChatTemperature)
	case conf.ProviderOllama:
		return c.chatOllamaVision(ctx, cfg, prompt, imageData)
	default:
		if !conf.SemanticChatReady(cfg) {
			return "", fmt.Errorf("chat model is not configured")
		}
		return c.chatOpenAICompatibleRaw(ctx, cfg.APIKey, cfg.BaseURL, cfg.ChatModel, messages, cfg.ChatThinking, cfg.ChatMaxTokens, cfg.ChatTemperature)
	}
}

func (c *Client) chatOpenAICompatible(ctx context.Context, apiKey, baseURL, model string, messages []ChatMessage, thinking bool, maxTokens int, temperature float64) (string, error) {
	raw := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		raw = append(raw, map[string]any{"role": msg.Role, "content": msg.Content})
	}
	return c.chatOpenAICompatibleRaw(ctx, apiKey, baseURL, model, raw, thinking, maxTokens, temperature)
}

func (c *Client) chatOpenAICompatibleRaw(ctx context.Context, apiKey, baseURL, model string, messages []map[string]any, thinking bool, maxTokens int, temperature float64) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = conf.DefaultGLMBaseURL
	}
	thinkingType := "disabled"
	if thinking {
		thinkingType = "enabled"
	}
	payload := map[string]any{
		"model":       model,
		"messages":    messages,
		"thinking":    map[string]any{"type": thinkingType},
		"max_tokens":  maxTokens,
		"temperature": temperature,
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error map[string]any `json:"error"`
	}
	if err := c.doJSON(ctx, apiKey, base+"/chat/completions", payload, &resp); err != nil {
		return "", err
	}
	if len(resp.Error) > 0 {
		return "", fmt.Errorf("chat error: %v", resp.Error)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("chat returned empty choices")
	}
	answer := strings.TrimSpace(resp.Choices[0].Message.Content)
	if answer == "" {
		return "", fmt.Errorf("chat returned empty content")
	}
	return answer, nil
}

func (c *Client) chatMMX(ctx context.Context, cfg conf.SemanticConfig, messages []ChatMessage) (string, error) {
	raw := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		raw = append(raw, map[string]any{"role": msg.Role, "content": msg.Content})
	}
	return c.chatMMXRaw(ctx, cfg, raw)
}

func (c *Client) chatMMXRaw(ctx context.Context, cfg conf.SemanticConfig, messages []map[string]any) (string, error) {
	payload := map[string]any{
		"model":       cfg.ChatModel,
		"messages":    messages,
		"stream":      false,
		"max_tokens":  cfg.ChatMaxTokens,
		"temperature": cfg.ChatTemperature,
	}
	var lastErr error
	attempted := 0
	for round := 1; round <= miniMaxMaxAttemptsForError(lastErr); round++ {
		excluded := map[string]bool{}
		for {
			lease, keyCount, err := miniMaxGlobalKeyPool.Acquire(ctx, excluded)
			if err != nil {
				return "", fmt.Errorf("minimax chat failed before request: %w", sanitizeMiniMaxError(err, nil))
			}
			if lease == nil {
				break
			}
			attempted++
			answer, err := c.doMiniMaxChatWithLease(ctx, lease, payload)
			if err == nil {
				return answer, nil
			}
			lastErr = err
			excluded[lease.label] = true
			if !shouldSwitchMiniMaxKey(err) {
				return "", fmt.Errorf("minimax chat failed with key=%s: %w", lease.label, err)
			}
			if len(excluded) >= keyCount {
				break
			}
		}
		if round < miniMaxMaxAttemptsForError(lastErr) {
			if waitErr := sleepWithContext(ctx, miniMaxRetryDelay(lastErr, round)); waitErr != nil {
				return "", fmt.Errorf("minimax chat failed: %w", waitErr)
			}
		}
	}
	maxAttempts := miniMaxMaxAttemptsForError(lastErr)
	if lastErr == nil {
		return "", fmt.Errorf("minimax chat failed after %d round(s), tried %d key attempt(s)", maxAttempts, attempted)
	}
	return "", fmt.Errorf("minimax chat failed after %d round(s), tried %d key attempt(s), last_error=%w", maxAttempts, attempted, lastErr)
}

func (c *Client) doMiniMaxChatWithLease(ctx context.Context, lease *miniMaxAPIKeyLease, payload map[string]any) (string, error) {
	defer lease.Release()
	base := strings.TrimRight(strings.TrimSpace(lease.baseURL), "/")
	if base == "" {
		base = defaultMiniMaxBaseURL
	}
	var resp miniMaxChatResponse
	if err := c.doJSONRequest(ctx, lease.apiKey, base+"/chat/completions", payload, &resp); err != nil {
		return "", fmt.Errorf("key=%s: %w", lease.label, sanitizeMiniMaxError(err, []string{lease.apiKey}))
	}
	answer, err := parseMiniMaxChatResponse(resp)
	if err != nil {
		return "", fmt.Errorf("key=%s: %w", lease.label, sanitizeMiniMaxError(err, []string{lease.apiKey}))
	}
	return answer, nil
}

func (c *Client) analyzeMiniMaxImage(ctx context.Context, prompt string, imageData []byte, mimeType string) (string, error) {
	mimeType = strings.TrimSpace(mimeType)
	if mimeType == "" {
		mimeType = http.DetectContentType(imageData)
	}
	payload := map[string]any{
		"prompt":    prompt,
		"image_url": "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(imageData),
	}
	var lastErr error
	attempted := 0
	for round := 1; round <= miniMaxMaxAttemptsForError(lastErr); round++ {
		excluded := map[string]bool{}
		for {
			lease, keyCount, err := miniMaxGlobalKeyPool.Acquire(ctx, excluded)
			if err != nil {
				return "", fmt.Errorf("minimax vision failed before request: %w", sanitizeMiniMaxError(err, nil))
			}
			if lease == nil {
				break
			}
			attempted++
			answer, err := c.doMiniMaxVisionWithLease(ctx, lease, payload)
			if err == nil {
				return answer, nil
			}
			lastErr = err
			excluded[lease.label] = true
			if !shouldSwitchMiniMaxKey(err) {
				return "", fmt.Errorf("minimax vision failed with key=%s: %w", lease.label, err)
			}
			if len(excluded) >= keyCount {
				break
			}
		}
		if round < miniMaxMaxAttemptsForError(lastErr) {
			if waitErr := sleepWithContext(ctx, miniMaxRetryDelay(lastErr, round)); waitErr != nil {
				return "", fmt.Errorf("minimax vision failed: %w", waitErr)
			}
		}
	}
	maxAttempts := miniMaxMaxAttemptsForError(lastErr)
	if lastErr == nil {
		return "", fmt.Errorf("minimax vision failed after %d round(s), tried %d key attempt(s)", maxAttempts, attempted)
	}
	return "", fmt.Errorf("minimax vision failed after %d round(s), tried %d key attempt(s), last_error=%w", maxAttempts, attempted, lastErr)
}

func (c *Client) doMiniMaxVisionWithLease(ctx context.Context, lease *miniMaxAPIKeyLease, payload map[string]any) (string, error) {
	defer lease.Release()
	var resp miniMaxVisionResponse
	if err := c.doJSONRequest(ctx, lease.apiKey, miniMaxVisionURL(lease.baseURL), payload, &resp); err != nil {
		return "", fmt.Errorf("key=%s: %w", lease.label, sanitizeMiniMaxError(err, []string{lease.apiKey}))
	}
	answer, err := parseMiniMaxVisionResponse(resp)
	if err != nil {
		return "", fmt.Errorf("key=%s: %w", lease.label, sanitizeMiniMaxError(err, []string{lease.apiKey}))
	}
	return answer, nil
}

func miniMaxVisionURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = defaultMiniMaxBaseURL
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/coding_plan/vlm"
	}
	return base + "/v1/coding_plan/vlm"
}

func shouldSwitchMiniMaxKey(err error) bool {
	if err == nil {
		return false
	}
	if isNonRetryableMiniMaxError(err) {
		return false
	}
	return isMiniMaxAuthError(err) || isRetryableMiniMaxError(err)
}

func isMiniMaxAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "model http 401") || strings.Contains(msg, "model http 403")
}

func isNonRetryableMiniMaxError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, token := range []string{
		"model http 400",
		"model http 404",
		"model http 422",
		"max tokens exceeded",
		"model not found",
		"invalid request",
		"decode model response failed",
		"returned empty choices",
		"returned empty content",
	} {
		if strings.Contains(msg, token) {
			return true
		}
	}
	return false
}

func sanitizeMiniMaxError(err error, keys []string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			msg = strings.ReplaceAll(msg, key, miniMaxKeyLabel(key))
		}
	}
	msg = miniMaxSecretRe.ReplaceAllString(msg, "sk-***")
	return errors.New(msg)
}

func miniMaxKeyLabel(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "***"
	}
	r := []rune(key)
	if len(r) <= 4 {
		return "***"
	}
	return "***" + string(r[len(r)-4:])
}

func uniqueNonEmptyStrings(items []string) []string {
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func splitMiniMaxAPIKeys(raw string) []string {
	return uniqueNonEmptyStrings(strings.Split(raw, ","))
}

func miniMaxEnvAPIKeys() []string {
	if keys := splitMiniMaxAPIKeys(os.Getenv("MINIMAX_API_KEYS")); len(keys) > 0 {
		return keys
	}
	var keys []string
	for _, prefix := range []string{"MINIMAX", "MINMAX"} {
		keys = append(keys, os.Getenv(prefix+"_API_KEY"))
		for i := 2; i <= 20; i++ {
			keys = append(keys, os.Getenv(fmt.Sprintf("%s%d_API_KEY", prefix, i)))
		}
	}
	return uniqueNonEmptyStrings(keys)
}

func miniMaxEnvBaseURL() string {
	for _, name := range []string{"MINIMAX_BASE_URL", "MINMAX_BASE_URL"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

func loadMiniMaxHTTPConfig() (miniMaxHTTPConfig, error) {
	if keys := miniMaxEnvAPIKeys(); len(keys) > 0 {
		baseURL := miniMaxEnvBaseURL()
		if baseURL == "" {
			baseURL = defaultMiniMaxCNBaseURL
		}
		return miniMaxHTTPConfig{APIKeys: keys, BaseURL: baseURL}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return miniMaxHTTPConfig{}, err
	}
	path := filepath.Join(home, ".mmx", "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return miniMaxHTTPConfig{}, fmt.Errorf("minimax config not found: set MINIMAX_API_KEYS, run mmx auth login, or create %s", path)
	}
	var cfg struct {
		APIKey  string `json:"api_key"`
		BaseURL string `json:"base_url"`
		Region  string `json:"region"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return miniMaxHTTPConfig{}, fmt.Errorf("decode minimax config failed: %w", err)
	}
	apiKeys := uniqueNonEmptyStrings([]string{cfg.APIKey})
	if len(apiKeys) == 0 {
		return miniMaxHTTPConfig{}, fmt.Errorf("minimax api key is not configured in %s", path)
	}
	baseURL := miniMaxEnvBaseURL()
	if baseURL == "" {
		baseURL = strings.TrimSpace(cfg.BaseURL)
	}
	if baseURL == "" {
		switch strings.ToLower(strings.TrimSpace(cfg.Region)) {
		case "cn":
			baseURL = defaultMiniMaxCNBaseURL
		default:
			baseURL = defaultMiniMaxBaseURL
		}
	}
	return miniMaxHTTPConfig{APIKeys: apiKeys, BaseURL: baseURL}, nil
}

func MiniMaxConfiguredKeyCount() int {
	cfg, err := loadMiniMaxHTTPConfig()
	if err != nil {
		return 1
	}
	if len(cfg.APIKeys) == 0 {
		return 1
	}
	return len(cfg.APIKeys)
}

func (c *Client) chatOllamaVision(ctx context.Context, cfg conf.SemanticConfig, prompt string, imageData []byte) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.OllamaBaseURL), "/")
	if base == "" {
		base = conf.DefaultOllamaBaseURL
	}
	payload := map[string]any{
		"model":  cfg.ChatModel,
		"prompt": prompt,
		"images": []string{base64.StdEncoding.EncodeToString(imageData)},
		"stream": false,
	}
	var resp struct {
		Response string `json:"response"`
		Error    string `json:"error"`
	}
	if err := c.doJSONNoAuth(ctx, base+"/api/generate", payload, &resp); err != nil {
		return "", err
	}
	if resp.Error != "" {
		return "", fmt.Errorf("ollama vision error: %s", resp.Error)
	}
	answer := strings.TrimSpace(resp.Response)
	if answer == "" {
		return "", fmt.Errorf("ollama vision returned empty content")
	}
	return answer, nil
}

func parseMiniMaxChatResponse(resp miniMaxChatResponse) (string, error) {
	if len(resp.Error) > 0 {
		return "", fmt.Errorf("minimax chat error: %v", resp.Error)
	}
	if resp.BaseResp.StatusCode != 0 {
		return "", fmt.Errorf("minimax chat error: %s", strings.TrimSpace(resp.BaseResp.StatusMsg))
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("minimax chat returned empty choices")
	}
	answer := strings.TrimSpace(resp.Choices[0].Message.Content)
	if answer == "" {
		return "", fmt.Errorf("minimax chat returned empty content")
	}
	return stripReasoningBlocks(answer), nil
}

func parseMiniMaxVisionResponse(resp miniMaxVisionResponse) (string, error) {
	if len(resp.Error) > 0 {
		return "", fmt.Errorf("minimax vision error: %v", resp.Error)
	}
	if resp.BaseResp.StatusCode != 0 {
		return "", fmt.Errorf("minimax vision error: %s", strings.TrimSpace(resp.BaseResp.StatusMsg))
	}
	answer := strings.TrimSpace(resp.Content)
	if answer == "" {
		return "", fmt.Errorf("minimax vision returned empty content")
	}
	return stripReasoningBlocks(answer), nil
}

func stripReasoningBlocks(s string) string {
	s = thinkBlockRe.ReplaceAllString(s, "")
	s = thoughtBlockRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func isRetryableMiniMaxError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, token := range []string{
		"context deadline exceeded",
		"client.timeout",
		"timeout",
		"temporary",
		"connection reset",
		"connection refused",
		"eof",
		"model http 408",
		"model http 409",
		"model http 425",
		"model http 429",
		"model http 529",
		"model http 500",
		"model http 502",
		"model http 503",
		"model http 504",
		"network request failed",
		"overloaded_error",
		"server is overloaded",
	} {
		if strings.Contains(msg, token) {
			return true
		}
	}
	return false
}

func miniMaxMaxAttemptsForError(err error) int {
	if isMiniMaxTimeoutError(err) {
		return maxMiniMaxTimeoutAttempts
	}
	return maxMiniMaxChatAttempts
}

func isMiniMaxTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, token := range []string{
		"context deadline exceeded",
		"client.timeout",
		"timeout",
		"deadline exceeded",
	} {
		if strings.Contains(msg, token) {
			return true
		}
	}
	return false
}

func miniMaxRetryDelay(err error, attempt int) time.Duration {
	msg := ""
	if err != nil {
		msg = strings.ToLower(err.Error())
	}
	if wait := parseResetAt(msg); wait > 0 {
		return wait
	}
	if attempt < 1 {
		attempt = 1
	}
	return time.Duration(attempt) * miniMaxRetryBaseDelay
}

func parseResetAt(msg string) time.Duration {
	m := miniMaxResetAtRe.FindStringSubmatch(msg)
	if len(m) != 2 {
		return 0
	}
	ts, err := time.Parse(time.RFC3339, m[1])
	if err != nil {
		return 0
	}
	wait := time.Until(ts.Add(10 * time.Minute))
	if wait <= 0 {
		return 0
	}
	return wait
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) chatOllama(ctx context.Context, cfg conf.SemanticConfig, messages []ChatMessage) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.OllamaBaseURL), "/")
	if base == "" {
		base = conf.DefaultOllamaBaseURL
	}
	done := ollamaScheduler.Begin(ctx, c, base, cfg.ChatModel, "chat")
	defer done()
	payload := map[string]any{
		"model":      cfg.ChatModel,
		"messages":   messages,
		"stream":     false,
		"keep_alive": "30s",
		"options": map[string]any{
			"temperature": cfg.ChatTemperature,
			"num_predict": cfg.ChatMaxTokens,
		},
	}
	var resp struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Response string `json:"response"`
		Error    string `json:"error"`
	}
	if err := c.doJSONNoAuth(ctx, base+"/api/chat", payload, &resp); err != nil {
		return "", err
	}
	if resp.Error != "" {
		return "", fmt.Errorf("ollama chat error: %s", resp.Error)
	}
	answer := strings.TrimSpace(resp.Message.Content)
	if answer == "" {
		answer = strings.TrimSpace(resp.Response)
	}
	if answer == "" {
		return "", fmt.Errorf("ollama chat returned empty content")
	}
	return answer, nil
}

func (c *Client) ChatStream(ctx context.Context, cfg conf.SemanticConfig, messages []ChatMessage, onDelta func(string) error) error {
	cfg = conf.NormalizeSemanticConfig(cfg)
	clean := make([]ChatMessage, 0, len(messages))
	for _, msg := range messages {
		role := strings.TrimSpace(msg.Role)
		content := strings.TrimSpace(msg.Content)
		if role == "" || content == "" {
			continue
		}
		clean = append(clean, ChatMessage{Role: role, Content: content})
	}
	if len(clean) == 0 {
		return nil
	}
	if cfg.ChatProvider == conf.ProviderOllama {
		answer, err := c.chatOllama(ctx, cfg, clean)
		if err != nil {
			return err
		}
		if onDelta != nil {
			return onDelta(answer)
		}
		return nil
	}
	if cfg.ChatProvider == conf.ProviderMMX {
		answer, err := c.chatMMX(ctx, cfg, clean)
		if err != nil {
			return err
		}
		if onDelta != nil {
			return onDelta(answer)
		}
		return nil
	}
	if !conf.SemanticChatReady(cfg) {
		return fmt.Errorf("chat model is not configured")
	}
	apiKey := cfg.APIKey
	base := cfg.BaseURL
	if cfg.ChatProvider == conf.ProviderDeepSeek {
		apiKey = cfg.DeepSeekAPIKey
		base = cfg.DeepSeekBaseURL
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = conf.DefaultGLMBaseURL
	}
	thinkingType := "disabled"
	if cfg.ChatThinking {
		thinkingType = "enabled"
	}
	payload := map[string]any{
		"model":       cfg.ChatModel,
		"messages":    clean,
		"thinking":    map[string]any{"type": thinkingType},
		"stream":      true,
		"max_tokens":  cfg.ChatMaxTokens,
		"temperature": cfg.ChatTemperature,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
		return fmt.Errorf("chat http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		delta, err := parseChatStreamDelta([]byte(data))
		if err != nil {
			return err
		}
		if delta == "" {
			continue
		}
		if onDelta != nil {
			if err := onDelta(delta); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func parseChatStreamDelta(raw []byte) (string, error) {
	var payload struct {
		Choices []struct {
			Delta struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"delta"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("decode glm stream failed: %w; response_snippet=%q", err, trimSnippet(raw, 260))
	}
	if len(payload.Error) > 0 {
		return "", fmt.Errorf("glm chat stream error: %v", payload.Error)
	}
	if len(payload.Choices) == 0 {
		return "", nil
	}
	if payload.Choices[0].Delta.Content != "" {
		return payload.Choices[0].Delta.Content, nil
	}
	return payload.Choices[0].Message.Content, nil
}

func (c *Client) doJSON(ctx context.Context, apiKey, url string, reqBody any, out any) error {
	if strings.TrimSpace(apiKey) == "" {
		return fmt.Errorf("glm api key is empty")
	}
	return c.doJSONRequest(ctx, apiKey, url, reqBody, out)
}

func (c *Client) doJSONNoAuth(ctx context.Context, url string, reqBody any, out any) error {
	return c.doJSONRequest(ctx, "", url, reqBody, out)
}

func (c *Client) unloadOllamaModel(ctx context.Context, base, model string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	if base = strings.TrimRight(strings.TrimSpace(base), "/"); base == "" {
		base = conf.DefaultOllamaBaseURL
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	payload := map[string]any{
		"model":      model,
		"prompt":     "",
		"stream":     false,
		"keep_alive": 0,
	}
	var out map[string]any
	if err := c.doJSONNoAuth(ctx, base+"/api/generate", payload, &out); err == nil {
		return
	}
	embedPayload := map[string]any{
		"model":      model,
		"input":      "",
		"keep_alive": 0,
	}
	_ = c.doJSONNoAuth(ctx, base+"/api/embed", embedPayload, &out)
}

func (c *Client) doJSONRequest(ctx context.Context, apiKey, url string, reqBody any, out any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) // utf-8 BOM guard
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("model http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		// Some upstream gateways occasionally return malformed numeric literals like "0. 123".
		// Repair this specific pattern and retry once.
		if strings.Contains(err.Error(), "after decimal point in numeric literal") {
			if fixed := fixBrokenJSONNumbers(raw); len(fixed) > 0 && !bytes.Equal(fixed, raw) {
				if err2 := json.Unmarshal(fixed, out); err2 == nil {
					return nil
				}
			}
		}
		return fmt.Errorf("decode model response failed: %w; response_snippet=%q", err, trimSnippet(raw, 260))
	}
	return nil
}

func sanitizeInputs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, truncateApproxTokens(item, maxEmbeddingInputTokens))
	}
	return out
}

func truncateApproxTokens(s string, maxTokens int) string {
	if maxTokens <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxTokens {
		return s
	}
	return string(runes[:maxTokens])
}

var (
	brokenNumRe      = regexp.MustCompile(`([0-9])\.\s+([0-9])`)
	thinkBlockRe     = regexp.MustCompile(`(?is)<think\b[^>]*>.*?</think>`)
	thoughtBlockRe   = regexp.MustCompile(`(?is)<thought\b[^>]*>.*?</thought>`)
	miniMaxResetAtRe = regexp.MustCompile(`(?i)resets at\s+([0-9T:+-]{5,})`)
)

func fixBrokenJSONNumbers(in []byte) []byte {
	// Best-effort repair only; keeps behavior unchanged for valid JSON.
	s := string(in)
	for i := 0; i < 8; i++ {
		next := brokenNumRe.ReplaceAllString(s, `$1.$2`)
		if next == s {
			break
		}
		s = next
	}
	return []byte(s)
}

func trimSnippet(in []byte, n int) string {
	s := strings.TrimSpace(string(in))
	if n <= 0 || len([]rune(s)) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "..."
}

func parseFirstFloat(raw string) (float64, bool) {
	m := regexp.MustCompile(`[-+]?(?:\d+(?:\.\d*)?|\.\d+)`).FindString(strings.TrimSpace(raw))
	if m == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(m, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
