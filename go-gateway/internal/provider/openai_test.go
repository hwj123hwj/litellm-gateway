package provider

import (
	"encoding/json"
	"testing"
)

func TestToOpenAIRequestConvertsAnthropicTools(t *testing.T) {
	req := &Request{
		Model:     "coding",
		Messages:  []Message{{Role: "assistant", Content: NewBlocksContent([]ContentBlock{{Type: "text", Text: "hello"}, {Type: "tool_use", ID: "call_1", Name: "weather", Input: json.RawMessage(`{"city":"Beijing"}`)}})}},
		MaxTokens: 128,
	}
	if err := req.SetRawField("tools", []map[string]any{{
		"name":         "weather",
		"description":  "Get weather",
		"input_schema": json.RawMessage(`{"type":"object"}`),
	}}); err != nil {
		t.Fatalf("set raw tools: %v", err)
	}

	oai := toOpenAIRequest(req)
	if oai.Model != "coding" {
		t.Fatalf("expected model coding, got %s", oai.Model)
	}
	if len(oai.Messages) != 1 {
		t.Fatalf("expected one message, got %d", len(oai.Messages))
	}
	if len(oai.Messages[0].ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(oai.Messages[0].ToolCalls))
	}
	if len(oai.Tools) != 1 || oai.Tools[0].Function.Name != "weather" {
		t.Fatalf("expected one converted tool, got %#v", oai.Tools)
	}
}

func TestToOpenAIRequestPreservesToolChoice(t *testing.T) {
	req := &Request{Model: "coding"}
	if err := req.SetRawField("tool_choice", map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": "openwiki_read_personal_history_batch",
		},
	}); err != nil {
		t.Fatalf("set raw tool_choice: %v", err)
	}

	oai := toOpenAIRequest(req)
	var choice struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(oai.ToolChoice, &choice); err != nil {
		t.Fatalf("unmarshal forwarded tool_choice: %v", err)
	}
	if choice.Type != "function" || choice.Function.Name != "openwiki_read_personal_history_batch" {
		t.Fatalf("expected tool_choice to be preserved, got %#v", choice)
	}
}

func TestToOpenAIRequestPreservesMultimodalAndThinkingFields(t *testing.T) {
	imageBlock := json.RawMessage(`{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}`)
	req := &Request{
		Model: "glm-vision",
		Messages: []Message{{
			Role: "user",
			Content: NewBlocksContent([]ContentBlock{
				{Type: "text", Text: "What is in this image?"},
				{Type: "image_url", ImageURL: json.RawMessage(`{"url":"data:image/png;base64,abc"}`), Raw: imageBlock},
			}),
		}},
	}
	if err := req.SetRawField("extra_body", map[string]any{
		"thinking": map[string]any{"type": "disabled"},
	}); err != nil {
		t.Fatalf("set extra_body: %v", err)
	}
	if err := req.SetRawField("thinking", map[string]any{"type": "disabled"}); err != nil {
		t.Fatalf("set thinking: %v", err)
	}

	oai := toOpenAIRequest(req)
	payload, err := json.Marshal(oai)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var decoded struct {
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
		ExtraBody map[string]any `json:"extra_body"`
		Thinking  map[string]any `json:"thinking"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if len(decoded.Messages) != 1 || len(decoded.Messages[0].Content) != 2 {
		t.Fatalf("expected text and image content parts, got %s", payload)
	}
	if decoded.Messages[0].Content[1]["type"] != "image_url" {
		t.Fatalf("expected image_url block, got %#v", decoded.Messages[0].Content[1])
	}
	if decoded.ExtraBody["thinking"] == nil || decoded.Thinking["type"] != "disabled" {
		t.Fatalf("expected thinking fields to be preserved, got %s", payload)
	}
}

func TestToOpenAIRequestPreservesUnknownTopLevelFields(t *testing.T) {
	req := &Request{Model: "vendor-model", Messages: []Message{{
		Role: "user", Content: NewStringContent("hello"),
	}}}
	if err := req.SetRawField("vendor_extension", map[string]any{"mode": "precise"}); err != nil {
		t.Fatalf("set vendor field: %v", err)
	}
	if err := req.SetRawField("temperature", 0); err != nil {
		t.Fatalf("set zero temperature: %v", err)
	}

	payload, err := json.Marshal(toOpenAIRequest(req))
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if decoded["vendor_extension"].(map[string]any)["mode"] != "precise" {
		t.Fatalf("unknown extension was lost: %s", payload)
	}
	if value, exists := decoded["temperature"]; !exists || value != float64(0) {
		t.Fatalf("explicit zero value was lost: %s", payload)
	}
}

func TestOpenAIContentPartNormalizesResponsesInputImage(t *testing.T) {
	part := openAIContentPart(ContentBlock{
		Type: "image_url",
		Raw:  json.RawMessage(`{"type":"input_image","image_url":"https://example.test/cat.png","detail":"high"}`),
	})
	var decoded struct {
		Type     string `json:"type"`
		ImageURL struct {
			URL    string `json:"url"`
			Detail string `json:"detail"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(part, &decoded); err != nil {
		t.Fatalf("decode normalized image: %v", err)
	}
	if decoded.Type != "image_url" || decoded.ImageURL.URL != "https://example.test/cat.png" || decoded.ImageURL.Detail != "high" {
		t.Fatalf("unexpected normalized image part: %s", part)
	}
}

func TestOpenAIContentPartConvertsAnthropicBase64Image(t *testing.T) {
	part := openAIContentPart(ContentBlock{
		Type:   "image",
		Source: json.RawMessage(`{"type":"base64","media_type":"image/png","data":"abc"}`),
	})
	var decoded map[string]any
	if err := json.Unmarshal(part, &decoded); err != nil {
		t.Fatalf("decode image part: %v", err)
	}
	if decoded["type"] != "image_url" {
		t.Fatalf("expected image_url, got %s", part)
	}
	imageURL := decoded["image_url"].(map[string]any)["url"]
	if imageURL != "data:image/png;base64,abc" {
		t.Fatalf("unexpected converted URL: %#v", imageURL)
	}
}

func TestFromOpenAIResponseConvertsToolCalls(t *testing.T) {
	resp := fromOpenAIResponse(&openAIResponse{
		ID:    "chatcmpl_1",
		Model: "glm-5-turbo",
		Choices: []openAIChoice{{
			Message: openAIMessage{
				Role:    "assistant",
				Content: "",
				ToolCalls: []openAIToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: openAIToolFunction{
						Name:      "weather",
						Arguments: `{"city":"Beijing"}`,
					},
				}},
			},
			FinishReason: "tool_calls",
		}},
		Usage: openAIUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	})

	if resp.StopReason != "tool_use" {
		t.Fatalf("expected stop_reason tool_use, got %s", resp.StopReason)
	}
	if len(resp.Content) != 1 || resp.Content[0].Type != "tool_use" {
		t.Fatalf("expected tool_use block, got %#v", resp.Content)
	}
	if string(resp.Content[0].Input) != `{"city":"Beijing"}` {
		t.Fatalf("expected parsed tool input, got %s", string(resp.Content[0].Input))
	}
}
