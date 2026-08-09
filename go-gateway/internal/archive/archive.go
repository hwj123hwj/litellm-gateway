package archive

import (
	"encoding/json"
	"io"
	"log"
	"sync"
	"time"
)

// Archiver is the entry point handlers call to persist conversations. It owns
// a bounded async worker goroutine so archival never blocks the request path,
// even when the SQLite write is slow. When Config.Enabled is false, Submit is a
// no-op and the worker goroutine is never started.
type Archiver struct {
	store Store
	cfg   Config
	log   *log.Logger

	queue chan Archive
	wg    sync.WaitGroup
}

// NewArchiver constructs an Archiver. If cfg.Enabled is false, the returned
// Archiver is a safe no-op (Submit returns immediately, store is never touched).
// Otherwise a background worker is started that drains the queue and writes to
// store. The queue is bounded (buffer 512); if it saturates, additional
// submissions are dropped with a log warning rather than blocking callers.
func NewArchiver(store Store, cfg Config, logger *log.Logger) *Archiver {
	if store == nil {
		store = NoopStore{}
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	a := &Archiver{
		store: store,
		cfg:   cfg,
		log:   logger,
	}
	if cfg.Enabled {
		a.queue = make(chan Archive, 512)
		a.wg.Add(1)
		go a.worker()
		logger.Printf("archive: enabled (max_body_kb=%d, retention_days=%d)", cfg.MaxBodyKB, cfg.RetentionDays)
	} else {
		logger.Printf("archive: disabled")
	}
	return a
}

// Enabled reports whether archival is active. Handlers can short-circuit
// expensive capture work when this returns false.
func (a *Archiver) Enabled() bool {
	return a.cfg.Enabled
}

// Submit enqueues an archive for asynchronous persistence. It is non-blocking:
// if the queue is full the record is dropped with a warning, and if archiving
// is disabled the call is a no-op. The caller therefore never waits on disk I/O.
func (a *Archiver) Submit(ar Archive) {
	if !a.cfg.Enabled {
		return
	}
	// Apply body size cap before queuing so the queue itself never holds
	// oversized payloads.
	ar.RequestBody = truncateBody(ar.RequestBody, a.cfg.MaxBodyKB)
	ar.ResponseBody = truncateBody(ar.ResponseBody, a.cfg.MaxBodyKB)
	if ar.SchemaVersion == 0 {
		ar.SchemaVersion = SchemaVersion
	}
	select {
	case a.queue <- ar:
	default:
		a.log.Printf("archive: queue full, dropping request_id=%s", ar.RequestID)
	}
}

// Close drains the queue and stops the worker. It blocks until all queued
// archives have been written (or the store returns an error) and is safe to
// call from main's defer chain.
func (a *Archiver) Close() {
	if !a.cfg.Enabled {
		return
	}
	close(a.queue)
	a.wg.Wait()
}

// CleanupRetention purges archives older than cfg.RetentionDays. Intended to be
// called periodically by the existing background cleanup task in main.
func (a *Archiver) CleanupRetention() {
	if !a.cfg.Enabled || a.cfg.RetentionDays <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -a.cfg.RetentionDays)
	n, err := a.store.DeleteArchivesBefore(cutoff)
	if err != nil {
		a.log.Printf("archive: cleanup error: %v", err)
		return
	}
	if n > 0 {
		a.log.Printf("archive: cleaned up %d archives older than %d days", n, a.cfg.RetentionDays)
	}
}

func (a *Archiver) worker() {
	defer a.wg.Done()
	for ar := range a.queue {
		if err := a.store.SaveArchive(ar); err != nil {
			a.log.Printf("archive: save error (request_id=%s): %v", ar.RequestID, err)
		}
	}
}

// truncateBody caps a JSON body string to maxKB kilobytes. When truncation is
// needed, the body is wrapped in a valid JSON envelope so downstream JSONL
// consumers can always parse the result without encountering broken JSON.
//
// The envelope preserves a preview of the original content and carries a
// "truncated" flag so consumers can distinguish truncated from complete bodies.
func truncateBody(body string, maxKB int) string {
	if maxKB <= 0 {
		return body
	}
	maxBytes := maxKB * 1024
	if len(body) <= maxBytes {
		return body
	}
	// Instead of cutting mid-JSON, wrap the preview in a valid JSON object so
	// downstream JSONL consumers always parse valid JSON. The preview must be
	// trimmed to account for JSON-escaping overhead (each byte can expand up
	// to 6x for control chars like \u0000), so we iteratively shrink until the
	// marshaled envelope fits within maxBytes.
	preview := body[:maxBytes]
	for {
		b, _ := json.Marshal(map[string]any{
			"truncated": true,
			"preview":   preview,
		})
		if len(b) <= maxBytes || len(preview) == 0 {
			return string(b)
		}
		// Trim by 10% and retry — converges quickly even for pathological input.
		preview = preview[:len(preview)*9/10]
	}
}

// MaxBodyKB returns the configured per-body size limit in KB. Exposed so
// handlers can size their archive sinks to match.
func (a *Archiver) MaxBodyKB() int {
	return a.cfg.MaxBodyKB
}
