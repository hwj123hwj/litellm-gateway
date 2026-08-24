package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/provider"
)

func TestResponsesToProviderRequestPreservesMaxOutputTokens(t *testing.T) {
	req := &responsesRequest{
		Model:           "coding",
		Input:           []byte(`"hello"`),
		MaxOutputTokens: 16,
	}

	providerReq, err := responsesToProviderRequest(req)
	if err != nil {
		t.Fatalf("responsesToProviderRequest() error = %v", err)
	}
	if providerReq.MaxTokens != 16 {
		t.Fatalf("MaxTokens = %d, want 16", providerReq.MaxTokens)
	}
	raw, ok := providerReq.RawField("max_tokens")
	if !ok {
		t.Fatal("expected max_tokens to be present in the provider payload")
	}
	if string(raw) != "16" {
		t.Fatalf("raw max_tokens = %s, want 16", raw)
	}
}

func TestResponsesHandlerPreservesProviderStatusWhenAllStreamsFail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := log.New(io.Discard, "", 0)
	router := provider.NewRouter(logger)
	first := &stubChatProvider{
		name:      "first",
		streamErr: &provider.ProviderError{Provider: "first", StatusCode: http.StatusForbidden, Message: "account disabled"},
	}
	second := &stubChatProvider{
		name:      "second",
		streamErr: errors.New("network unavailable"),
	}
	router.RegisterProvider(first.name, first)
	router.RegisterProvider(second.name, second)
	router.RegisterChain("coding", []string{first.name, second.name})

	engine := gin.New()
	engine.POST("/v1/responses", NewResponsesHandler(router, logger).Handle)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"coding","input":"hello","stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected upstream 403 without fallback, got %d: %s", w.Code, w.Body.String())
	}
}

func TestResponsesHandlerSupportsNonStreamChatGPTPassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := log.New(io.Discard, "", 0)
	router := provider.NewRouter(logger)
	stub := &nonStreamChatGPTStub{streamBody: strings.Join([]string{
		"event: response.output_item.done",
		`data: {"type":"response.output_item.done","item":{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"OK"}]}}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","model":"gpt-5.6-luna","output":[],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`,
		"",
	}, "\n")}
	router.RegisterProvider("chatgpt", stub)
	router.RegisterChain("gpt-luna", []string{"chatgpt"})

	engine := gin.New()
	engine.POST("/v1/responses", NewResponsesHandler(router, logger).Handle)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-luna","input":"connection test","stream":false}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var response struct {
		Status string `json:"status"`
		Output []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("response is invalid JSON: %v", err)
	}
	if response.Status != "completed" || len(response.Output) != 1 || response.Output[0].Content[0].Text != "OK" {
		t.Fatalf("response = %+v, want completed response with OK", response)
	}
}

type nonStreamChatGPTStub struct {
	streamBody string
}

func (p *nonStreamChatGPTStub) Name() string                   { return "chatgpt" }
func (p *nonStreamChatGPTStub) URL() string                    { return "http://chatgpt" }
func (p *nonStreamChatGPTStub) APIKey() string                 { return "" }
func (p *nonStreamChatGPTStub) UseBearer() bool                { return true }
func (p *nonStreamChatGPTStub) IsHealthy(context.Context) bool { return true }
func (p *nonStreamChatGPTStub) ForwardRequest(context.Context, *provider.Request) (*provider.Response, error) {
	return nil, errors.New("not used")
}
func (p *nonStreamChatGPTStub) ForwardRawResponsesStream(_ context.Context, _ json.RawMessage, w io.Writer) error {
	_, err := io.WriteString(w, p.streamBody)
	return err
}
