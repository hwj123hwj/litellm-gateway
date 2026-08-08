package provider

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

type errorPolicyProvider struct {
	name  string
	err   error
	resp  *Response
	calls int
}

func (p *errorPolicyProvider) Name() string                   { return p.name }
func (p *errorPolicyProvider) URL() string                    { return "http://example.com" }
func (p *errorPolicyProvider) APIKey() string                 { return "" }
func (p *errorPolicyProvider) UseBearer() bool                { return true }
func (p *errorPolicyProvider) IsHealthy(context.Context) bool { return true }
func (p *errorPolicyProvider) ForwardRequest(context.Context, *Request) (*Response, error) {
	p.calls++
	return p.resp, p.err
}

func TestProviderHTTPErrorExtractsStatusAndRetryMetadata(t *testing.T) {
	resp := httptest.NewRecorder()
	resp.Header().Set("Retry-After", "12")
	resp.Header().Set("X-Request-ID", "req_123")
	resp.WriteHeader(http.StatusTooManyRequests)
	response := resp.Result()

	err := NewHTTPError("glm", response, []byte(`{"error":{"message":"quota exceeded"}}`))
	if err.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", err.StatusCode)
	}
	if err.Message != "quota exceeded" {
		t.Fatalf("message = %q, want quota exceeded", err.Message)
	}
	if err.RetryAfter != "12" || err.RequestID != "req_123" {
		t.Fatalf("metadata = retry-after %q request-id %q", err.RetryAfter, err.RequestID)
	}
	if !ShouldFallback(err) {
		t.Fatal("429 provider errors should be eligible for fallback")
	}
}

func TestShouldFallbackStopsOnClientErrors(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity} {
		if ShouldFallback(&ProviderError{StatusCode: status}) {
			t.Fatalf("status %d should not fallback", status)
		}
	}
	if !ShouldFallback(errors.New("network unavailable")) {
		t.Fatal("untyped provider/network errors should remain fallback eligible")
	}
}

func TestRouterDoesNotFallbackOnProviderClientError(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	router := NewRouter(logger)
	first := &errorPolicyProvider{
		name: "first",
		err:  &ProviderError{Provider: "first", StatusCode: http.StatusUnauthorized, Message: "invalid key"},
	}
	second := &errorPolicyProvider{name: "second", resp: &Response{Model: "fallback"}}
	router.RegisterProvider(first.name, first)
	router.RegisterProvider(second.name, second)
	router.RegisterChain("coding", []string{first.name, second.name})

	_, err := router.Forward(context.Background(), "coding", &Request{Model: "coding"})
	if err == nil {
		t.Fatal("expected the provider error to be returned")
	}
	if second.calls != 0 {
		t.Fatal("must not fallback on provider authentication errors")
	}
}

func TestRouterFallsBackOnRateLimit(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	router := NewRouter(logger)
	first := &errorPolicyProvider{
		name: "first",
		err:  &ProviderError{Provider: "first", StatusCode: http.StatusTooManyRequests, Message: "quota exceeded"},
	}
	second := &errorPolicyProvider{name: "second", resp: &Response{Model: "fallback"}}
	router.RegisterProvider(first.name, first)
	router.RegisterProvider(second.name, second)
	router.RegisterChain("coding", []string{first.name, second.name})

	resp, err := router.Forward(context.Background(), "coding", &Request{Model: "coding"})
	if err != nil || resp == nil || resp.Model != "fallback" {
		t.Fatalf("expected fallback response, got resp=%#v err=%v", resp, err)
	}
	if second.calls != 1 {
		t.Fatalf("expected fallback provider to be called once, got %d", second.calls)
	}
}
