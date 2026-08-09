package metrics

import (
	"net/http"
	"testing"
	"time"

	"github.com/weijian/go-llm-gateway/internal/requestmeta"
)

type memoryStore struct {
	records []RequestRecord
}

func (s *memoryStore) SaveRecord(RequestRecord) error { return nil }
func (s *memoryStore) GetRecentLogs(int) ([]RequestRecord, error) {
	return append([]RequestRecord(nil), s.records...), nil
}

func TestCollectorRestoresRecentLogsFromStore(t *testing.T) {
	store := &memoryStore{records: []RequestRecord{
		{RequestID: "newer", Timestamp: time.Now(), Method: http.MethodPost, Path: "/v1/chat/completions", ProviderAttempts: []requestmeta.ProviderAttempt{{Provider: "glm", Status: "success"}}},
		{RequestID: "older", Timestamp: time.Now().Add(-time.Minute), Method: http.MethodPost, Path: "/v1/messages"},
		{RequestID: "favicon", Timestamp: time.Now(), Method: http.MethodGet, Path: "/favicon.ico"},
	}}
	collector := NewCollector()
	collector.SetStore(store)

	records := collector.GetRecentLogs(2)
	if len(records) != 2 || records[0].RequestID != "newer" || records[1].RequestID != "older" {
		t.Fatalf("restored records = %#v", records)
	}
	if len(records[0].ProviderAttempts) != 1 {
		t.Fatalf("restored attempts = %#v", records[0].ProviderAttempts)
	}
}

func TestCollectorOnlyRecordsModelAPIRequests(t *testing.T) {
	collector := NewCollector()
	now := time.Now()
	for _, record := range []RequestRecord{
		{Timestamp: now, Method: http.MethodGet, Path: "/favicon.ico", StatusCode: http.StatusNoContent},
		{Timestamp: now, Method: http.MethodGet, Path: "/v1/models", StatusCode: http.StatusOK},
		{Timestamp: now, Method: http.MethodGet, Path: "/admin/dashboard", StatusCode: http.StatusOK},
		{Timestamp: now, Method: http.MethodPost, Path: "/v1/chat/completions", Model: "coding", StatusCode: http.StatusOK},
	} {
		collector.Record(record)
	}

	summary := collector.GetDashboard()
	if summary.TodayRequests != 1 || summary.SuccessRate != 100 || summary.ActiveModels != 1 {
		t.Fatalf("summary = %#v, want one successful model API request", summary)
	}
	logs := collector.GetRecentLogs(10)
	if len(logs) != 1 || logs[0].Path != "/v1/chat/completions" {
		t.Fatalf("business logs = %#v, want only model API request", logs)
	}
}

func TestIsBusinessRequestRecognizesAllModelProtocols(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   bool
	}{
		{method: http.MethodPost, path: "/v1/messages", want: true},
		{method: http.MethodPost, path: "/messages", want: true},
		{method: http.MethodPost, path: "/v1/chat/completions", want: true},
		{method: http.MethodPost, path: "/chat/completions", want: true},
		{method: http.MethodPost, path: "/v1/responses", want: true},
		{method: http.MethodPost, path: "/responses", want: true},
		{method: http.MethodGet, path: "/v1/chat/completions", want: false},
		{method: http.MethodGet, path: "/", want: false},
		{method: http.MethodGet, path: "/favicon.ico", want: false},
	}
	for _, test := range tests {
		if got := IsBusinessRequest(test.method, test.path); got != test.want {
			t.Errorf("IsBusinessRequest(%q, %q) = %v, want %v", test.method, test.path, got, test.want)
		}
	}
}
