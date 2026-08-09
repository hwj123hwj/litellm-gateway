package archive

import (
	"testing"
	"time"
)

func TestArchiverDisabledIsNoop(t *testing.T) {
	// When Enabled=false, Submit should never touch the store.
	a := NewArchiver(NoopStore{}, Config{Enabled: false}, nil)
	a.Submit(Archive{
		RequestID:    "test-1",
		RequestBody:  `{"hi":1}`,
		ResponseBody: `{"bye":1}`,
	})
	a.Close() // should not block / should not panic
}

func TestArchiverEnabledPersistsAsync(t *testing.T) {
	store := &captureStore{}
	a := NewArchiver(store, Config{Enabled: true, MaxBodyKB: 64}, nil)

	a.Submit(Archive{
		RequestID:    "async-1",
		RequestBody:  `{"model":"x"}`,
		ResponseBody: `{"content":"hi"}`,
	})
	a.Close()
	// A handler that finishes after shutdown must not panic when it submits.
	a.Submit(Archive{RequestID: "late"})

	if len(store.saved) != 1 {
		t.Fatalf("expected 1 saved archive, got %d", len(store.saved))
	}
	if store.saved[0].RequestID != "async-1" {
		t.Errorf("saved RequestID = %q, want async-1", store.saved[0].RequestID)
	}
}

func TestArchiverTruncatesOversizedBody(t *testing.T) {
	store := &captureStore{}
	a := NewArchiver(store, Config{Enabled: true, MaxBodyKB: 1}, nil) // 1 KB limit

	big := string(make([]byte, 3000))
	a.Submit(Archive{
		RequestID:   "big",
		RequestBody: big,
	})
	a.Close()

	if len(store.saved) != 1 {
		t.Fatalf("expected 1 saved archive, got %d", len(store.saved))
	}
	body := store.saved[0].RequestBody
	if len(body) > 2048 {
		t.Errorf("body not truncated: len=%d", len(body))
	}
	if !contains(body, "\"truncated\"") {
		t.Errorf("truncated body missing marker")
	}
}

func TestArchiverQueueBackpressurePreservesEveryRecord(t *testing.T) {
	store := &slowStore{block: make(chan struct{})}
	a := NewArchiver(store, Config{Enabled: true, MaxBodyKB: 64}, nil)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 600; i++ {
			a.Submit(Archive{RequestID: "flood"})
		}
		close(done)
	}()

	// Submit must apply backpressure once the queue is full, then continue
	// without dropping records after the store becomes available.
	time.Sleep(20 * time.Millisecond)
	close(store.block)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("archive submissions remained blocked after store recovery")
	}
	a.Close()
	if got := store.saved; got != 600 {
		t.Fatalf("saved archives = %d, want 600", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// captureStore records every SaveArchive call for assertions.
type captureStore struct {
	saved []Archive
}

func (s *captureStore) SaveArchive(a Archive) error {
	s.saved = append(s.saved, a)
	return nil
}
func (s *captureStore) QueryArchives(int, int) ([]Archive, int, error) { return nil, 0, nil }
func (s *captureStore) ExportArchives(int64, time.Time, int) ([]Archive, error) {
	return nil, nil
}
func (s *captureStore) DeleteArchivesBefore(time.Time) (int64, error) { return 0, nil }
func (s *captureStore) DeleteArchive(int64) error                     { return nil }
func (s *captureStore) Close() error                                  { return nil }

// slowStore blocks on a channel for each SaveArchive call. Tests close the
// channel to let the worker drain and Close() to return.
type slowStore struct {
	block chan struct{}
	saved int
}

func (s *slowStore) SaveArchive(Archive) error {
	<-s.block
	s.saved++
	return nil
}
func (s *slowStore) QueryArchives(int, int) ([]Archive, int, error) { return nil, 0, nil }
func (s *slowStore) ExportArchives(int64, time.Time, int) ([]Archive, error) {
	return nil, nil
}
func (s *slowStore) DeleteArchivesBefore(time.Time) (int64, error) { return 0, nil }
func (s *slowStore) DeleteArchive(int64) error                     { return nil }
func (s *slowStore) Close() error                                  { return nil }
