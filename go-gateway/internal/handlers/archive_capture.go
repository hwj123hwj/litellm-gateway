package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/archive"
	"github.com/weijian/go-llm-gateway/internal/requestmeta"
)

// defaultSinkMaxBytes caps how much of a streamed SSE transcript the archive
// sink retains in memory. This is independent of ARCHIVE_MAX_BODY_KB (which
// applies during the final Submit) because the sink must stay bounded *during*
// streaming, not just after. When the cap is reached the sink stops accepting
// data and records a truncation marker so terminal-event detection can still
// inspect the tail.
const defaultSinkMaxBytes = 256 * 1024 // 256 KB per stream

// archiveSink is a thread-safe, bounded buffer used to tee streamed SSE data
// so it can be archived after the stream completes. It implements io.Writer.
//
// The buffer has a hard capacity (maxBytes). Once the cap is reached, further
// writes are silently dropped and a truncation marker is appended. This
// prevents unbounded memory growth from long or non-terminating streams.
type archiveSink struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	maxBytes int
	capped   bool // true once the cap was hit
}

func newArchiveSink() *archiveSink {
	return &archiveSink{maxBytes: defaultSinkMaxBytes}
}

func (s *archiveSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.capped {
		return len(p), nil // silently drop beyond the cap
	}
	remaining := s.maxBytes - s.buf.Len()
	if remaining <= 0 {
		s.capped = true
		_, _ = s.buf.WriteString("\n...[stream archive truncated at capacity]\n")
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = s.buf.Write(p[:remaining])
		s.capped = true
		_, _ = s.buf.WriteString("\n...[stream archive truncated at capacity]\n")
		return len(p), nil
	}
	return s.buf.Write(p)
}

func (s *archiveSink) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]byte, s.buf.Len())
	copy(out, s.buf.Bytes())
	return out
}

// Len returns the number of bytes currently in the buffer (thread-safe).
func (s *archiveSink) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Len()
}

// teeStreamWriter wraps an io.Writer (typically the deferredStreamWriter that
// writes to the HTTP client) and copies every byte into an archiveSink, so the
// handler can archive the full SSE transcript without delaying the live stream.
//
// It also forwards Flush calls to the underlying writer so that streaming
// semantics (incremental SSE delivery) are preserved when the archive tee is
// active. Without this, http.Flusher type assertions in provider streaming
// code would fail and net/http would buffer events.
type teeStreamWriter struct {
	dst  io.Writer
	sink *archiveSink
}

func newTeeStreamWriter(dst io.Writer, sink *archiveSink) *teeStreamWriter {
	return &teeStreamWriter{dst: dst, sink: sink}
}

func (w *teeStreamWriter) Write(p []byte) (int, error) {
	if w.sink != nil {
		_, _ = w.sink.Write(p)
	}
	return w.dst.Write(p)
}

// Flush forwards the flush to the destination if it implements http.Flusher.
// This is critical: provider streaming code checks for http.Flusher to deliver
// SSE events incrementally. Without this method, the tee wrapper would break
// that contract and cause events to be buffered until the stream ends.
func (w *teeStreamWriter) Flush() {
	if flusher, ok := w.dst.(http.Flusher); ok {
		flusher.Flush()
	}
}

// submitArchive is the shared post-request archival helper used by all three
// protocol handlers. It reads the request/response metadata that middleware
// and handlers already set on the gin.Context, fills in the bodies, and pushes
// the record to the archiver. It is safe to call even when archiving is
// disabled (the archiver will no-op).
func submitArchive(
	c *gin.Context,
	archiver *archive.Archiver,
	protocol archive.Protocol,
	requestBody []byte,
	responseBody []byte,
	status archive.Status,
	statusCode int,
	errorReason string,
) {
	if archiver == nil || !archiver.Enabled() {
		return
	}
	ar := archive.NewArchive()
	ar.RequestID = c.GetString(requestmeta.RequestIDKey)
	ar.Timestamp = nowFunc()
	ar.Protocol = protocol
	ar.Model = c.GetString(requestmeta.ModelKey)
	ar.Provider = c.GetString(requestmeta.ProviderKey)
	ar.IsStream = c.GetBool(requestmeta.RequestIsStreamKey)
	ar.Status = status
	ar.StatusCode = statusCode
	ar.InputTokens = c.GetInt(requestmeta.InputTokensKey)
	ar.OutputTokens = c.GetInt(requestmeta.OutputTokensKey)
	ar.RequestBody = string(archive.Redact(requestBody))

	// For streaming responses, responseBody is an SSE transcript — a sequence
	// of "data: {json}\n\n" lines, not a single JSON document. archive.Redact
	// would fail to parse it and return the raw bytes, leaving sensitive data
	// or inline multimedia un-sanitized. We redact each SSE data payload
	// individually instead.
	if ar.IsStream {
		ar.ResponseBody = string(redactSSETranscript(responseBody))
	} else {
		ar.ResponseBody = string(archive.Redact(responseBody))
	}

	// Sanitize error reasons: upstream error messages may contain credentials
	// (e.g. "invalid api key: sk-secret"). Apply the same redaction logic.
	ar.ErrorReason = sanitizeErrorReason(errorReason)
	archiver.Submit(ar)
}

