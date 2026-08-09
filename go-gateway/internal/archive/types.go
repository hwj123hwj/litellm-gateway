// Package archive implements the conversation archival data plane: it stores
// sanitized request/response payloads so they can be exported for knowledge
// base ingestion while keeping the lightweight request_logs table untouched.
//
// Archives are physically isolated from request_logs in a separate table and
// are disabled by default (ARCHIVE_ENABLED=false). When disabled, the archiver
// is a no-op and adds zero overhead to request handling.
package archive

import "time"

// SchemaVersion identifies the JSONL export payload shape. Bump this whenever
// the Archive struct gains/loses a field so downstream consumers can branch.
const SchemaVersion = 1

// Status classifies how an archived conversation terminated.
type Status string

const (
	StatusCompleted   Status = "completed"   // normal finish (non-stream or fully streamed)
	StatusInterrupted Status = "interrupted" // stream cut mid-flight (client disconnect / upstream error)
	StatusError       Status = "error"       // upstream returned a non-2xx before any body was streamed
)

// Protocol is the entry protocol that produced the archive.
type Protocol string

const (
	ProtocolMessages        Protocol = "messages"         // /v1/messages
	ProtocolChatCompletions Protocol = "chat_completions" // /v1/chat/completions
	ProtocolResponses       Protocol = "responses"        // /v1/responses (includes ChatGPT passthrough)
)

// Archive is a single sanitized conversation record stored in the DB and
// emitted by the JSONL exporter. Raw JSON bodies are stored as sanitized
// text (sensitive headers redacted, multimedia replaced by digests).
type Archive struct {
	ID            int64     `json:"id"`
	RequestID     string    `json:"request_id"`
	Timestamp     time.Time `json:"timestamp"`
	Protocol      Protocol  `json:"protocol"`
	Model         string    `json:"model"`
	Provider      string    `json:"provider"`
	IsStream      bool      `json:"is_stream"`
	Status        Status    `json:"status"`
	StatusCode    int       `json:"status_code"`
	InputTokens   int       `json:"input_tokens"`
	OutputTokens  int       `json:"output_tokens"`
	RequestBody   string    `json:"request_body"`           // sanitized JSON text
	ResponseBody  string    `json:"response_body"`          // sanitized JSON text (final aggregated state for streams)
	ErrorReason   string    `json:"error_reason,omitempty"` // populated when Status != completed
	SchemaVersion int       `json:"schema_version"`
}

// NewArchive builds an Archive with the schema version pre-filled.
func NewArchive() Archive {
	return Archive{SchemaVersion: SchemaVersion}
}

// Config controls archive behavior. All fields have safe defaults so a
// zero-value Config is a valid "disabled" configuration.
type Config struct {
	Enabled       bool // master switch, mirrors ARCHIVE_ENABLED
	MaxBodyKB     int  // per-body truncation limit, mirrors ARCHIVE_MAX_BODY_KB
	RetentionDays int  // rows older than this are purged by Cleanup, mirrors ARCHIVE_RETENTION_DAYS
}

// DefaultConfig returns production-safe defaults: disabled, 256 KB per body,
// 90-day retention. These are overridden by environment variables in config.Load.
func DefaultConfig() Config {
	return Config{
		Enabled:       false,
		MaxBodyKB:     256,
		RetentionDays: 90,
	}
}

// Store is the persistence contract for archives. The SQLite implementation
// lives in internal/storage; defining the interface here keeps the archive
// package free of a hard dependency on the storage package for testing.
type Store interface {
	SaveArchive(a Archive) error
	QueryArchives(limit int, offset int) ([]Archive, int, error)
	ExportArchives(sinceID int64, sinceTime time.Time, limit int) ([]Archive, error)
	DeleteArchivesBefore(cutoff time.Time) (int64, error)
	DeleteArchive(id int64) error
	Close() error
}

// NoopStore implements Store as a no-op. Used when archiving is disabled or
// the SQLite store failed to initialize, so handlers never need nil checks.
type NoopStore struct{}

func (NoopStore) SaveArchive(Archive) error                      { return nil }
func (NoopStore) QueryArchives(int, int) ([]Archive, int, error) { return nil, 0, nil }
func (NoopStore) ExportArchives(int64, time.Time, int) ([]Archive, error) {
	return nil, nil
}
func (NoopStore) DeleteArchivesBefore(time.Time) (int64, error) { return 0, nil }
func (NoopStore) DeleteArchive(int64) error                     { return nil }
func (NoopStore) Close() error                                  { return nil }
