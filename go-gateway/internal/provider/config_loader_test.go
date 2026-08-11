package provider

import (
	"testing"
	"time"
)

func TestProviderConfigUsesLegacyAliAPIKey(t *testing.T) {
	t.Setenv("ALI_API_KEY", "")
	t.Setenv("DASHSCOPE_API_KEY", "")
	t.Setenv("ALIYUN_MAAS_API_KEY", "legacy-ali-key")

	configured, err := NewOpenAIProviderFromConfig(&ProviderConfig{
		Name:      "ali",
		Type:      "openai",
		URL:       "https://example.invalid/v1/chat/completions",
		APIKeyEnv: "ALI_API_KEY",
	})
	if err != nil {
		t.Fatalf("expected legacy Ali key to configure provider: %v", err)
	}
	if configured.APIKey() != "legacy-ali-key" {
		t.Fatalf("expected legacy Ali key, got %q", configured.APIKey())
	}
}

func TestProviderConfigAppliesRequestTimeout(t *testing.T) {
	t.Setenv("GLM_API_KEY", "glm-test-key")

	configured, err := NewOpenAIProviderFromConfig(&ProviderConfig{
		Name:                  "glm",
		Type:                  "openai",
		URL:                   "https://example.invalid/v1/chat/completions",
		APIKeyEnv:             "GLM_API_KEY",
		RequestTimeoutSeconds: 900,
	})
	if err != nil {
		t.Fatalf("expected provider to configure: %v", err)
	}

	openAI, ok := configured.(*OpenAIProvider)
	if !ok {
		t.Fatalf("expected *OpenAIProvider, got %T", configured)
	}
	if openAI.client.Timeout != 900*time.Second {
		t.Fatalf("request timeout = %s, want 15m", openAI.client.Timeout)
	}
}

func TestOpenAIProviderUsesSafeDefaultRequestTimeout(t *testing.T) {
	provider := NewOpenAIProvider(&Config{Name: "test"})
	if provider.client.Timeout != defaultOpenAIRequestTimeout {
		t.Fatalf("request timeout = %s, want %s", provider.client.Timeout, defaultOpenAIRequestTimeout)
	}
}
