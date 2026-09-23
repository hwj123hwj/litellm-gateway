package provider

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 端到端验证：链首档返回 403（账号无该模型权限）时，必须降级到下一档并成功，
// 而不是把 403 抛给客户端。这是「持续降级」的核心保证。
func TestChainDegradesPastForbiddenToNextTier(t *testing.T) {
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"message":"Access to model denied"}}`)
	}))
	defer forbidden.Close()

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","model":"second","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer ok.Close()

	logger := log.New(io.Discard, "", 0)
	router := NewRouter(logger)

	first := NewOpenAIProvider(&Config{Name: "tier1", URL: forbidden.URL, APIKey: "k"})
	second := NewOpenAIProvider(&Config{Name: "tier2", URL: ok.URL, APIKey: "k"})
	router.RegisterProvider("tier1", NewBoundModelProviderWrapper(first, "model-a"))
	router.RegisterProvider("tier2", NewBoundModelProviderWrapper(second, "model-b"))
	router.RegisterChain("coding", []string{"tier1", "tier2"})

	resp, providerName, attempts, err := router.ForwardWithDetails(context.Background(), "coding", &Request{Model: "coding"})
	if err != nil {
		t.Fatalf("chain must degrade past 403, got error: %v", err)
	}
	if resp == nil || resp.Model != "second" {
		t.Fatalf("expected second tier response, got %#v", resp)
	}
	if providerName != "tier2" {
		t.Fatalf("final provider = %q, want tier2", providerName)
	}
	if len(attempts) != 2 {
		t.Fatalf("attempts = %d, want 2 (403 then success)", len(attempts))
	}
}

// 反向保证：400（请求本身不合法）不降级——换 provider 也是同样的请求错误。
func TestChainDoesNotDegradeOnBadRequest(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid schema"}}`)
	}))
	defer bad.Close()

	var secondCalled bool
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","choices":[],"usage":{}}`)
	}))
	defer second.Close()

	logger := log.New(io.Discard, "", 0)
	router := NewRouter(logger)
	router.RegisterProvider("tier1", NewBoundModelProviderWrapper(
		NewOpenAIProvider(&Config{Name: "tier1", URL: bad.URL, APIKey: "k"}), "model-a"))
	router.RegisterProvider("tier2", NewBoundModelProviderWrapper(
		NewOpenAIProvider(&Config{Name: "tier2", URL: second.URL, APIKey: "k"}), "model-b"))
	router.RegisterChain("coding", []string{"tier1", "tier2"})

	if _, err := router.Forward(context.Background(), "coding", &Request{Model: "coding"}); err == nil {
		t.Fatal("expected the 400 error to surface")
	}
	if secondCalled {
		t.Fatal("400 must not degrade to the next tier")
	}
}
