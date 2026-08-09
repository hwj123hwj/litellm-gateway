package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/provider"
	"github.com/weijian/go-llm-gateway/internal/requestmeta"
)

// deferredStreamWriter delays the HTTP 200/SSE headers until the first byte
// is actually available. This leaves the response uncommitted when an
// upstream provider fails before producing output, so the handler can safely
// try a fallback provider or return the original HTTP status.
type deferredStreamWriter struct {
	dst       io.Writer
	commit    func()
	committed bool
}

func newDeferredStreamWriter(c *gin.Context) *deferredStreamWriter {
	return &deferredStreamWriter{
		dst: c.Writer,
		commit: func() {
			if c.Writer.Written() {
				return
			}
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("Connection", "keep-alive")
			c.Header("X-Accel-Buffering", "no")
			c.Writer.WriteHeader(http.StatusOK)
		},
	}
}

func (w *deferredStreamWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	w.Commit()
	return w.dst.Write(p)
}

func (w *deferredStreamWriter) Commit() {
	if w.committed {
		return
	}
	w.commit()
	w.committed = true
}

func (w *deferredStreamWriter) Started() bool {
	return w.committed
}

func (w *deferredStreamWriter) Flush() {
	if flusher, ok := w.dst.(http.Flusher); ok {
		flusher.Flush()
	}
}

// usageTrackingWriter observes SSE data while forwarding it unchanged. It is
// used on direct/pass-through streams where no protocol converter gets a
// chance to extract usage metadata for the archive.
type usageTrackingWriter struct {
	dst       io.Writer
	ctx       *gin.Context
	pending   string
	event     string
	dataLines []string
}

func newUsageTrackingWriter(dst io.Writer, ctx *gin.Context) *usageTrackingWriter {
	return &usageTrackingWriter{dst: dst, ctx: ctx}
}

func (w *usageTrackingWriter) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	if n > 0 {
		w.consume(p[:n])
	}
	return n, err
}

func (w *usageTrackingWriter) Flush() {
	if flusher, ok := w.dst.(interface{ Flush() }); ok {
		flusher.Flush()
	}
}

func (w *usageTrackingWriter) Finish() {
	if w.pending != "" {
		w.consume([]byte("\n"))
	}
	w.flushEvent()
}

func (w *usageTrackingWriter) consume(p []byte) {
	w.pending += string(p)
	for {
		idx := strings.IndexByte(w.pending, '\n')
		if idx < 0 {
			return
		}
		line := strings.TrimSuffix(w.pending[:idx], "\r")
		w.pending = w.pending[idx+1:]
		switch {
		case line == "":
			w.flushEvent()
		case strings.HasPrefix(line, "event:"):
			w.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			w.dataLines = append(w.dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

func (w *usageTrackingWriter) flushEvent() {
	if len(w.dataLines) == 0 {
		w.event = ""
		return
	}
	payload := strings.Join(w.dataLines, "\n")
	w.event = ""
	w.dataLines = nil
	if payload == "" || payload == "[DONE]" {
		return
	}
	var value any
	if err := json.Unmarshal([]byte(payload), &value); err != nil {
		return
	}
	inputTokens, outputTokens, ok := findUsage(value)
	if !ok || w.ctx == nil {
		return
	}
	currentInput := w.ctx.GetInt(requestmeta.InputTokensKey)
	currentOutput := w.ctx.GetInt(requestmeta.OutputTokensKey)
	if inputTokens > 0 {
		currentInput = inputTokens
	}
	if outputTokens > 0 {
		currentOutput = outputTokens
	}
	if inputTokens > 0 || outputTokens > 0 {
		setUsageMetadata(w.ctx, currentInput, currentOutput)
	}
}

// findUsage accepts Anthropic message usage, OpenAI chat usage, and native
// Responses usage. Walking nested objects also covers response.completed
// payloads without coupling the tracker to one protocol's response shape.
func findUsage(value any) (int, int, bool) {
	switch current := value.(type) {
	case map[string]any:
		if rawUsage, ok := current["usage"]; ok {
			if usage, ok := rawUsage.(map[string]any); ok {
				input, output, found := usageCounts(usage)
				if found {
					return input, output, true
				}
			}
		}
		for _, child := range current {
			if input, output, ok := findUsage(child); ok {
				return input, output, true
			}
		}
	case []any:
		for _, child := range current {
			if input, output, ok := findUsage(child); ok {
				return input, output, true
			}
		}
	}
	return 0, 0, false
}

func usageCounts(usage map[string]any) (int, int, bool) {
	input, inputFound := usageNumber(usage, "input_tokens", "prompt_tokens", "inputTokens", "promptTokens")
	output, outputFound := usageNumber(usage, "output_tokens", "completion_tokens", "outputTokens", "completionTokens")
	return input, output, inputFound || outputFound
}

func usageNumber(usage map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		value, ok := usage[key]
		if !ok {
			continue
		}
		switch number := value.(type) {
		case float64:
			return int(number), true
		case int:
			return number, true
		}
	}
	return 0, false
}

func writeSSEEvent(w io.Writer, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher, ok := w.(interface{ Flush() }); ok {
		flusher.Flush()
	}
	return err
}

func streamErrorPayload(err error) map[string]any {
	payload := map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "server_error",
			"message": err.Error(),
		},
	}
	var providerErr *provider.ProviderError
	if errors.As(err, &providerErr) {
		errorPayload := payload["error"].(map[string]any)
		if providerErr.StatusCode > 0 {
			errorPayload["code"] = providerErr.StatusCode
		}
		if providerErr.RequestID != "" {
			errorPayload["request_id"] = providerErr.RequestID
		}
	}
	return payload
}

func writeAnthropicStreamError(w io.Writer, err error) error {
	return writeSSEEvent(w, "error", streamErrorPayload(err))
}

func writeResponsesStreamError(w io.Writer, err error) error {
	return writeSSEEvent(w, "error", streamErrorPayload(err))
}

func writeOpenAIStreamError(w io.Writer, err error) error {
	payload := map[string]any{
		"error": map[string]any{
			"type":    "server_error",
			"message": err.Error(),
		},
	}
	if err := writeSSEEvent(w, "", payload); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
		return err
	}
	if flusher, ok := w.(interface{ Flush() }); ok {
		flusher.Flush()
	}
	return nil
}
