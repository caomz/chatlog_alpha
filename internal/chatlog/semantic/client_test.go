package semantic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sjzar/chatlog/internal/chatlog/conf"
)

func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func TestAcquireMiniMaxLeaseContextHonorsParentDeadline(t *testing.T) {
	baseCtx, baseCancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer baseCancel()

	acquireCtx, acquireCancel := acquireMiniMaxLeaseContext(baseCtx, miniMaxAcquireTimeout)
	defer acquireCancel()

	baseDeadline, _ := baseCtx.Deadline()
	acquireDeadline, ok := acquireCtx.Deadline()
	if !ok {
		t.Fatal("acquire context missing deadline")
	}
	if acquireDeadline.After(baseDeadline) {
		t.Fatalf("acquire context deadline (%s) should not exceed parent deadline (%s)", acquireDeadline, baseDeadline)
	}
}

func TestParseMiniMaxChatResponse(t *testing.T) {
	var resp miniMaxChatResponse
	resp.Choices = append(resp.Choices, struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}{})
	resp.Choices[0].Message.Content = "  连接正常  "

	got, err := parseMiniMaxChatResponse(resp)
	if err != nil {
		t.Fatalf("parseMiniMaxChatResponse returned error: %v", err)
	}
	if got != "连接正常" {
		t.Fatalf("parseMiniMaxChatResponse = %q", got)
	}
}

func TestParseMiniMaxChatResponseStripsReasoning(t *testing.T) {
	var resp miniMaxChatResponse
	resp.Choices = append(resp.Choices, struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}{})
	resp.Choices[0].Message.Content = "<think>分析过程</think>\n{\"entities\":[],\"relations\":[],\"events\":[],\"facts\":[]}"

	got, err := parseMiniMaxChatResponse(resp)
	if err != nil {
		t.Fatalf("parseMiniMaxChatResponse returned error: %v", err)
	}
	if got != "{\"entities\":[],\"relations\":[],\"events\":[],\"facts\":[]}" {
		t.Fatalf("parseMiniMaxChatResponse = %q", got)
	}
}

func TestIsRetryableMiniMaxError(t *testing.T) {
	cases := []struct {
		name    string
		errText string
		expect  bool
	}{
		{name: "429", errText: "minimax chat failed: model http 429: too many requests", expect: true},
		{name: "529", errText: "minimax chat failed: model http 529: overloaded_error", expect: true},
		{name: "overloaded_error", errText: "minimax chat error: map[error:map[message:当前时段请求拥挤 type:overloaded_error] type:error]", expect: true},
		{name: "timeout", errText: "network timeout", expect: true},
		{name: "non retryable", errText: "max tokens exceeded", expect: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryableMiniMaxError(errors.New(tc.errText)); got != tc.expect {
				t.Fatalf("isRetryableMiniMaxError(%q) = %v, want %v", tc.errText, got, tc.expect)
			}
		})
	}
}

func TestMiniMaxRetryDelay(t *testing.T) {
	delay := miniMaxRetryDelay(errors.New("model http 529: overloaded_error"), 2)
	if delay < 1500*time.Millisecond || delay > 3*time.Second {
		t.Fatalf("unexpected delay for retryable error: %v", delay)
	}
}

func TestMiniMaxMaxAttemptsForTimeout(t *testing.T) {
	if got := miniMaxMaxAttemptsForError(errors.New("Post https://api.minimaxi.com/v1/chat/completions: context deadline exceeded (Client.Timeout exceeded while awaiting headers)")); got != maxMiniMaxTimeoutAttempts {
		t.Fatalf("timeout attempts = %d, want %d", got, maxMiniMaxTimeoutAttempts)
	}
	if got := miniMaxMaxAttemptsForError(errors.New("model http 529: overloaded_error")); got != maxMiniMaxChatAttempts {
		t.Fatalf("non-timeout attempts = %d, want %d", got, maxMiniMaxChatAttempts)
	}
}

