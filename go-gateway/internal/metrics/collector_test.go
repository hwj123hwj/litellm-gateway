package metrics

import (
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
		{RequestID: "newer", Timestamp: time.Now(), ProviderAttempts: []requestmeta.ProviderAttempt{{Provider: "glm", Status: "success"}}},
		{RequestID: "older", Timestamp: time.Now().Add(-time.Minute)},
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
