package assistant

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelSelectionMetadata(t *testing.T) {
	a, err := New(Config{
		Model: "m-default", Models: []string{"m-alt", "m-default", "m-default"},
		BaseURL: "http://127.0.0.1:1", APIKey: "k",
	}, newTestMemStore(t), log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	if a.DefaultModel() != "m-default" {
		t.Fatal("default model:", a.DefaultModel())
	}
	got := a.Models()
	if len(got) != 2 || got[0] != "m-default" || got[1] != "m-alt" {
		t.Fatal("models should be deduped:", got)
	}
}

func TestChatRejectsUnknownModel(t *testing.T) {
	a, err := New(Config{
		Model: "m-default", Models: []string{"m-alt"},
		BaseURL: "http://127.0.0.1:1", APIKey: "k",
	}, newTestMemStore(t), log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Chat(context.Background(), "hi", "m-nope", nil)
	if err == nil || !strings.Contains(err.Error(), "m-nope") || !strings.Contains(err.Error(), "m-alt") {
		t.Fatal("expected unknown-model error listing available models, got:", err)
	}
}

func TestGatewayStatusAndLogsTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/admin/dashboard":
			w.Write([]byte(`{"summary":{"today_requests":12,"success_rate":91.7,"avg_latency_ms":850,"cache_hit_rate":55.5,"uptime":"2h"},"providers":[{"name":"alpha","status":"online","state":"closed","requests":9,"errors":1,"avg_latency":500},{"name":"beta","status":"offline","state":"open","requests":3,"errors":3,"avg_latency":4000}]}`))
		case "/admin/logs":
			w.Write([]byte(`{"logs":[
				{"timestamp":"t1","model":"coding","provider":"alpha","status_code":200,"latency_ms":300},
				{"timestamp":"t2","model":"coding","provider":"beta","status_code":502,"latency_ms":4000,"error":"上游 502"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	status := newGatewayStatusTool(srv.URL, "k").(*gatewayStatusTool)
	res, err := status.Execute(context.Background(), []byte("{}"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"alpha", "beta", "熔断器=open", "91.7%", "缓存命中 55.5%"} {
		if !strings.Contains(res.Content, want) {
			t.Fatalf("status tool missing %q in:\n%s", want, res.Content)
		}
	}

	logs := newGatewayLogsTool(srv.URL, "k").(*gatewayLogsTool)
	all, err := logs.Execute(context.Background(), []byte(`{"limit":10}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(all.Content, "HTTP 200") || !strings.Contains(all.Content, "上游 502") {
		t.Fatalf("logs tool missing entries:\n%s", all.Content)
	}
	failures, err := logs.Execute(context.Background(), []byte(`{"failures_only":true}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(failures.Content, "HTTP 200") || !strings.Contains(failures.Content, "上游 502") {
		t.Fatalf("failures_only should keep only the failed entry:\n%s", failures.Content)
	}

	// 未授权时工具返回 IsError 而不是 panic。
	bad := newGatewayStatusTool(srv.URL, "wrong").(*gatewayStatusTool)
	res, err = bad.Execute(context.Background(), []byte("{}"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected IsError on unauthorized fetch")
	}
}
