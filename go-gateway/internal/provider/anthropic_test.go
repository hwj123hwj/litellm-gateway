package provider

import (
	"testing"
)

func TestNewAnthropicProvider(t *testing.T) {
	config := &Config{
		Name:      "test-provider",
		URL:       "https://api.example.com/v1/messages",
		APIKey:    "test-key",
		UseBearer: false,
	}

	p := NewAnthropicProvider(config)

	if p.Name() != "test-provider" {
		t.Errorf("Expected name 'test-provider', got %s", p.Name())
	}
	if p.APIKey() != "test-key" {
		t.Errorf("Expected APIKey 'test-key', got %s", p.APIKey())
	}
	if p.URL() != config.URL {
		t.Errorf("Expected URL '%s', got %s", config.URL, p.URL())
	}
	if p.UseBearer() != false {
		t.Errorf("Expected UseBearer false")
	}
}

func TestAnthropicProviderWithBearer(t *testing.T) {
	config := &Config{
		Name:      "longcat",
		URL:       "https://api.longcat.chat/anthropic/v1/messages",
		APIKey:    "longcat-key",
		UseBearer: true,
	}
	p := NewAnthropicProvider(config)
	if !p.UseBearer() {
		t.Errorf("Expected UseBearer true for longcat")
	}
}

func TestProviderInterface(t *testing.T) {
	config := &Config{Name: "test", URL: "https://api.example.com", APIKey: "key"}
	p := NewAnthropicProvider(config)
	// 验证实现了 Provider 接口
	var _ Provider = p
}
