package provider

import "testing"

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
