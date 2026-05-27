package dailyreport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sjzar/chatlog/internal/model"
	"github.com/sjzar/chatlog/pkg/util/dat2img"
)

type VisionClient interface {
	AnalyzeImage(ctx context.Context, prompt string, imageData []byte, mimeType string) (string, error)
}

type AnalysisClient interface {
	VisionClient
	AnalyzeText(ctx context.Context, title, data, instruction string) (string, error)
}

type AIAnalysisConfig struct {
	Vision      VisionConfig
	Summary     bool
	Concurrency int
}

type ImageResolver interface {
	ResolveImage(ctx context.Context, ref MediaRef) ([]byte, string, string, error)
}

type MediaSource interface {
	GetMedia(_type string, key string) (*model.Media, error)
}

type VisionConfig struct {
	Enabled   bool
	MaxImages int
	CachePath string
	Provider  string
	Model     string
}

type LocalImageResolver struct {
	DataDir string
	ImgKey  string
	Media   MediaSource
}

type visionCache map[string]visionCacheEntry

type visionCacheEntry struct {
	Analysis string `json:"analysis,omitempty"`
	Error    string `json:"error,omitempty"`
}

func ApplyVision(ctx context.Context, report *Report, resolver ImageResolver, client VisionClient, cfg VisionConfig) error {
	return ApplyAIAnalysis(ctx, report, resolver, imageOnlyAnalysisClient{client: client}, AIAnalysisConfig{
		Vision:      cfg,
		Summary:     false,
		Concurrency: 0,
	})
}

type imageOnlyAnalysisClient struct {
	client VisionClient
}

func (c imageOnlyAnalysisClient) AnalyzeImage(ctx context.Context, prompt string, imageData []byte, mimeType string) (string, error) {
	if c.client == nil {
		return "", fmt.Errorf("vision client is not configured")
	}
	return c.client.AnalyzeImage(ctx, prompt, imageData, mimeType)
}

func (c imageOnlyAnalysisClient) AnalyzeText(ctx context.Context, title, data, instruction string) (string, error) {
	return "", fmt.Errorf("text analyzer is not configured")
}

func ApplyAIAnalysis(ctx context.Context, report *Report, resolver ImageResolver, client AnalysisClient, cfg AIAnalysisConfig) error {
	if report == nil {
		return nil
	}
	if cfg.Vision.Enabled {
		if err := applyVisionConcurrent(ctx, report, resolver, client, cfg); err != nil {
			return err
		}
	}
	if cfg.Summary {
		if err := applyTextAnalysisConcurrent(ctx, report, client, cfg); err != nil {
			return err
		}
	}
	return nil
}

type visionTarget struct {
	chatName string
	sender   string
	content  string
	refs     []MediaRef
	set      func(string, string)
}

type visionJob struct {
	key      string
	prompt   string
	data     []byte
	mimeType string
	targets  []visionTarget
}

