package provider

import (
	"encoding/json"
	"testing"
)

func TestRequestMarshal(t *testing.T) {
	req := &Request{
		Model:     "glm-sonnet",
		Messages:  []Message{{Role: "user", Content: NewStringContent("hello")}},
		MaxTokens: 1000,
	}
	if err := req.SetRawField("messages", req.Messages); err != nil {
		t.Fatalf("Failed to set raw messages: %v", err)
	}
	if err := req.SetRawField("max_tokens", req.MaxTokens); err != nil {
		t.Fatalf("Failed to set raw max_tokens: %v", err)
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal request: %v", err)
	}

	if decoded.Model != req.Model {
		t.Errorf("Marshaling roundtrip failed: got %s, want %s", decoded.Model, req.Model)
	}
}

func TestResponseContentIsSlice(t *testing.T) {
	// 验证 Response.Content 是数组（对齐 Anthropic 真实格式）
	raw := `{"id":"msg_1","type":"message","role":"assistant","model":"glm-5-turbo","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":3}}`
	var resp Response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if len(resp.Content) != 1 {
		t.Errorf("Expected 1 content block, got %d", len(resp.Content))
	}
	if resp.Content[0].Text != "hi" {
		t.Errorf("Expected text 'hi', got %s", resp.Content[0].Text)
	}
}

func TestContentBlockMarshalPreservesToolResultContent(t *testing.T) {
	block := ContentBlock{Type: "tool_result", ToolUseID: "call_1", ContentStr: "sunny"}
	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal tool result: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode tool result: %v", err)
	}
	if decoded["content"] != "sunny" {
		t.Fatalf("expected tool result content, got %s", data)
	}
}

func TestConfigValidation(t *testing.T) {
	cfg := &Config{
		Name:      "test",
		URL:       "https://api.example.com",
		APIKey:    "key123",
		UseBearer: true,
	}
	if cfg.Name != "test" {
		t.Errorf("Config name mismatch")
	}
	if !cfg.UseBearer {
		t.Errorf("UseBearer should be true")
	}
}