func TestLoadMiniMaxHTTPConfigFromEnvKeys(t *testing.T) {
	clearMiniMaxEnv(t)
	t.Setenv("MINIMAX_API_KEYS", " sk-cp-one1111, ,sk-cp-two2222,sk-cp-one1111 ")
	t.Setenv("MINIMAX_BASE_URL", "https://example.test/v1")

	cfg, err := loadMiniMaxHTTPConfig()
	if err != nil {
		t.Fatalf("loadMiniMaxHTTPConfig returned error: %v", err)
	}
	if got, want := strings.Join(cfg.APIKeys, ","), "sk-cp-one1111,sk-cp-two2222"; got != want {
		t.Fatalf("APIKeys = %q, want %q", got, want)
	}
	if cfg.BaseURL != "https://example.test/v1" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
}

func TestMiniMaxConfiguredKeyCountFromEnv(t *testing.T) {
	clearMiniMaxEnv(t)
	t.Setenv("MINIMAX_API_KEYS", " sk-cp-one1111,sk-cp-two2222,sk-cp-one1111 ")
	t.Setenv("MINIMAX_BASE_URL", "https://example.test/v1")
	if got := MiniMaxConfiguredKeyCount(); got != 2 {
		t.Fatalf("MiniMaxConfiguredKeyCount = %d, want 2", got)
	}
}

func TestLoadMiniMaxHTTPConfigFromLegacyMinmaxEnv(t *testing.T) {
	clearMiniMaxEnv(t)
	t.Setenv("MINMAX_API_KEY", "sk-cp-legacy1111")
	t.Setenv("MINMAX2_API_KEY", "sk-cp-legacy2222")

	cfg, err := loadMiniMaxHTTPConfig()
	if err != nil {
		t.Fatalf("loadMiniMaxHTTPConfig returned error: %v", err)
	}
	if got, want := strings.Join(cfg.APIKeys, ","), "sk-cp-legacy1111,sk-cp-legacy2222"; got != want {
		t.Fatalf("APIKeys = %q, want %q", got, want)
	}
	if cfg.BaseURL != defaultMiniMaxCNBaseURL {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, defaultMiniMaxCNBaseURL)
	}
}