// redactSSETranscript parses an SSE byte stream and redacts each "data:"
// payload individually. Lines that are not valid JSON (e.g. "data: [DONE]")
// are left as-is. Non-data lines (event:, comments) are also preserved.
func redactSSETranscript(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "data: ") && !strings.HasPrefix(trimmed, "data:") {
			continue
		}
		payload := strings.TrimPrefix(trimmed, "data: ")
		payload = strings.TrimPrefix(payload, "data:")
		payload = strings.TrimSpace(payload)
		// Skip non-JSON payloads like [DONE]
		if payload == "[DONE]" || payload == "" {
			continue
		}
		// Redact the individual JSON payload
		redacted := archive.Redact([]byte(payload))
		lines[i] = "data: " + string(redacted)
	}
	return []byte(strings.Join(lines, "\n"))
}

// sanitizeErrorReason redacts any credential-like content from an error
// message before it is persisted in error_reason. Upstream errors can contain
// echoed secrets (e.g. "invalid key: sk-xxx"), so we must treat the entire
// error string as untrusted.
func sanitizeErrorReason(reason string) string {
	if reason == "" {
		return ""
	}
	// Try to parse as JSON and redact; if not JSON, apply pattern-based
	// scrubbing for common credential formats.
	redacted := archive.Redact([]byte(reason))
	// Even if Redact returns the raw string (non-JSON), apply a regex-free
	// scrub for common credential prefixes that upstream APIs might echo.
	result := string(redacted)
	for _, pattern := range []string{"sk-", "Bearer ", "x-api-key:"} {
		if idx := strings.Index(result, pattern); idx >= 0 {
			// Replace from the pattern to the next space or end of string
			end := idx + len(pattern)
			for end < len(result) && result[end] != ' ' && result[end] != '\n' && result[end] != '"' && result[end] != '\'' {
				end++
			}
			result = result[:idx] + "[REDACTED]" + result[end:]
		}
	}
	return truncateReason(result)
}

// parseStreamEndState inspects raw SSE bytes to determine whether the stream
// reached a terminal event. It parses SSE event/data lines to match terminal
// event types precisely, rather than using fragile substring searches.
//
// Recognized terminal events:
//   - OpenAI chat.completions: data: [DONE]
//   - Responses API: event: response.completed / response.failed / response.incomplete
//   - Anthropic Messages: event: message_stop
func parseStreamEndState(raw []byte, streamErr error) (archive.Status, string) {
	if streamErr != nil {
		return archive.StatusInterrupted, sanitizeErrorReason(streamErr.Error())
	}
	text := string(raw)

	// Parse SSE structure: look at "event:" and "data:" lines
	hasDone := false
	hasCompleted := false
	hasMessageStop := false
	hasFailed := false
	hasIncomplete := false

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "data: [DONE]":
			hasDone = true
		case strings.HasPrefix(trimmed, "event: response.completed"):
			hasCompleted = true
		case strings.HasPrefix(trimmed, "event: response.failed"):
			hasFailed = true
		case strings.HasPrefix(trimmed, "event: response.incomplete"):
			hasIncomplete = true
		case strings.HasPrefix(trimmed, "event: message_stop"):
			hasMessageStop = true
		case strings.Contains(trimmed, "\"type\":\"response.completed\""):
			// Some Responses SSE payloads embed the type in data JSON
			hasCompleted = true
		case strings.Contains(trimmed, "\"type\":\"message_stop\""):
			hasMessageStop = true
		}
	}

	if hasDone || hasCompleted || hasMessageStop {
		return archive.StatusCompleted, ""
	}
	if hasFailed {
		return archive.StatusError, "upstream returned response.failed"
	}
	if hasIncomplete {
		return archive.StatusInterrupted, "upstream returned response.incomplete"
	}

	// No terminal marker found → the stream was interrupted before completion.
	return archive.StatusInterrupted, "stream ended without terminal event"
}

func truncateReason(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 512 {
		return s[:512] + "..."
	}
	return s
}

// marshalCompact serializes v to compact JSON, ignoring errors (returns empty
// string on failure). Used when archiving non-stream responses where we want
// the exact JSON the client received.
func marshalCompact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
