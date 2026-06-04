package conf

import "testing"

func TestNormalizeSemanticConfigDefaultsMMXToMiniMaxM3(t *testing.T) {
	cfg := NormalizeSemanticConfig(SemanticConfig{
		ChatProvider: ProviderMMX,
	})

	if cfg.ChatModel != DefaultMMXChat {
		t.Fatalf("expected default MMX chat model %q, got %q", DefaultMMXChat, cfg.ChatModel)
	}
	if cfg.ChatModel != "MiniMax-M3" {
		t.Fatalf("expected MiniMax-M3 default, got %q", cfg.ChatModel)
	}
}

func TestNormalizeSemanticConfigResetsKnownMMXModelsForOtherProviders(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		input    string
		want     string
	}{
		{name: "glm resets m3", provider: ProviderGLM, input: DefaultMMXChat, want: DefaultGLMChat},
		{name: "glm resets legacy m27", provider: ProviderGLM, input: LegacyMMXChatM27, want: DefaultGLMChat},
		{name: "deepseek resets m3", provider: ProviderDeepSeek, input: DefaultMMXChat, want: DefaultDeepSeekChat},
		{name: "ollama resets m3", provider: ProviderOllama, input: DefaultMMXChat, want: DefaultOllamaChat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NormalizeSemanticConfig(SemanticConfig{
				ChatProvider: tt.provider,
				ChatModel:    tt.input,
			})
			if cfg.ChatModel != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, cfg.ChatModel)
			}
		})
	}
}
