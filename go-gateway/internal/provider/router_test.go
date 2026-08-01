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

	router.RegisterProvider("glm", NewAnthropicProvider(&Config{
		Name: "glm", URL: "https://api.example.com", APIKey: "test-key",
	}))
	router.RegisterChain("coding", []string{"glm"})

	providers, err := router.Route("coding")
	if err != nil {
		t.Fatalf("Route() failed: %v", err)
	}
	if len(providers) != 1 {
		t.Errorf("Expected 1 provider, got %d", len(providers))
	}
}

func TestRouterSkipsUnavailableFallbackProviders(t *testing.T) {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	router := NewRouter(logger)
	router.RegisterProvider("glm", NewAnthropicProvider(&Config{
		Name: "glm", URL: "https://api.example.com", APIKey: "test-key",
	}))
	router.RegisterChain("coding", []string{"glm", "ali"})

	providers, err := router.Route("coding")
	if err != nil {
		t.Fatalf("Route() failed when only an optional fallback was unavailable: %v", err)
	}
	if len(providers) != 1 || providers[0].Name() != "glm" {
		t.Fatalf("expected the available primary provider, got %#v", providers)
	}
}

func TestRouterReturnsErrorWhenAllProvidersUnavailable(t *testing.T) {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	router := NewRouter(logger)
	router.RegisterChain("coding", []string{"glm", "ali"})

	if _, err := router.Route("coding"); err == nil {
		t.Fatal("expected an error when all configured providers are unavailable")
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

	for _, name := range []string{"glm"} {
		router.RegisterProvider(name, NewAnthropicProvider(&Config{
			Name: name, URL: "https://api.example.com", APIKey: "key",
		}))
	}

	providers := router.ListProviders()
	sort.Strings(providers)
	if len(providers) != 1 {
		t.Errorf("Expected 1 provider, got %d: %v", len(providers), providers)
	}
}
