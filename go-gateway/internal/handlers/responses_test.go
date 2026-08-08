package handlers

import (
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
