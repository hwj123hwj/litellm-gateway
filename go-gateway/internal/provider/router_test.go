package provider

import (
	"log"
	"os"
	"sort"
	"testing"
)

func TestRouterRegisterProvider(t *testing.T) {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	router := NewRouter(logger)

	router.RegisterProvider("glm", NewAnthropicProvider(&Config{
		Name: "glm", URL: "https://api.example.com", APIKey: "test-key",
	}))

	providers := router.ListProviders()
	if len(providers) != 1 || providers[0] != "glm" {
		t.Errorf("Expected provider 'glm', got %v", providers)
	}
}

func TestRouterRegisterChain(t *testing.T) {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	router := NewRouter(logger)

	for _, name := range []string{"glm", "mimo", "longcat"} {
		router.RegisterProvider(name, NewAnthropicProvider(&Config{
			Name: name, URL: "https://api.example.com", APIKey: "test-key",
		}))
	}
	router.RegisterChain("coding", []string{"glm", "mimo", "longcat"})

	providers, err := router.Route("coding")
	if err != nil {
		t.Fatalf("Route() failed: %v", err)
	}
	if len(providers) != 3 {
		t.Errorf("Expected 3 providers, got %d", len(providers))
	}
}

func TestRouterMapModelName(t *testing.T) {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	router := NewRouter(logger)

	tests := []struct {
		modelName    string
		providerName string
		expected     string
	}{
		{"glm-sonnet", "glm", "glm-5-turbo"},
		{"glm-opus", "glm", "glm-5.2"},
		{"glm-haiku", "glm", "glm-4.7"},
		{"mimo-sonnet", "mimo", "mimo-v2.5"},
		{"mimo-opus", "mimo", "mimo-v2.5-pro"},
		{"longcat-opus", "longcat", "LongCat-2.0-Preview"},
		{"easyclaw-sonnet", "easyclaw", "claude-sonnet-4-6"},
		{"deepv-deepseek-flash", "deepv-deepseek", "deepseek-v4-flash"},
		{"deepv-glm5", "deepv-glm5", "glm-5"},
		{"deepv-claude-sonnet", "deepv-claude", "claude-sonnet-4-6"},
		{"deepv-kimi", "deepv-kimi", "kimi-k2.6"},
	}

	for _, tt := range tests {
		result := router.mapModelName(tt.modelName, tt.providerName)
		if result != tt.expected {
			t.Errorf("mapModelName(%q, %q) = %q, want %q", tt.modelName, tt.providerName, result, tt.expected)
		}
	}
}

func TestRouterUnknownModel(t *testing.T) {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	router := NewRouter(logger)
	router.RegisterProvider("glm", NewAnthropicProvider(&Config{
		Name: "glm", URL: "https://api.example.com", APIKey: "test-key",
	}))

	_, err := router.Route("unknown-model")
	if err == nil {
		t.Fatal("Expected error for unknown model")
	}
}

func TestRouterListProviders(t *testing.T) {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	router := NewRouter(logger)

	for _, name := range []string{"glm", "mimo", "longcat"} {
		router.RegisterProvider(name, NewAnthropicProvider(&Config{
			Name: name, URL: "https://api.example.com", APIKey: "key",
		}))
	}

	providers := router.ListProviders()
	sort.Strings(providers)
	if len(providers) != 3 {
		t.Errorf("Expected 3 providers, got %d: %v", len(providers), providers)
	}
}
