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

// archiveSink is a thread-safe bytes.Buffer used to tee streamed SSE data so
// it can be archived after the stream completes. It implements io.Writer.
type archiveSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *archiveSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *archiveSink) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Return a copy so the caller can use it after the sink is reset/reused.
	out := make([]byte, s.buf.Len())
	copy(out, s.buf.Bytes())
	return out
}

// teeStreamWriter wraps an io.Writer (typically the deferredStreamWriter that
// writes to the HTTP client) and copies every byte into an archiveSink, so the
// handler can archive the full SSE transcript without delaying the live stream.
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

// teeStreamCopy copies from r to w while teeing into sink. Used for the raw
// SSE passthrough paths (e.g. Anthropic-compatible direct forwarding).
func teeStreamCopy(w io.Writer, r io.Reader, sink *archiveSink) (int64, error) {
	if sink == nil {
		return io.Copy(w, r)
	}
	return io.Copy(&teeStreamWriter{dst: w, sink: sink}, r)
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
	ar.ResponseBody = string(archive.Redact(responseBody))
	ar.ErrorReason = errorReason
	archiver.Submit(ar)
}

// captureResponseBody extracts the JSON body that gin has serialized into the
// response writer's buffer. Used for non-streaming responses where we archive
// the exact payload sent back to the client.
func captureResponseBody(w http.ResponseWriter) []byte {
	// gin's responseWriter exposes the written bytes via the Written() and
	// Body() helpers from the sse package, but those aren't public. Instead,
	// handlers that want archival pass the response object directly to
	// submitArchive. This helper remains for future use with custom writers.
	_ = w
	return nil
}

// parseStreamEndState inspects raw SSE bytes to determine whether the stream
// reached a terminal event ([DONE] for OpenAI chat, response.completed for
// Responses, message_stop for Anthropic). Returns (status, reason).
func parseStreamEndState(raw []byte, streamErr error) (archive.Status, string) {
	if streamErr != nil {
		return archive.StatusInterrupted, truncateReason(streamErr.Error())
	}
	text := string(raw)
	// OpenAI chat.completions: stream ends with "data: [DONE]"
	if strings.Contains(text, "[DONE]") {
		return archive.StatusCompleted, ""
	}
	// Responses API: ends with "response.completed"
	if strings.Contains(text, "response.completed") {
		return archive.StatusCompleted, ""
	}
	// Anthropic Messages: ends with "message_stop" event
	if strings.Contains(text, "message_stop") {
		return archive.StatusCompleted, ""
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
