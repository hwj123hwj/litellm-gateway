package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/provider"
)

type stubChatProvider struct {
	name       string
	resp       *provider.Response
	streamData string
	lastReq    *provider.Request
}

func (p *stubChatProvider) Name() string                    { return p.name }
func (p *stubChatProvider) URL() string                     { return "http://example.com" }
func (p *stubChatProvider) APIKey() string                  { return "" }
func (p *stubChatProvider) UseBearer() bool                 { return true }
func (p *stubChatProvider) IsHealthy(_ context.Context) bool { return true }
func (p *stubChatProvider) ForwardRequest(_ context.Context, req *provider.Request) (*provider.Response, error) {
	p.lastReq = req
	return p.resp, nil
}
func (p *stubChatProvider) ForwardStream(_ context.Context, req *provider.Request, w io.Writer) error {
	p.lastReq = req
	_, err := io.WriteString(w, p.streamData)
	return err
}

func TestChatCompletionsHandlerReturnsOpenAIResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := log.New(io.Discard, "", 0)
	router := provider.NewRouter(logger)
	router.RegisterChain("coding", []string{"stub"})
	router.RegisterProvider("stub", &stubChatProvider{
		name: "stub",
		resp: &provider.Response{
			ID:         "msg_123",
			Type:       "message",
			Role:       "assistant",
			Model:      "glm-5-turbo",
			StopReason: "end_turn",
			Content: []provider.ContentBlock{
				{Type: "text", Text: "hello from gateway"},
			},
		},
	})

	handler := NewChatCompletionsHandler(router, logger)
	engine := gin.New()
	engine.POST("/v1/chat/completions", handler.Handle)

	body := `{"model":"coding","messages":[{"role":"user","content":"hi"}],"max_tokens":128}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp["object"] != "chat.completion" {
		t.Fatalf("expected object chat.completion, got %#v", resp["object"])
	}

	choices, ok := resp["choices"].([]any)
	if !ok || len(choices) != 1 {
		t.Fatalf("expected 1 choice, got %#v", resp["choices"])
	}

	choice := choices[0].(map[string]any)
	message := choice["message"].(map[string]any)
	if message["content"] != "hello from gateway" {
		t.Fatalf("expected assistant content, got %#v", message["content"])
	}
	if choice["finish_reason"] != "stop" {
		t.Fatalf("expected finish_reason stop, got %#v", choice["finish_reason"])
	}
}

func TestChatCompletionsHandlerConvertsSystemAndToolMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := log.New(io.Discard, "", 0)
	router := provider.NewRouter(logger)
	stub := &stubChatProvider{
		name: "stub",
		resp: &provider.Response{
			ID:         "msg_456",
			Type:       "message",
			Role:       "assistant",
			Model:      "glm-5-turbo",
			StopReason: "tool_use",
			Content: []provider.ContentBlock{
				{Type: "tool_use", ID: "call_1", Name: "get_weather", Input: json.RawMessage(`{"city":"Beijing"}`)},
			},
		},
	}
	router.RegisterProvider("stub", stub)
	router.RegisterChain("coding", []string{"stub"})

	handler := NewChatCompletionsHandler(router, logger)
	engine := gin.New()
	engine.POST("/v1/chat/completions", handler.Handle)

	body := `{
		"model":"coding",
		"messages":[
			{"role":"system","content":"You are helpful"},
			{"role":"user","content":"hello"},
			{"role":"assistant","content":"calling tool","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Beijing\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"sunny"}
		],
		"tools":[{"type":"function","function":{"name":"get_weather","description":"Get weather","parameters":{"type":"object"}}}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if stub.lastReq == nil {
		t.Fatal("expected provider request to be captured")
	}
	if len(stub.lastReq.Messages) != 3 {
		t.Fatalf("expected 3 non-system messages, got %d", len(stub.lastReq.Messages))
	}
	if stub.lastReq.Messages[1].Role != "assistant" {
		t.Fatalf("expected assistant message, got %s", stub.lastReq.Messages[1].Role)
	}
	if len(stub.lastReq.Messages[1].Content.Blocks()) != 2 {
		t.Fatalf("expected assistant text + tool_use blocks, got %d", len(stub.lastReq.Messages[1].Content.Blocks()))
	}
	if stub.lastReq.Messages[2].Role != "user" {
		t.Fatalf("expected tool message to map to user, got %s", stub.lastReq.Messages[2].Role)
	}
	blocks := stub.lastReq.Messages[2].Content.Blocks()
	if len(blocks) != 1 || blocks[0].Type != "tool_result" || blocks[0].ToolUseID != "call_1" {
		t.Fatalf("expected tool_result block, got %#v", blocks)
	}
	if _, ok := stub.lastReq.RawField("system"); !ok {
		t.Fatal("expected system field to be preserved in raw payload")
	}
	if _, ok := stub.lastReq.RawField("tools"); !ok {
		t.Fatal("expected tools field to be preserved in raw payload")
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	choices := resp["choices"].([]any)
	choice := choices[0].(map[string]any)
	message := choice["message"].(map[string]any)
	toolCalls, ok := message["tool_calls"].([]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("expected one tool_call, got %#v", message["tool_calls"])
	}
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("expected finish_reason tool_calls, got %#v", choice["finish_reason"])
	}
}

func TestChatCompletionsHandlerStreamsOpenAISSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := log.New(io.Discard, "", 0)
	router := provider.NewRouter(logger)
	stub := &stubChatProvider{
		name: "stub",
		streamData: strings.Join([]string{
			"event: message_start",
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_789\",\"model\":\"glm-5-turbo\"}}",
			"",
			"event: content_block_start",
			"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}",
			"",
			"event: content_block_delta",
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}",
			"",
			"event: content_block_stop",
			"data: {\"type\":\"content_block_stop\",\"index\":0}",
			"",
			"event: message_delta",
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}",
			"",
			"event: message_stop",
			"data: {\"type\":\"message_stop\"}",
			"",
		}, "\n"),
	}
	router.RegisterProvider("stub", stub)
	router.RegisterChain("coding", []string{"stub"})

	handler := NewChatCompletionsHandler(router, logger)
	engine := gin.New()
	engine.POST("/v1/chat/completions", handler.Handle)

	body := `{"model":"coding","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("expected SSE content-type, got %q", got)
	}
	bodyText := w.Body.String()
	if !strings.Contains(bodyText, `"object":"chat.completion.chunk"`) {
		t.Fatalf("expected OpenAI chunk object, got %s", bodyText)
	}
	if !strings.Contains(bodyText, `"content":"Hello"`) {
		t.Fatalf("expected streamed content chunk, got %s", bodyText)
	}
	if !strings.Contains(bodyText, `"finish_reason":"stop"`) {
		t.Fatalf("expected stop finish_reason, got %s", bodyText)
	}
	if !strings.Contains(bodyText, `data: [DONE]`) {
		t.Fatalf("expected DONE sentinel, got %s", bodyText)
	}
}

func TestChatCompletionsHandlerStreamsToolCalls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := log.New(io.Discard, "", 0)
	router := provider.NewRouter(logger)
	stub := &stubChatProvider{
		name: "stub",
		streamData: strings.Join([]string{
			"event: message_start",
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_999\",\"model\":\"glm-5-turbo\"}}",
			"",
			"event: content_block_start",
			"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_weather\",\"name\":\"get_weather\",\"input\":{}}}",
			"",
			"event: content_block_delta",
			"data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"city\\\":\\\"Beijing\\\"}\"}}",
			"",
			"event: content_block_stop",
			"data: {\"type\":\"content_block_stop\",\"index\":1}",
			"",
			"event: message_delta",
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":10}}",
			"",
			"event: message_stop",
			"data: {\"type\":\"message_stop\"}",
			"",
		}, "\n"),
	}
	router.RegisterProvider("stub", stub)
	router.RegisterChain("coding", []string{"stub"})

	handler := NewChatCompletionsHandler(router, logger)
	engine := gin.New()
	engine.POST("/v1/chat/completions", handler.Handle)

	body := `{"model":"coding","stream":true,"messages":[{"role":"user","content":"what is weather"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	bodyText := w.Body.String()
	if !strings.Contains(bodyText, `"index":0`) {
		t.Fatalf("expected zero-based tool_call index 0, got %s", bodyText)
	}
	if !strings.Contains(bodyText, `"name":"get_weather"`) {
		t.Fatalf("expected tool name get_weather, got %s", bodyText)
	}
	if strings.Contains(bodyText, `"name":""`) {
		t.Fatalf("arguments-only tool chunks must omit the name field, got %s", bodyText)
	}
	if !strings.Contains(bodyText, `"arguments":"{\"city\":\"Beijing\"}"`) {
		t.Fatalf("expected arguments string, got %s", bodyText)
	}
	if !strings.Contains(bodyText, `"finish_reason":"tool_calls"`) {
		t.Fatalf("expected finish_reason tool_calls, got %s", bodyText)
	}
}