func applyVisionConcurrent(ctx context.Context, report *Report, resolver ImageResolver, client AnalysisClient, cfg AIAnalysisConfig) error {
	visionCfg := cfg.Vision
	if report == nil || !visionCfg.Enabled {
		return nil
	}
	if resolver == nil || client == nil {
		setAllImageErrors(report, "vision resolver or client is not configured")
		return nil
	}
	if visionCfg.MaxImages <= 0 {
		visionCfg.MaxImages = DefaultMaxImages
	}
	cache := loadVisionCache(visionCfg.CachePath)
	changed := false
	var cacheMu sync.Mutex
	used := 0
	jobsByKey := map[string]*visionJob{}
	var jobs []*visionJob
	addTarget := func(target visionTarget) {
		if len(target.refs) == 0 {
			return
		}
		ref := target.refs[0]
		data, mimeType, cacheID, err := resolver.ResolveImage(ctx, ref)
		if err != nil {
			target.set("", err.Error())
			return
		}
		key := visionCacheKey(visionCfg.Provider, visionCfg.Model, cacheID, data)
		if item, ok := cache[key]; ok && strings.TrimSpace(item.Analysis) != "" && !looksLikeMissingImageAnalysis(item.Analysis) {
			target.set(item.Analysis, "")
			return
		}
		if job, ok := jobsByKey[key]; ok {
			job.targets = append(job.targets, target)
			return
		}
		if used >= visionCfg.MaxImages {
			target.set("", "vision max_images reached")
			return
		}
		used++
		job := &visionJob{
			key:      key,
			prompt:   buildVisionPrompt(target.chatName, target.sender, target.content),
			data:     data,
			mimeType: mimeType,
			targets:  []visionTarget{target},
		}
		jobsByKey[key] = job
		jobs = append(jobs, job)
	}

	for i := range report.Mentions {
		item := &report.Mentions[i]
		addTarget(visionTarget{chatName: item.ChatName, sender: item.SenderName, content: item.Content, refs: item.MediaRefs, set: func(a, e string) {
			item.ImageAnalysis, item.ImageAnalysisError = a, e
		}})
		for j := range item.Before {
			msg := &item.Before[j]
			addTarget(visionTarget{chatName: item.ChatName, sender: msg.SenderName, content: msg.Content, refs: msg.MediaRefs, set: func(a, e string) {
				msg.ImageAnalysis, msg.ImageAnalysisError = a, e
			}})
		}
		for j := range item.After {
			msg := &item.After[j]
			addTarget(visionTarget{chatName: item.ChatName, sender: msg.SenderName, content: msg.Content, refs: msg.MediaRefs, set: func(a, e string) {
				msg.ImageAnalysis, msg.ImageAnalysisError = a, e
			}})
		}
	}
	for i := range report.PrivateUpdates {
		update := &report.PrivateUpdates[i]
		msg := &update.LatestMessage
		addTarget(visionTarget{chatName: update.ChatName, sender: msg.SenderName, content: msg.Content, refs: msg.MediaRefs, set: func(a, e string) {
			msg.ImageAnalysis, msg.ImageAnalysisError = a, e
		}})
	}

	runConcurrent(ctx, normalizeConcurrency(cfg.Concurrency), len(jobs), func(index int) {
		job := jobs[index]
		entry := visionCacheEntry{}
		analysis, err := client.AnalyzeImage(ctx, job.prompt, job.data, job.mimeType)
		if err != nil {
			entry.Error = err.Error()
		} else {
			entry.Analysis = strings.TrimSpace(analysis)
			if looksLikeMissingImageAnalysis(entry.Analysis) {
				entry.Error = "vision model did not receive image payload: " + trimRunes(entry.Analysis, 120)
				entry.Analysis = ""
			} else {
				cacheMu.Lock()
				cache[job.key] = entry
				changed = true
				cacheMu.Unlock()
			}
		}
		for _, target := range job.targets {
			target.set(entry.Analysis, entry.Error)
		}
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	report.Evidence = BuildEvidence(report.Mentions)
	if changed {
		_ = saveVisionCache(visionCfg.CachePath, cache)
	}
	return nil
}

func (r LocalImageResolver) ResolveImage(ctx context.Context, ref MediaRef) ([]byte, string, string, error) {
	_ = ctx
	if strings.TrimSpace(r.ImgKey) != "" {
		dat2img.SetAesKey(r.ImgKey)
	}
	candidates := r.candidatePaths(ref)
	for _, path := range candidates {
		data, mimeType, err := readImageFile(path)
		if err == nil {
			return data, mimeType, path, nil
		}
	}
	return nil, "", "", fmt.Errorf("image file not found for %s", ref.Key)
}

func (r LocalImageResolver) candidatePaths(ref MediaRef) []string {
	out := make([]string, 0, 4)
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if filepath.IsAbs(p) {
			out = append(out, imagePathCandidates(p)...)
			return
		}
		if strings.TrimSpace(r.DataDir) != "" {
			out = append(out, imagePathCandidates(filepath.Join(r.DataDir, p))...)
		}
	}
	add(ref.Path)
	if strings.Contains(ref.Key, "/") {
		add(ref.Key)
	}
	if r.Media != nil && strings.TrimSpace(ref.Key) != "" && !strings.Contains(ref.Key, "/") {
		if media, err := r.Media.GetMedia("image", ref.Key); err == nil && media != nil {
			add(media.Path)
		}
	}
	return uniqueStrings(out)
}

func imagePathCandidates(path string) []string {
	candidates := []string{path}
	if filepath.Ext(path) == "" {
		for _, suffix := range []string{"_t.dat", ".dat", "_h.dat", "_m.dat", "_s.dat", ".jpg", ".jpeg", ".png", ".gif"} {
			candidates = append(candidates, path+suffix)
		}
	}
	return candidates
}

func readImageFile(path string) ([]byte, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".dat" || ext == "" {
		out, convertedExt, err := dat2img.Dat2Image(data)
		if err == nil {
			data = out
			ext = "." + convertedExt
		} else if ext == ".dat" {
			return nil, "", err
		}
	}
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		switch ext {
		case ".jpg", ".jpeg":
			mimeType = "image/jpeg"
		case ".png":
			mimeType = "image/png"
		case ".gif":
			mimeType = "image/gif"
		case ".bmp":
			mimeType = "image/bmp"
		}
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return nil, "", fmt.Errorf("not an image: %s", path)
	}
	return data, mimeType, nil
}

