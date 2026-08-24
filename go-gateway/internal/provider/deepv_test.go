package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDeepVProviderCapabilities(t *testing.T) {
	flash := NewDeepVProvider(&Config{Name: "deepv-deepseek-flash", URL: "https://example.com/v1/chat/messages"}, "", "deepseek-v4-flash")
	for _, c := range []string{CapabilityText, CapabilityToolCall, CapabilityStreaming, CapabilityReasoning} {
		if !hasCapability(flash.Capabilities(), c) {
			t.Errorf("flash missing capability %q", c)
		}
	}
	if hasCapability(flash.Capabilities(), CapabilityVision) {
		t.Error("flash should not declare vision")
	}

	vision := NewDeepVProvider(&Config{Name: "deepv-deepseek-flash-vision", URL: "https://example.com/v1/chat/messages"}, "", "deepseek-v4-flash-vision-exp")
	for _, c := range []string{CapabilityText, CapabilityVision, CapabilityToolCall, CapabilityStreaming, CapabilityReasoning} {
		if !hasCapability(vision.Capabilities(), c) {
			t.Errorf("vision missing capability %q", c)
		}
	}
}

func hasCapability(list []string, want string) bool {
	for _, c := range list {
		if c == want {
			return true
		}
	}
	return false
}

func TestDeepVConvertRequest(t *testing.T) {
	p := NewDeepVProvider(&Config{Name: "deepv", URL: "https://example.com/v1/chat/messages"}, "", "deepseek-v4-flash-vision-exp")

	raw := `{
		"model": "deepseek-v4-flash-vision-exp",
		"max_tokens": 1024,
		"temperature": 0.7,
		"system": [{"type":"text","text":"be brief"}],
		"tools": [{"name":"lookup","description":"d","input_schema":{"type":"object","properties":{"q":{"type":"string"}}}}],
		"messages": [
			{"role":"user","content":[
				{"type":"text","text":"what is this?"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}
			]},
			{"role":"assistant","content":[
				{"type":"text","text":"let me check"},
				{"type":"tool_use","id":"tu_1","name":"lookup","input":{"q":"x"}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"tu_1","content":"found"}
			]}
		]
	}`

	var req Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	out, err := p.convertRequest(&req)
	if err != nil {
		t.Fatalf("convertRequest: %v", err)
	}

	if out.Model != "deepseek-v4-flash-vision-exp" {
		t.Errorf("model = %q", out.Model)
	}
	if out.SystemInstruction == nil || len(out.SystemInstruction.Parts) != 1 {
		t.Fatal("system instruction missing")
	}
	if out.Config.MaxOutputTokens != 1024 {
		t.Errorf("maxOutputTokens = %d", out.Config.MaxOutputTokens)
	}
	if out.Config.Temperature != 0.7 {
		t.Errorf("temperature = %v", out.Config.Temperature)
	}
	if len(out.Config.Tools) != 1 || len(out.Config.Tools[0].FunctionDeclarations) != 1 {
		t.Fatal("tools not converted")
	}

	if len(out.Contents) != 3 {
		t.Fatalf("contents len = %d, want 3", len(out.Contents))
	}

	user := out.Contents[0]
	if user.Role != "user" || len(user.Parts) != 2 {
		t.Fatalf("first user content wrong: role=%q parts=%d", user.Role, len(user.Parts))
	}
	if user.Parts[1].InlineData == nil {
		t.Fatal("image block should become inlineData")
	}
	if user.Parts[1].InlineData.MimeType != "image/png" || user.Parts[1].InlineData.Data != "aGVsbG8=" {
		t.Errorf("inlineData = %+v", user.Parts[1].InlineData)
	}

	model := out.Contents[1]
	if model.Role != "model" || model.Parts[1].FunctionCall == nil {
		t.Fatalf("assistant tool_use not converted: %+v", model)
	}

	toolResult := out.Contents[2]
	if toolResult.Parts[0].FunctionResponse == nil {
		t.Fatal("tool_result not converted")
	}
	if toolResult.Parts[0].FunctionResponse.Name != "lookup" {
		t.Errorf("function response name = %q", toolResult.Parts[0].FunctionResponse.Name)
	}
}

