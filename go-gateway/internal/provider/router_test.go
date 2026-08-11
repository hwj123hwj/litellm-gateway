package provider

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"testing"
	"time"
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

func TestRouterCircuitBreakerSkipsOpenProviderAndFallsBack(t *testing.T) {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	first := &errorPolicyProvider{
		name: "first",
		err:  &ProviderError{Provider: "first", StatusCode: http.StatusServiceUnavailable, Message: "down"},
	}
	second := &errorPolicyProvider{name: "second", resp: &Response{Model: "fallback"}}
	router := NewRouterWithCircuitConfig(logger, CircuitBreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  time.Hour,
		SuccessThreshold: 1,
	})
	router.RegisterProvider(first.name, first)
	router.RegisterProvider(second.name, second)
	router.RegisterChain("coding", []string{first.name, second.name})

	if _, err := router.Forward(context.Background(), "coding", &Request{Model: "coding"}); err != nil {
		t.Fatalf("first request should fallback: %v", err)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf("first request calls = first:%d second:%d", first.calls, second.calls)
	}

	if _, err := router.Forward(context.Background(), "coding", &Request{Model: "coding"}); err != nil {
		t.Fatalf("second request should use healthy fallback: %v", err)
	}
	if first.calls != 1 {
		t.Fatalf("open provider should be skipped, calls = %d", first.calls)
	}
	status, ok := router.ProviderStatus("first")
	if !ok || status.State != CircuitOpen || status.Status != "offline" {
		t.Fatalf("unexpected first provider status: %#v", status)
	}
}

func TestRouterForwardWithDetailsTracksFallbackAndFinalProvider(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	router := NewRouter(logger)
	first := &errorPolicyProvider{
		name: "first",
		err:  &ProviderError{Provider: "first", StatusCode: http.StatusTooManyRequests, Message: "quota exceeded"},
	}
	second := &errorPolicyProvider{name: "second", resp: &Response{Model: "fallback"}}
	router.RegisterProvider(first.name, first)
	router.RegisterProvider(second.name, second)
	router.RegisterChain("coding", []string{first.name, second.name})

	resp, finalProvider, attempts, err := router.ForwardWithDetails(context.Background(), "coding", &Request{Model: "coding"})
	if err != nil {
		t.Fatalf("ForwardWithDetails() error = %v", err)
	}
	if resp == nil || finalProvider != "second" {
		t.Fatalf("response/provider = %#v/%q", resp, finalProvider)
	}
	if len(attempts) != 2 || attempts[0].Provider != "first" || attempts[0].Status != "error" || attempts[0].StatusCode != http.StatusTooManyRequests || attempts[1].Provider != "second" || attempts[1].Status != "success" {
		t.Fatalf("attempts = %#v", attempts)
	}
}

func TestRouterKnowledgeCompileFallsBackAcrossConfiguredProviders(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	router := NewRouter(logger)
	first := &errorPolicyProvider{
		name: "glm-glm-5.2",
		err:  &ProviderError{Provider: "glm-glm-5.2", StatusCode: http.StatusTooManyRequests, Message: "quota exceeded"},
	}
	second := &errorPolicyProvider{
		name: "ali-qwen3.8-max-preview",
		err:  &ProviderError{Provider: "ali-qwen3.8-max-preview", StatusCode: http.StatusServiceUnavailable, Message: "temporarily unavailable"},
	}
	third := &errorPolicyProvider{
		name: "copilot",
		resp: &Response{Model: "gpt-4o"},
	}
	for _, candidate := range []*errorPolicyProvider{first, second, third} {
		router.RegisterProvider(candidate.name, candidate)
	}
	router.RegisterChain("glm-opus", []string{first.name, second.name, third.name})

	response, finalProvider, attempts, err := router.ForwardWithDetails(
		context.Background(),
		"glm-opus",
		&Request{Model: "glm-opus"},
	)
	if err != nil {
		t.Fatalf("expected Copilot fallback to succeed: %v", err)
	}
	if response == nil || finalProvider != "copilot" || response.Model != "gpt-4o" {
		t.Fatalf("response/provider = %#v/%q, want gpt-4o/copilot", response, finalProvider)
	}
	if len(attempts) != 3 || first.calls != 1 || second.calls != 1 || third.calls != 1 {
		t.Fatalf("attempts/calls = %#v/%d/%d/%d, want three attempts", attempts, first.calls, second.calls, third.calls)
	}
}

func TestRouterProviderAndRouteControls(t *testing.T) {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	router := NewRouter(logger)
	for _, name := range []string{"first", "second"} {
		router.RegisterProvider(name, &errorPolicyProvider{name: name, resp: &Response{Model: name}})
	}
	router.RegisterChain("coding", []string{"first", "second"})

	if err := router.SetProviderEnabled("first", false); err != nil {
		t.Fatalf("disable provider: %v", err)
	}
	providers, err := router.Route("coding")
	if err != nil || len(providers) != 1 || providers[0].Name() != "second" {
		t.Fatalf("route after disable = %#v, err=%v", providers, err)
	}
	if err := router.SetChainOrder("coding", []string{"second", "first"}); err != nil {
		t.Fatalf("reorder chain: %v", err)
	}
	routes := router.ListRouteStatuses()
	if len(routes) != 1 || len(routes[0].Providers) != 2 || routes[0].Providers[0].Name != "second" {
		t.Fatalf("route statuses = %#v", routes)
	}
	if err := router.SetModelCapabilities("coding", []string{CapabilityText, CapabilityVision}, []string{"text", "image"}); err != nil {
		t.Fatalf("set model capabilities: %v", err)
	}
	info := router.ListModelInfos()
	if len(info) != 1 || len(info[0].Capabilities) != 2 || info[0].Capabilities[1] != CapabilityVision {
		t.Fatalf("model info after update = %#v", info)
	}
	if err := router.SetProviderEnabled("missing", true); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("unknown provider error = %v", err)
	}
}