func buildVisionPrompt(chatName, sender, content string) string {
	return fmt.Sprintf("你正在为微信日报解析一张聊天图片。群/联系人：%s。发送人：%s。消息文本：%s。\n请只基于图片可见内容回答：1. 图片中的文字；2. 界面/截图/图片类型；3. 与当前对话有关的事实或待办。不要猜测图片外信息，无法识别就说明无法识别。输出不超过120字。",
		strings.TrimSpace(chatName), strings.TrimSpace(sender), strings.TrimSpace(content))
}

func looksLikeMissingImageAnalysis(s string) bool {
	normalized := strings.ToLower(strings.TrimSpace(s))
	if normalized == "" {
		return false
	}
	for _, token := range []string{
		"未收到图片",
		"未收到或无法显示图片",
		"未提供图片",
		"未能提供图片",
		"没有看到实际图片",
		"未看到图片",
		"无法查看图片",
		"无法查看或识别",
		"未接收到实际的图片",
		"请上传图片",
		"图片未提供",
		"no image",
		"image not provided",
		"not receive image",
		"without the image",
	} {
		if strings.Contains(normalized, strings.ToLower(token)) {
			return true
		}
	}
	return false
}

func trimRunes(s string, limit int) string {
	runes := []rune(strings.TrimSpace(s))
	if limit <= 0 || len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

func visionCacheKey(provider, model, cacheID string, data []byte) string {
	sum := sha256.Sum256(data)
	parts := []string{strings.TrimSpace(provider), strings.TrimSpace(model), strings.TrimSpace(cacheID), hex.EncodeToString(sum[:])}
	return strings.Join(parts, "|")
}

func loadVisionCache(path string) visionCache {
	if strings.TrimSpace(path) == "" {
		return visionCache{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return visionCache{}
	}
	var cache visionCache
	if err := json.Unmarshal(raw, &cache); err != nil || cache == nil {
		return visionCache{}
	}
	return cache
}

func saveVisionCache(path string, cache visionCache) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func setAllImageErrors(report *Report, errText string) {
	for i := range report.Mentions {
		if len(report.Mentions[i].MediaRefs) > 0 {
			report.Mentions[i].ImageAnalysisError = errText
		}
		for j := range report.Mentions[i].Before {
			if len(report.Mentions[i].Before[j].MediaRefs) > 0 {
				report.Mentions[i].Before[j].ImageAnalysisError = errText
			}
		}
		for j := range report.Mentions[i].After {
			if len(report.Mentions[i].After[j].MediaRefs) > 0 {
				report.Mentions[i].After[j].ImageAnalysisError = errText
			}
		}
	}
	for i := range report.PrivateUpdates {
		if len(report.PrivateUpdates[i].LatestMessage.MediaRefs) > 0 {
			report.PrivateUpdates[i].LatestMessage.ImageAnalysisError = errText
		}
	}
}

func uniqueStrings(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
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
