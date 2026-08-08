package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/weijian/go-llm-gateway/internal/provider"
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
