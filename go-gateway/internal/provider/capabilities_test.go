package provider

import (
	"encoding/json"
	"io"
	"log"
	"testing"
)

func TestRequestRequiredCapabilitiesIgnoresEmptyToolsAndDetectsReasoning(t *testing.T) {
	req := &Request{}
	if err := req.SetRawField("tools", []any{}); err != nil {
		t.Fatalf("set empty tools: %v", err)
	}
	if got := req.RequiredCapabilities(); containsCapability(got, CapabilityToolCall) {
		t.Fatalf("empty tools must not require tool calling: %v", got)
	}

	if err := req.SetRawField("reasoning_effort", "high"); err != nil {
		t.Fatalf("set reasoning effort: %v", err)
	}
	if got := req.RequiredCapabilities(); !containsCapability(got, CapabilityReasoning) {
		t.Fatalf("reasoning request must require reasoning capability: %v", got)
	}

	// Explicitly disabled thinking is not a reasoning requirement.
	req = &Request{}
	if err := req.SetRawField("thinking", map[string]any{"type": "disabled"}); err != nil {
		t.Fatalf("set thinking: %v", err)
	}
	if got := req.RequiredCapabilities(); containsCapability(got, CapabilityReasoning) {
		t.Fatalf("disabled thinking must not require reasoning: %v", got)
	}
}

func TestRequestRequiredCapabilitiesDetectsVisionAndTools(t *testing.T) {
	req := &Request{
		Stream: true,
		Messages: []Message{{
			Role:    "user",
			Content: NewBlocksContent([]ContentBlock{{Type: "text", Text: "look"}, {Type: "image_url"}}),
		}},
	}
	if err := req.SetRawField("tools", []map[string]any{{"name": "describe"}}); err != nil {
		t.Fatalf("set tools: %v", err)
	}

	got := req.RequiredCapabilities()
	for _, want := range []string{CapabilityText, CapabilityVision, CapabilityToolCall, CapabilityStreaming} {
		found := false
		for _, capability := range got {
			if capability == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected capability %q in %v", want, got)
		}
	}
}

func TestRouterFiltersProvidersByRequestCapabilities(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	router := NewRouter(logger)
	textProvider := NewBoundModelProviderWrapper(NewAnthropicProvider(&Config{
		Name: "text", URL: "https://example.invalid", APIKey: "key",
	}), "text-model", []string{CapabilityText, CapabilityToolCall, CapabilityStreaming})
	visionProvider := NewBoundModelProviderWrapper(NewAnthropicProvider(&Config{
		Name: "vision", URL: "https://example.invalid", APIKey: "key",
	}), "vision-model", []string{CapabilityText, CapabilityVision, CapabilityToolCall, CapabilityStreaming})
	router.RegisterProvider("text", textProvider)
	router.RegisterProvider("vision", visionProvider)
	router.RegisterChain("vision-route", []string{"text", "vision"})

	req := &Request{Messages: []Message{{
		Role:    "user",
		Content: NewBlocksContent([]ContentBlock{{Type: "image_url"}}),
	}}}
	providers, err := router.RouteForRequest("vision-route", req)
	if err != nil {
		t.Fatalf("route vision request: %v", err)
	}
	if len(providers) != 1 || providers[0].Name() != "vision" {
		t.Fatalf("expected only vision provider, got %#v", providers)
	}

	router.RegisterChain("text-route", []string{"text"})
	if _, err := router.RouteForRequest("text-route", req); !IsUnsupportedCapability(err) {
		t.Fatalf("expected unsupported capability error, got %v", err)
	}
}

func TestRegisterChainAdvertisesUnionOfProviderCapabilities(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	router := NewRouter(logger)
	router.RegisterProvider("text", NewBoundModelProviderWrapper(NewAnthropicProvider(&Config{
		Name: "text", URL: "https://example.invalid", APIKey: "key",
	}), "text-model", []string{CapabilityText, CapabilityToolCall}))
	router.RegisterProvider("vision", NewBoundModelProviderWrapper(NewAnthropicProvider(&Config{
		Name: "vision", URL: "https://example.invalid", APIKey: "key",
	}), "vision-model", []string{CapabilityText, CapabilityVision, CapabilityStreaming}))

	router.RegisterChain("auto", []string{"text", "vision"})
	infos := router.ListModelInfos()
	if len(infos) != 1 || infos[0].ID != "auto" {
		t.Fatalf("unexpected model infos: %#v", infos)
	}
	for _, want := range []string{CapabilityText, CapabilityVision, CapabilityToolCall, CapabilityStreaming} {
		if !containsCapability(infos[0].Capabilities, want) {
			t.Fatalf("chain capabilities missing %q: %#v", want, infos[0])
		}
	}
	if got, _ := json.Marshal(infos[0].InputModalities); string(got) != `["text","image"]` {
		t.Fatalf("unexpected input modalities: %s", got)
	}
}

func containsCapability(capabilities []string, target string) bool {
	for _, capability := range capabilities {
		if capability == target {
			return true
		}
	}
	return false
}

// Ensure the wrapper still satisfies the optional streaming provider surface.
var _ StreamProvider = (*BoundModelProviderWrapper)(nil)
