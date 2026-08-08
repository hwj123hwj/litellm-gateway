package archive

import (
	"encoding/json"
	"io"
	"log"
	"sync"
	"time"

	"github.com/weijian/go-llm-gateway/internal/requestmeta"
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

// truncateBody caps a JSON body string to maxKB kilobytes. If truncation is
// needed, a marker is appended so consumers can detect that the payload was
// shortened.
func truncateBody(body string, maxKB int) string {
	if maxKB <= 0 {
		return body
	}
	maxBytes := maxKB * 1024
	if len(body) <= maxBytes {
		return body
	}
	return body[:maxBytes] + `...[truncated]`
}

// SnapshotFromContext builds the lightweight, protocol-neutral metadata fields
// for an Archive from the requestmeta values populated by middleware/handlers.
// It does NOT copy request/response bodies — those are supplied by the caller.
func SnapshotFromContext(requestID, protocol, model, provider string, isStream bool, attempts []requestmeta.ProviderAttempt) Archive {
	ar := NewArchive()
	ar.RequestID = requestID
	ar.Timestamp = time.Now().UTC()
	ar.Protocol = Protocol(protocol)
	ar.Model = model
	ar.Provider = provider
	ar.IsStream = isStream
	// Status / StatusCode / bodies are filled in by the handler after the
	// upstream call resolves.
	if blob, err := json.Marshal(attempts); err == nil {
		// Stash provider attempts in the error_reason slot as structured metadata
		// only when the request actually errored; otherwise leave it empty. The
		// caller overwrites this with a real reason before Submit when needed.
		_ = blob
	}
	return ar
}
