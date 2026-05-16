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