func TestDeepVConvertImageURL(t *testing.T) {
	p := NewDeepVProvider(&Config{Name: "deepv", URL: "https://example.com/v1/chat/messages"}, "", "deepseek-v4-flash-vision-exp")

	raw := `{
		"model": "deepseek-v4-flash-vision-exp",
		"messages": [{"role":"user","content":[
			{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,QUJD"}},
			{"type":"image_url","image_url":{"url":"https://cdn.example.com/pic.png"}}
		]}]
	}`
	var req Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := p.convertRequest(&req)
	if err != nil {
		t.Fatalf("convertRequest: %v", err)
	}
	parts := out.Contents[0].Parts
	if len(parts) != 2 {
		t.Fatalf("parts = %d", len(parts))
	}
	if parts[0].InlineData == nil || parts[0].InlineData.MimeType != "image/jpeg" || parts[0].InlineData.Data != "QUJD" {
		t.Errorf("data URI part = %+v", parts[0])
	}
	if parts[1].FileData == nil || parts[1].FileData.FileURI != "https://cdn.example.com/pic.png" || parts[1].FileData.MimeType != "image/png" {
		t.Errorf("remote URL part = %+v", parts[1])
	}
}

func TestDeepVParseResponse(t *testing.T) {
	p := NewDeepVProvider(&Config{Name: "deepv", URL: "https://example.com/v1/chat/messages"}, "", "deepseek-v4-flash")

	body := `{
		"candidates": [{
			"content": {"parts": [
				{"text": "hello "},
				{"text": "world"},
				{"functionCall": {"id":"fc_1","name":"lookup","args":{"q":"1"}}}
			]},
			"finishReason": "TOOL_CALL"
		}],
		"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 7}
	}`

	resp, err := p.parseResponse([]byte(body), "deepseek-v4-flash")
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("stop_reason = %q", resp.StopReason)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 7 {
		t.Errorf("usage = %+v", resp.Usage)
	}
	if len(resp.Content) != 3 {
		t.Fatalf("content len = %d", len(resp.Content))
	}
	if resp.Content[1].Type != "text" || resp.Content[1].Text != "world" {
		t.Errorf("content[1] = %+v", resp.Content[1])
	}
	if resp.Content[2].Type != "tool_use" || resp.Content[2].Name != "lookup" {
		t.Errorf("content[2] = %+v", resp.Content[2])
	}
	if !strings.Contains(string(resp.Content[2].Input), `"q":"1"`) {
		t.Errorf("tool input = %s", resp.Content[2].Input)
	}
}

func TestDeepVParseResponseImage(t *testing.T) {
	p := NewDeepVProvider(&Config{Name: "deepv", URL: "https://example.com/v1/chat/messages"}, "", "deepseek-v4-flash-vision-exp")
	body := `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"QUJD"}}]},"finishReason":"STOP"}]}`
	resp, err := p.parseResponse([]byte(body), "deepseek-v4-flash-vision-exp")
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if len(resp.Content) != 1 || resp.Content[0].Type != "image" {
		t.Fatalf("content = %+v", resp.Content)
	}
	var source struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	}
	if err := json.Unmarshal(resp.Content[0].Source, &source); err != nil {
		t.Fatalf("source unmarshal: %v", err)
	}
	if source.Type != "base64" || source.MediaType != "image/png" || source.Data != "QUJD" {
		t.Errorf("source = %+v", source)
	}
}

func TestDeepVIsHealthyWithoutToken(t *testing.T) {
	// 默认 HOME 下有文件时可能误判；这里只验证不 panic 且返回 bool。
	p := NewDeepVProvider(&Config{Name: "deepv", URL: "https://example.com/v1/chat/messages"}, "", "deepseek-v4-flash")
	_ = p.IsHealthy(context.Background())
}
