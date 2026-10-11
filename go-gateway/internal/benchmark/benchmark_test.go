package benchmark

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestBenchmarkStopsOn429AndMeasuresActualOutput(t *testing.T) {
	var limited atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing auth")
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "" && string(body)[0] != '{' {
			t.Error("invalid JSON")
		}
		if limited.Add(1) == 1 {
			w.WriteHeader(429)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"message_start\"}\n\n: ping\n\n")
		w.(http.Flusher).Flush()
		time.Sleep(20 * time.Millisecond)
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"OK\"}}\n\n")
		w.(http.Flusher).Flush()
		time.Sleep(10 * time.Millisecond)
		fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":2}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()
	results, err := Run(context.Background(), server.Client(), server.URL, "test-key", []string{"limited", "good"}, 3, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(results[0].Samples) != 1 || !results[0].StoppedOnRateLimit || results[0].SuccessRate != 0 {
		t.Fatalf("limited=%+v", results[0])
	}
	good := results[1]
	if good.SuccessRate != 1 || len(good.Samples) != 3 {
		t.Fatalf("good=%+v", good)
	}
	for _, s := range good.Samples {
		if s.FirstOutputMs == nil || *s.FirstOutputMs < 15 || s.OutputTokens != 2 || s.TokensPerSecond == nil {
			t.Fatalf("timing=%+v", s)
		}
	}
}
func TestBenchmarkRejectsTruncatedStreamAndTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"message_start\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	results, err := Run(context.Background(), server.Client(), server.URL, "test-key", []string{"slow"}, 1, 30*time.Millisecond)
	if err != nil || results[0].Samples[0].Success || results[0].Samples[0].Error == "" {
		t.Fatalf("%+v err=%v", results, err)
	}
}