func TestLoadMiniMaxHTTPConfigEnvBaseURLOverridesConfig(t *testing.T) {
	clearMiniMaxEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MINIMAX_BASE_URL", "https://env-base.test/v1")
	mmxDir := filepath.Join(home, ".mmx")
	if err := os.MkdirAll(mmxDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cfgPath := filepath.Join(mmxDir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"api_key":"test-api-key","base_url":"https://file-base.test/v1"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := loadMiniMaxHTTPConfig()
	if err != nil {
		t.Fatalf("loadMiniMaxHTTPConfig returned error: %v", err)
	}
	if got, want := strings.Join(cfg.APIKeys, ","), "test-api-key"; got != want {
		t.Fatalf("APIKeys = %q, want %q", got, want)
	}
	if cfg.BaseURL != "https://env-base.test/v1" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
}

func TestMiniMaxKeyPoolLimitsOneConcurrentRequestPerKey(t *testing.T) {
	clearMiniMaxEnv(t)
	t.Setenv("MINIMAX_API_KEYS", "sk-cp-one1111,sk-cp-two2222")
	t.Setenv("MINIMAX_BASE_URL", "https://example.test/v1")
	pool := &miniMaxAPIKeyPool{}
	ctx := context.Background()

	lease1, total, err := pool.Acquire(ctx, nil)
	if err != nil {
		t.Fatalf("Acquire lease1: %v", err)
	}
	defer lease1.Release()
	lease2, total, err := pool.Acquire(ctx, nil)
	if err != nil {
		t.Fatalf("Acquire lease2: %v", err)
	}
	defer lease2.Release()
	if total != 2 {
		t.Fatalf("total keys = %d, want 2", total)
	}

	blockedCtx, cancel := context.WithTimeout(ctx, 35*time.Millisecond)
	defer cancel()
	if lease3, _, err := pool.Acquire(blockedCtx, nil); !errors.Is(err, context.DeadlineExceeded) {
		if lease3 != nil {
			lease3.Release()
		}
		t.Fatalf("third Acquire error = %v, want context deadline exceeded", err)
	}

	lease1.Release()
	freeCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	lease3, _, err := pool.Acquire(freeCtx, nil)
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	lease3.Release()
}

func TestChatMMXRawSwitchesKeyAfter429(t *testing.T) {
	clearMiniMaxEnv(t)
	resetMiniMaxGlobalKeyPool(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.Header.Get("Authorization") {
		case "Bearer sk-cp-first1111":
			http.Error(w, "too many requests", http.StatusTooManyRequests)
		case "Bearer sk-cp-second2222":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":" ok from second "}}]}`))
		default:
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
			http.Error(w, "unexpected key", http.StatusUnauthorized)
		}
	}))
	defer server.Close()
	t.Setenv("MINIMAX_API_KEYS", "sk-cp-first1111,sk-cp-second2222")
	t.Setenv("MINIMAX_BASE_URL", server.URL)

	got, err := NewClient().chatMMXRaw(context.Background(), miniMaxTestConfig(), []map[string]any{{"role": "user", "content": "hi"}})
	if err != nil {
		t.Fatalf("chatMMXRaw returned error: %v", err)
	}
	if got != "ok from second" {
		t.Fatalf("chatMMXRaw = %q", got)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestChatMMXRawSkipsAuthFailedKey(t *testing.T) {
	clearMiniMaxEnv(t)
	resetMiniMaxGlobalKeyPool(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.Header.Get("Authorization") {
		case "Bearer sk-cp-bad1111":
			http.Error(w, "forbidden", http.StatusForbidden)
		case "Bearer sk-cp-good2222":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok from good"}}]}`))
		default:
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
			http.Error(w, "unexpected key", http.StatusUnauthorized)
		}
	}))
	defer server.Close()
	t.Setenv("MINIMAX_API_KEYS", "sk-cp-bad1111,sk-cp-good2222")
	t.Setenv("MINIMAX_BASE_URL", server.URL)

	got, err := NewClient().chatMMXRaw(context.Background(), miniMaxTestConfig(), []map[string]any{{"role": "user", "content": "hi"}})
	if err != nil {
		t.Fatalf("chatMMXRaw returned error: %v", err)
	}
	if got != "ok from good" {
		t.Fatalf("chatMMXRaw = %q", got)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

func TestChatMMXRawDoesNotSwitchKeyForSensitiveError(t *testing.T) {
	clearMiniMaxEnv(t)
	resetMiniMaxGlobalKeyPool(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer sk-cp-first1111" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"unprocessable_entity_error","message":"output new_sensitive (1027)","http_code":"422"}}`))
	}))
	defer server.Close()
	t.Setenv("MINIMAX_API_KEYS", "sk-cp-first1111,sk-cp-second2222")
	t.Setenv("MINIMAX_BASE_URL", server.URL)

	_, err := NewClient().chatMMXRaw(context.Background(), miniMaxTestConfig(), []map[string]any{{"role": "user", "content": "hi"}})
	if err == nil {
		t.Fatal("expected sensitive error")
	}
	if !strings.Contains(err.Error(), "output new_sensitive (1027)") {
		t.Fatalf("error = %v, want sensitive code", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want fail-fast without key switch", calls.Load())
	}
}

func TestAnalyzeMiniMaxImageUsesVisionEndpoint(t *testing.T) {
	clearMiniMaxEnv(t)
	resetMiniMaxGlobalKeyPool(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/coding_plan/vlm" {
			t.Errorf("path = %q, want /v1/coding_plan/vlm", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-cp-vision1111" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":"vision ok","base_resp":{"status_code":0,"status_msg":"success"}}`))
	}))
	defer server.Close()
	t.Setenv("MINIMAX_API_KEYS", "sk-cp-vision1111")
	t.Setenv("MINIMAX_BASE_URL", server.URL)

	got, err := NewClient().AnalyzeImage(context.Background(), miniMaxTestConfig(), "describe", []byte{0xff, 0xd8, 0xff, 0xd9}, "image/jpeg")
	if err != nil {
		t.Fatalf("AnalyzeImage returned error: %v", err)
	}
	if got != "vision ok" {
		t.Fatalf("AnalyzeImage = %q", got)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestMiniMaxChatAndVisionShareOneKeyConcurrency(t *testing.T) {
	clearMiniMaxEnv(t)
	resetMiniMaxGlobalKeyPool(t)
	var calls atomic.Int32
	var active atomic.Int32
	var maxActive atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		now := active.Add(1)
		for {
			max := maxActive.Load()
			if now <= max || maxActive.CompareAndSwap(max, now) {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
		active.Add(-1)
		if r.Header.Get("Authorization") != "Bearer sk-cp-one1111" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"chat ok"}}]}`))
		case "/v1/coding_plan/vlm":
			_, _ = w.Write([]byte(`{"content":"vision ok","base_resp":{"status_code":0,"status_msg":"success"}}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("MINIMAX_API_KEYS", "sk-cp-one1111")
	t.Setenv("MINIMAX_BASE_URL", server.URL)

	client := NewClient()
	cfg := miniMaxTestConfig()
	errCh := make(chan error, 2)
	go func() {
		_, err := client.Chat(context.Background(), cfg, []ChatMessage{{Role: "user", Content: "hi"}})
		errCh <- err
	}()
	go func() {
		_, err := client.AnalyzeImage(context.Background(), cfg, "describe", []byte{0xff, 0xd8, 0xff, 0xd9}, "image/jpeg")
		errCh <- err
	}()
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("request %d returned error: %v", i, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
	if maxActive.Load() != 1 {
		t.Fatalf("max active requests for one key = %d, want 1", maxActive.Load())
	}
}

func TestMiniMaxErrorSanitizesFullAPIKey(t *testing.T) {
	key := "sk-cp-secretABCDE"
	err := sanitizeMiniMaxError(fmt.Errorf("upstream echoed %s", key), []string{key})
	if strings.Contains(err.Error(), key) {
		t.Fatalf("sanitized error leaked key: %s", err)
	}
	if !strings.Contains(err.Error(), "key_redacted") {
		t.Fatalf("sanitized error did not include key_redacted marker: %s", err)
	}
}

func clearMiniMaxEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"MINIMAX_API_KEYS", "MINMAX_API_KEYS", "MINIMAX_BASE_URL", "MINMAX_BASE_URL"} {
		t.Setenv(name, "")
	}
	for _, prefix := range []string{"MINIMAX", "MINMAX"} {
		t.Setenv(prefix+"_API_KEY", "")
		for i := 2; i <= 20; i++ {
			t.Setenv(fmt.Sprintf("%s%d_API_KEY", prefix, i), "")
		}
	}
}

func resetMiniMaxGlobalKeyPool(t *testing.T) {
	t.Helper()
	old := miniMaxGlobalKeyPool
	miniMaxGlobalKeyPool = &miniMaxAPIKeyPool{}
	t.Cleanup(func() {
		miniMaxGlobalKeyPool = old
	})
}

func miniMaxTestConfig() conf.SemanticConfig {
	return conf.NormalizeSemanticConfig(conf.SemanticConfig{
		ChatProvider:    conf.ProviderMMX,
		ChatModel:       conf.DefaultMMXChat,
		ChatMaxTokens:   conf.DefaultSemanticMaxTokens,
		ChatTemperature: conf.DefaultSemanticTemp,
	})
}

func TestMiniMaxKeyPoolStatusReportsConfiguredCountWithoutSecrets(t *testing.T) {
	resetMiniMaxGlobalKeyPool(t)
	clearMiniMaxEnv(t)
	keys := []string{
		"sk-cp-poolstat1111",
		"sk-cp-poolstat2222",
		"sk-cp-poolstat3333",
		"sk-cp-poolstat4444",
		"sk-cp-poolstat5555",
	}
	t.Setenv("MINIMAX_API_KEYS", strings.Join(keys, ","))

	cfg, err := loadMiniMaxHTTPConfig()
	if err != nil {
		t.Fatalf("loadMiniMaxHTTPConfig returned error: %v", err)
	}
	if got, want := len(cfg.APIKeys), 5; got != want {
		t.Fatalf("len(APIKeys) = %d, want %d", got, want)
	}

	// Touch the global pool with a real Acquire so the snapshot reflects 5 slots.
	acquireCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	lease, total, err := miniMaxGlobalKeyPool.Acquire(acquireCtx, nil)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}
	if lease == nil {
		t.Fatalf("Acquire returned nil lease")
	}
	miniMaxGlobalKeyPool.recordLease()
	lease.Release()

	// Record an error so retry_count and last_error_bucket surface.
	miniMaxGlobalKeyPool.recordError("minimax_sensitive_1027", "")
	miniMaxGlobalKeyPool.recordError("minimax_rate_limited", "")

	snap := MiniMaxKeyPoolStatus()
	if snap.ConfiguredKeyCount != 5 {
		t.Fatalf("ConfiguredKeyCount = %d, want 5", snap.ConfiguredKeyCount)
	}
	if snap.LeasedRequestCount < 1 {
		t.Fatalf("LeasedRequestCount = %d, want >= 1", snap.LeasedRequestCount)
	}
	if snap.RetryCount < 2 {
		t.Fatalf("RetryCount = %d, want >= 2", snap.RetryCount)
	}
	if snap.LastErrorBucket == "" {
		t.Fatalf("LastErrorBucket is empty")
	}
	if snap.BusyKeyCount < 0 || snap.BusyKeyCount > snap.ConfiguredKeyCount {
		t.Fatalf("BusyKeyCount out of range: %d", snap.BusyKeyCount)
	}
	if snap.IdleKeyCount < 0 {
		t.Fatalf("IdleKeyCount is negative: %d", snap.IdleKeyCount)
	}
	if snap.IdleKeyCount+snap.BusyKeyCount != snap.ConfiguredKeyCount {
		t.Fatalf("idle(%d) + busy(%d) != configured(%d)", snap.IdleKeyCount, snap.BusyKeyCount, snap.ConfiguredKeyCount)
	}

	// Verify the snapshot never carries a real key, key prefix, or any
	// reversible fragment.
	jsonBlob, err := jsonMarshal(snap)
	if err != nil {
		t.Fatalf("jsonMarshal failed: %v", err)
	}
	if strings.Contains(jsonBlob, "sk-") {
		t.Fatalf("snapshot leaked sk- prefix: %s", jsonBlob)
	}
	for _, k := range keys {
		if strings.Contains(jsonBlob, k) {
			t.Fatalf("snapshot leaked key fragment: key=%s blob=%s", k, jsonBlob)
		}
	}
	// Labels should be present and redaction-style (stable ordinal key_N).
	if len(snap.Labels) != 5 {
		t.Fatalf("len(Labels) = %d, want 5", len(snap.Labels))
	}
	for i, label := range snap.Labels {
		want := fmt.Sprintf("key_%d", i+1)
		if label != want {
			t.Fatalf("label[%d] = %q, want %q", i, label, want)
		}
	}
}

func TestMiniMaxKeyPoolStatusIdleAndBusyAfterLease(t *testing.T) {
	resetMiniMaxGlobalKeyPool(t)
	clearMiniMaxEnv(t)
	t.Setenv("MINIMAX_API_KEYS", "sk-cp-leased-1111,sk-cp-leased-2222")

	acquireCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	lease, total, err := miniMaxGlobalKeyPool.Acquire(acquireCtx, nil)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if lease == nil {
		t.Fatalf("Acquire returned nil lease")
	}
	// Do not release the lease so that the slot stays busy.
	snap := MiniMaxKeyPoolStatus()
	if snap.ConfiguredKeyCount != 2 {
		t.Fatalf("ConfiguredKeyCount = %d, want 2", snap.ConfiguredKeyCount)
	}
	if snap.BusyKeyCount != 1 {
		t.Fatalf("BusyKeyCount = %d, want 1", snap.BusyKeyCount)
	}
	if snap.IdleKeyCount != 1 {
		t.Fatalf("IdleKeyCount = %d, want 1", snap.IdleKeyCount)
	}
	lease.Release()

	snap = MiniMaxKeyPoolStatus()
	if snap.BusyKeyCount != 0 {
		t.Fatalf("BusyKeyCount after release = %d, want 0", snap.BusyKeyCount)
	}
	if snap.IdleKeyCount != 2 {
		t.Fatalf("IdleKeyCount after release = %d, want 2", snap.IdleKeyCount)
	}
}

func TestClassifyMiniMaxErrorBucketCoversKnownBuckets(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{errors.New("minimax chat error: input new_sensitive (1026)"), "minimax_sensitive_1026"},
		{errors.New("minimax chat error: output new_sensitive (1027)"), "minimax_sensitive_1027"},
		{errors.New("minimax chat failed: context deadline exceeded"), "minimax_timeout"},
		{errors.New("minimax chat failed before request: context deadline exceeded"), "minimax_before_request_timeout"},
		{errors.New("minimax chat error: model http 401"), "minimax_auth_error"},
		{errors.New("minimax chat error: model http 429"), "minimax_rate_limited"},
		{errors.New("minimax chat error: model http 500"), "minimax_server_error"},
		{errors.New("minimax chat error: model http 400"), "minimax_client_error"},
		{errors.New("decode model response failed: <nil>"), "minimax_decode_error"},
		{errors.New("minimax chat returned empty choices"), "minimax_empty_response"},
		{errors.New("minimax api key is not configured"), "minimax_config_error"},
		{errors.New("minimax chat error: upstream service unavailable"), "minimax_upstream_error"},
		{errors.New("something else entirely"), "minimax_other"},
	}
	for _, c := range cases {
		if got := classifyMiniMaxErrorBucket(c.err); got != c.want {
			t.Errorf("classifyMiniMaxErrorBucket(%q) = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestClassifyMiniMaxErrorBucketEmptyForNil(t *testing.T) {
	if got := classifyMiniMaxErrorBucket(nil); got != "" {
		t.Fatalf("classifyMiniMaxErrorBucket(nil) = %q, want empty", got)
	}
}

func TestMiniMaxKeyPoolLabelsUseStableOrdinals(t *testing.T) {
	resetMiniMaxGlobalKeyPool(t)
	clearMiniMaxEnv(t)
	t.Setenv("MINIMAX_API_KEYS", "sk-cp-ord1111,sk-cp-ord2222,sk-cp-ord3333")

	acquireCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	lease, total, err := miniMaxGlobalKeyPool.Acquire(acquireCtx, nil)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	lease.Release()

	snap := MiniMaxKeyPoolStatus()
	wantLabels := []string{"key_1", "key_2", "key_3"}
	if len(snap.Labels) != len(wantLabels) {
		t.Fatalf("len(Labels) = %d, want %d", len(snap.Labels), len(wantLabels))
	}
	for i, want := range wantLabels {
		if snap.Labels[i] != want {
			t.Fatalf("snap.Labels[%d] = %q, want %q", i, snap.Labels[i], want)
		}
	}

	// Sanitized token must be used in error messages, not the raw key bytes.
	errBoom := sanitizeMiniMaxError(fmt.Errorf("upstream echoed sk-cp-ord2222 boom"),
		[]string{"sk-cp-ord2222"})
	if strings.Contains(errBoom.Error(), "sk-cp-ord2222") {
		t.Fatalf("sanitized error leaked raw key: %s", errBoom)
	}
	if !strings.Contains(errBoom.Error(), "key_redacted") {
		t.Fatalf("sanitized error missing key_redacted token: %s", errBoom)
	}
}

func TestRecordErrorQuarantinesKeyOnAuthError(t *testing.T) {
	resetMiniMaxGlobalKeyPool(t)
	clearMiniMaxEnv(t)
	keys := []string{
		"sk-cp-qauth1111",
		"sk-cp-qauth2222",
		"sk-cp-qauth3333",
	}
	t.Setenv("MINIMAX_API_KEYS", strings.Join(keys, ","))

	// Touch Acquire once so ensureLocked materializes the 3 slots. chatMMXRaw
	// always calls recordError after a successful lease, so production code
	// hits the same "slots are populated" precondition.
	acquireCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	lease, total, err := miniMaxGlobalKeyPool.Acquire(acquireCtx, nil)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	lease.Release()

	// Trigger an auth error for key_2.
	miniMaxGlobalKeyPool.recordError("minimax_auth_error", "key_2")

	snap := MiniMaxKeyPoolStatus()
	if snap.QuarantinedKeyCount != 1 {
		t.Fatalf("QuarantinedKeyCount = %d, want 1", snap.QuarantinedKeyCount)
	}
	if snap.HealthyKeyCount != 2 {
		t.Fatalf("HealthyKeyCount = %d, want 2", snap.HealthyKeyCount)
	}
	if len(snap.QuarantinedLabels) != 1 || snap.QuarantinedLabels[0] != "key_2" {
		t.Fatalf("QuarantinedLabels = %v, want [key_2]", snap.QuarantinedLabels)
	}
	if snap.LastQuarantinedLabel != "key_2" || snap.LastQuarantinedFor != "minimax_auth_error" {
		t.Fatalf("LastQuarantinedLabel=%q LastQuarantinedFor=%q", snap.LastQuarantinedLabel, snap.LastQuarantinedFor)
	}
	if snap.LastQuarantinedAt.IsZero() {
		t.Fatalf("LastQuarantinedAt is zero")
	}

	// Acquire must not return the quarantined slot even after multiple attempts.
	gotLabels := map[string]bool{}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && len(gotLabels) < 2 {
		acquireCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		lease, _, err := miniMaxGlobalKeyPool.Acquire(acquireCtx, nil)
		cancel()
		if err != nil || lease == nil {
			continue
		}
		gotLabels[lease.label] = true
		lease.Release()
	}
	if gotLabels["key_2"] {
		t.Fatalf("Acquire returned quarantined key_2 (got=%v)", gotLabels)
	}
	if !gotLabels["key_1"] || !gotLabels["key_3"] {
		t.Fatalf("Acquire did not surface healthy labels (got=%v, want key_1 and key_3)", gotLabels)
	}

	// Repeat the same auth error: snapshot should report a single quarantine
	// (hits increment but no extra key is quarantined).
	miniMaxGlobalKeyPool.recordError("minimax_auth_error", "key_2")
	snap = MiniMaxKeyPoolStatus()
	if snap.QuarantinedKeyCount != 1 {
		t.Fatalf("QuarantinedKeyCount after repeat = %d, want 1", snap.QuarantinedKeyCount)
	}
}

func TestRecordErrorDoesNotQuarantineOnSensitiveOrRateOrDecode(t *testing.T) {
	resetMiniMaxGlobalKeyPool(t)
	clearMiniMaxEnv(t)
	t.Setenv("MINIMAX_API_KEYS", "sk-cp-noq1111,sk-cp-noq2222")

	acquireCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	lease, _, err := miniMaxGlobalKeyPool.Acquire(acquireCtx, nil)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	lease.Release()

	// None of these buckets are allowed to move a key into quarantine; the
	// AC explicitly demands sensitive 1026/1027 not rotate keys.
	for _, bucket := range []string{
		"minimax_sensitive_1026",
		"minimax_sensitive_1027",
		"minimax_rate_limited",
		"minimax_decode_error",
		"minimax_timeout",
		"minimax_client_error",
		"minimax_server_error",
		"minimax_config_error",
	} {
		miniMaxGlobalKeyPool.recordError(bucket, "key_1")
	}
	snap := MiniMaxKeyPoolStatus()
	if snap.QuarantinedKeyCount != 0 {
		t.Fatalf("QuarantinedKeyCount = %d, want 0 (snap=%+v)", snap.QuarantinedKeyCount, snap)
	}
	if len(snap.QuarantinedLabels) != 0 {
		t.Fatalf("QuarantinedLabels = %v, want []", snap.QuarantinedLabels)
	}
}

func TestAcquireSkipsQuarantinedKey(t *testing.T) {
	resetMiniMaxGlobalKeyPool(t)
	clearMiniMaxEnv(t)
	t.Setenv("MINIMAX_API_KEYS", "sk-cp-skip1111,sk-cp-skip2222,sk-cp-skip3333")

	// Materialize slots before issuing the auth error (production only records
	// after a successful lease).
	acquireCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	lease, _, err := miniMaxGlobalKeyPool.Acquire(acquireCtx, nil)
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	lease.Release()

	// Mark key_2 quarantined.
	miniMaxGlobalKeyPool.recordError("minimax_auth_error", "key_2")

	gotLabels := map[string]bool{}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && len(gotLabels) < 2 {
		acquireCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		lease, total, err := miniMaxGlobalKeyPool.Acquire(acquireCtx, nil)
		cancel()
		if err != nil || lease == nil {
			continue
		}
		if total != 3 {
			t.Fatalf("total = %d, want 3 (quarantined key still counts in configured)", total)
		}
		gotLabels[lease.label] = true
		lease.Release()
	}
	if gotLabels["key_2"] {
		t.Fatalf("Acquire returned quarantined key_2 (got=%v)", gotLabels)
	}
	if !gotLabels["key_1"] || !gotLabels["key_3"] {
		t.Fatalf("Acquire did not surface both healthy labels (got=%v)", gotLabels)
	}

	// Snapshot should report key_2 as quarantined and configured=3.
	snap := MiniMaxKeyPoolStatus()
	if snap.ConfiguredKeyCount != 3 {
		t.Fatalf("ConfiguredKeyCount = %d, want 3", snap.ConfiguredKeyCount)
	}
	if snap.QuarantinedKeyCount != 1 || snap.HealthyKeyCount != 2 {
		t.Fatalf("Quarantined=%d Healthy=%d, want 1/2", snap.QuarantinedKeyCount, snap.HealthyKeyCount)
	}
}
