package metrics

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

type lifecycleStore struct {
	started chan struct{}
	release chan struct{}
	saved   atomic.Int32
}

func (s *lifecycleStore) GetRecentLogs(int) ([]RequestRecord, error) { return nil, nil }
func (s *lifecycleStore) SaveRecord(r RequestRecord) error {
	return s.SaveRecordContext(context.Background(), r)
}
func (s *lifecycleStore) SaveRecordContext(ctx context.Context, _ RequestRecord) error {
	close(s.started)
	select {
	case <-s.release:
		s.saved.Add(1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestCollectorCloseWaitsForAcceptedWrites(t *testing.T) {
	c := NewCollector()
	s := &lifecycleStore{started: make(chan struct{}), release: make(chan struct{})}
	c.SetStore(s)
	c.Record(RequestRecord{Timestamp: time.Now(), Method: http.MethodPost, Path: "/v1/messages", StatusCode: 200})
	<-s.started
	closer, ok := any(c).(interface{ Close(context.Context) error })
	if !ok {
		close(s.release)
		t.Fatal("collector has no bounded persistence close")
	}
	done := make(chan error, 1)
	go func() { done <- closer.Close(context.Background()) }()
	select {
	case err := <-done:
		close(s.release)
		t.Fatalf("close returned before write: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(s.release)
	if err := <-done; err != nil || s.saved.Load() != 1 {
		t.Fatalf("err=%v saved=%d", err, s.saved.Load())
	}
}

func TestCollectorCloseCancelsHungWrites(t *testing.T) {
	c := NewCollector()
	s := &lifecycleStore{started: make(chan struct{}), release: make(chan struct{})}
	c.SetStore(s)
	c.Record(RequestRecord{Timestamp: time.Now(), Method: http.MethodPost, Path: "/v1/messages", StatusCode: 200})
	<-s.started
	closer, ok := any(c).(interface{ Close(context.Context) error })
	if !ok {
		close(s.release)
		t.Fatal("collector has no bounded persistence close")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := closer.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close err=%v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := closer.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("write cancellation not drained: %v", err)
	}
}
