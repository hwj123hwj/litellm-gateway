package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDeepVProviderCapabilities(t *testing.T) {
	p := NewDeepVProvider(&Config{Name: "deepv-deepseek-flash", URL: "https://example.com/v1/chat/messages"}, "", "deepseek-flash")
	for _, c := range []string{CapabilityText, CapabilityVision, CapabilityToolCall, CapabilityStreaming, CapabilityReasoning} {
		if !hasCapability(p.Capabilities(), c) {
			t.Errorf("missing capability %q", c)
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
	p := NewDeepVProvider(&Config{Name: "deepv", URL: "https://example.com/v1/chat/messages"}, "", "deepseek-v4.1-flash")

	raw := `{
		"model": "deepseek-v4.1-flash",
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

	if out.Model != "deepseek-v4.1-flash" {
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
	// 缺思维链的工具调用轮会被注入占位 reasoning part，排在其他 part 之前。
	if model.Role != "model" || model.Parts[0].Reasoning != placeholderReasoning {
		t.Fatalf("placeholder reasoning not injected: %+v", model)
	}
	if model.Parts[2].FunctionCall == nil {
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

// TestDeepVToolCallTurnKeepsRealReasoning 验证：带真实思维链的工具调用轮
// 原样回传，不被占位文本覆盖；纯文本轮不注入占位。
func TestDeepVToolCallTurnKeepsRealReasoning(t *testing.T) {
	p := NewDeepVProvider(&Config{Name: "deepv", URL: "https://example.com/v1/chat/messages"}, "", "deepseek-v4.1-flash")

	raw := `{
		"model": "deepseek-v4.1-flash",
		"messages": [
			{"role":"user","content":[{"type":"text","text":"go"}]},
			{"role":"assistant","content":[
				{"type":"text","text":"plain answer, no tools"}
			]},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"real chain"},
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
	if len(out.Contents) != 4 {
		t.Fatalf("contents len = %d, want 4", len(out.Contents))
	}

	plain := out.Contents[1]
	if plain.Parts[0].Reasoning != "" || plain.Parts[0].Text != "plain answer, no tools" {
		t.Errorf("plain text turn should not get placeholder: %+v", plain.Parts)
	}

	withTools := out.Contents[2]
	if withTools.Parts[0].Reasoning != "real chain" {
		t.Errorf("real reasoning should be preserved, got %q", withTools.Parts[0].Reasoning)
	}
}

func TestDeepVConvertImageURL(t *testing.T) {
	p := NewDeepVProvider(&Config{Name: "deepv", URL: "https://example.com/v1/chat/messages"}, "", "deepseek-v4.1-flash")

	raw := `{
		"model": "deepseek-v4.1-flash",
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
	p := NewDeepVProvider(&Config{Name: "deepv", URL: "https://example.com/v1/chat/messages"}, "", "deepseek-v4.1-flash")

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

	resp, err := p.parseResponse([]byte(body), "deepseek-v4.1-flash")
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
	p := NewDeepVProvider(&Config{Name: "deepv", URL: "https://example.com/v1/chat/messages"}, "", "deepseek-v4.1-flash")
	body := `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"QUJD"}}]},"finishReason":"STOP"}]}`
	resp, err := p.parseResponse([]byte(body), "deepseek-v4.1-flash")
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
	p := NewDeepVProvider(&Config{Name: "deepv", URL: "https://example.com/v1/chat/messages"}, "", "deepseek-v4.1-flash")
	_ = p.IsHealthy(context.Background())
}

// TestDeepVToolResultImageForwarding 验证工具结果里的图片块会转成
// inlineData part 随 functionResponse 一起上送，供多模态模型读图。
func TestDeepVToolResultImageForwarding(t *testing.T) {
	p := NewDeepVProvider(&Config{Name: "deepv", URL: "https://example.com/v1/chat/messages"}, "", "deepseek-v4.1-flash")

	raw := `{
		"model": "deepseek-v4.1-flash",
		"messages": [
			{"role":"user","content":[{"type":"text","text":"read it"}]},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"need the file"},
				{"type":"tool_use","id":"tu_1","name":"Read","input":{"path":"a.png"}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"tu_1","content":[
					{"type":"text","text":"png file"},
					{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJD"}}
				]}
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

	toolTurn := out.Contents[2]
	if toolTurn.Role != "user" {
		t.Fatalf("tool result turn role = %q", toolTurn.Role)
	}
	assistantTurn := out.Contents[1]
	if assistantTurn.Parts[0].Reasoning != "need the file" {
		t.Errorf("reasoning before tool_use missing: %+v", assistantTurn.Parts[0])
	}
	fr := toolTurn.Parts[0].FunctionResponse
	if fr == nil || fr.Name != "Read" || fr.Response["result"] != "png file" {
		t.Fatalf("functionResponse wrong: %+v", fr)
	}
	if len(toolTurn.Parts) < 2 || toolTurn.Parts[1].InlineData == nil {
		t.Fatalf("tool result image not forwarded as inlineData: %+v", toolTurn.Parts)
	}
	if toolTurn.Parts[1].InlineData.MimeType != "image/png" || toolTurn.Parts[1].InlineData.Data != "QUJD" {
		t.Errorf("inlineData = %+v", toolTurn.Parts[1].InlineData)
	}
}
