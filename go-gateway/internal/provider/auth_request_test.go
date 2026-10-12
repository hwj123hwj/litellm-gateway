package provider

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

// sendFailTransport 首次请求模拟复用已被对端关闭的 keep-alive 连接：
// completeWrite=false 时请求体写完前 broken pipe（可安全重发）；
// completeWrite=true 时先触发 WroteRequest 再 connection reset（上游可能已收到，不重发）。
type sendFailTransport struct {
	calls         int
	completeWrite bool
}

func (t *sendFailTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls++
	if t.calls > 1 {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("{}")),
		}, nil
	}
	if t.completeWrite {
		if tr := httptrace.ContextClientTrace(req.Context()); tr != nil && tr.WroteRequest != nil {
			tr.WroteRequest(httptrace.WroteRequestInfo{})
		}
		return nil, &url.Error{Op: "Post", URL: req.URL.String(), Err: &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}}
	}
	return nil, &url.Error{Op: "Post", URL: req.URL.String(), Err: &net.OpError{Op: "write", Net: "tcp", Err: syscall.EPIPE}}
}

func TestPostWithAuthRetriesIncompleteSend(t *testing.T) {
	transport := &sendFailTransport{}
	client := &http.Client{Transport: transport}

	resp, err := postWithAuth(context.Background(), client, "glm", "https://example.invalid/v1/chat/completions", &Config{Name: "glm"}, func(*http.Request) {}, []byte("{}"))
	if err != nil {
		t.Fatalf("broken pipe before full write should retry once and succeed: %v", err)
	}
	resp.Body.Close()
	if transport.calls != 2 {
		t.Fatalf("calls = %d, want exactly one immediate retry", transport.calls)
	}
}

func TestPostWithAuthDoesNotRetryAfterFullWrite(t *testing.T) {
	transport := &sendFailTransport{completeWrite: true}
	client := &http.Client{Transport: transport}

	_, err := postWithAuth(context.Background(), client, "glm", "https://example.invalid/v1/chat/completions", &Config{Name: "glm"}, func(*http.Request) {}, []byte("{}"))
	if err == nil || !strings.Contains(err.Error(), "send request") {
		t.Fatalf("reset after full write must surface the error: %v", err)
	}
	if transport.calls != 1 {
		t.Fatalf("calls = %d, want no retry after the request was fully written", transport.calls)
	}
}
