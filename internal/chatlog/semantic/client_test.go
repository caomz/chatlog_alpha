package semantic

import (
	"context"
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
	if !strings.Contains(err.Error(), "***BCDE") && !strings.Contains(err.Error(), "sk-***") {
		t.Fatalf("sanitized error did not include a redacted marker: %s", err)
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
