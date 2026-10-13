package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// StreamTiming measures actual model progress, excluding headers and keepalives.
type StreamTiming struct{ FirstOutputMs *float64 }

// RunStreamAttempt buffers the prelude until content, reasoning, a tool call,
// or a successful terminal event arrives. A stalled prelude can still fall back.
// The callback must honor context cancellation, as all built-in providers do.
func (r *Router) RunStreamAttempt(ctx context.Context, req *Request, p Provider, dst io.Writer, forward func(context.Context, io.Writer) error) (StreamTiming, error) {
	ctx = r.BeginRouting(ctx)
	firstLimit, overridden := r.firstOutputTimeout(req, p)
	if overridden {
		r.extendWaitingBudget(ctx, firstLimit)
	}
	limit := r.waitingTimeout(ctx, firstLimit)
	if limit <= 0 {
		return StreamTiming{}, routingTimeout(p.Name(), "fallback wait budget")
	}
	r.mu.RLock()
	idle := r.policy.StreamIdleTimeout
	r.mu.RUnlock()
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	w := &progressWriter{ctx: child, dst: dst, started: time.Now(), cancel: cancel, idle: idle}
	w.deadline = time.Now().Add(limit)
	w.phase = "first output"
	if limit < firstLimit {
		w.phase = "fallback wait budget"
	}
	w.timer = time.AfterFunc(limit, w.expire)
	err := forward(child, w)
	if err == nil && len(w.pending) > 0 {
		_, err = w.Write([]byte("\n\n"))
	}
	w.mu.Lock()
	w.finished = true
	w.timer.Stop()
	timing, timeoutErr := w.timing, w.err
	w.mu.Unlock()
	if ctx.Err() != nil {
		return timing, &RequestCanceledError{Cause: ctx.Err()}
	}
	if timeoutErr != nil {
		return timing, routingTimeout(p.Name(), timeoutErr.Error())
	}
	if err == nil && !w.terminal {
		return timing, incompleteStreamError(p.Name())
	}
	if err == context.DeadlineExceeded {
		return timing, routingTimeout(p.Name(), "response")
	}
	return timing, err
}

type progressWriter struct {
	ctx                context.Context
	dst                io.Writer
	started            time.Time
	cancel             context.CancelFunc
	idle               time.Duration
	deadline           time.Time
	phase              string
	mu                 sync.Mutex
	timer              *time.Timer
	finished, released bool
	terminal           bool
	err                error
	timing             StreamTiming
	pending, prelude   []byte
}

func (w *progressWriter) expire() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finished || w.err != nil || time.Now().Before(w.deadline) {
		return
	}
	w.err = fmt.Errorf("%s", w.phase)
	w.cancel()
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	data, err := w.prepareWrite(p)
	w.mu.Unlock()
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return len(p), nil
	}
	// A slow downstream must not hold the timer mutex and prevent upstream cancellation.
	n, err := w.dst.Write(data)
	if err != nil {
		w.cancel()
		return 0, err
	}
	if n != len(data) {
		w.cancel()
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}

// prepareWrite parses under the state lock; I/O happens after releasing it.
func (w *progressWriter) prepareWrite(p []byte) ([]byte, error) {
	if w.err != nil || w.ctx.Err() != nil {
		return nil, w.ctx.Err()
	}
	w.pending = append(w.pending, p...)
	if !w.released {
		w.prelude = append(w.prelude, p...)
	}
	if len(w.pending) > 2<<20 || len(w.prelude) > 2<<20 {
		return nil, fmt.Errorf("upstream SSE prelude or event exceeds 2 MiB")
	}
	progress, terminal := false, false
	for {
		end, width := bytes.Index(w.pending, []byte("\n\n")), 2
		if crlf := bytes.Index(w.pending, []byte("\r\n\r\n")); crlf >= 0 && (end < 0 || crlf < end) {
			end, width = crlf, 4
		}
		if end < 0 {
			break
		}
		if err := streamFrameError(w.pending[:end]); err != nil {
			return nil, err
		}
		more, done := sseProgress(w.pending[:end])
		w.terminal = w.terminal || done
		progress, terminal = progress || more, terminal || done
		w.pending = w.pending[end+width:]
	}
	if progress || terminal {
		w.timer.Stop()
		w.deadline = time.Now().Add(w.idle)
		w.phase = "stream idle"
		w.timer = time.AfterFunc(w.idle, w.expire)
		if progress && w.timing.FirstOutputMs == nil {
			ms := float64(time.Since(w.started).Microseconds()) / 1000
			w.timing.FirstOutputMs = &ms
		}
	}
	if !w.released {
		if !progress && !terminal {
			return nil, nil
		}
		w.released = true
		data := w.prelude
		w.prelude = nil
		return data, nil
	}
	return p, nil
}

func (w *progressWriter) Flush() {
	w.mu.Lock()
	released := w.released
	w.mu.Unlock()
	if released {
		if f, ok := w.dst.(interface{ Flush() }); ok {
			f.Flush()
		}
	}
}

// Inputs are either the gateway's Anthropic SSE or native Responses SSE.
func sseProgress(frame []byte) (bool, bool) {
	var data []string
	var eventName string
	for _, line := range strings.Split(string(frame), "\n") {
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	var event map[string]json.RawMessage
	if json.Unmarshal([]byte(strings.Join(data, "\n")), &event) != nil {
		return false, false
	}
	var kind string
	_ = json.Unmarshal(event["type"], &kind)
	if kind == "" {
		kind = eventName
	}
	text := func(raw json.RawMessage) bool { var s string; return json.Unmarshal(raw, &s) == nil && s != "" }
	switch kind {
	case "message_stop", "response.completed", "response.incomplete":
		return false, true
	case "content_block_delta":
		var delta map[string]json.RawMessage
		_ = json.Unmarshal(event["delta"], &delta)
		return text(delta["text"]) || text(delta["thinking"]) || text(delta["partial_json"]) || text(delta["signature"]), false
	case "content_block_start":
		var block map[string]json.RawMessage
		_ = json.Unmarshal(event["content_block"], &block)
		return text(block["text"]) || text(block["thinking"]) || text(block["name"]) || text(block["data"]), false
	case "response.output_text.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.delta", "response.function_call_arguments.delta", "response.refusal.delta":
		return text(event["delta"]), false
	case "response.output_item.added":
		var item map[string]json.RawMessage
		_ = json.Unmarshal(event["item"], &item)
		return text(item["name"]), false
	}
	return false, false
}

// ModelStreamProgress classifies a complete Anthropic or Responses SSE frame.
// Headers and keepalives are excluded from both runtime and benchmark timing.
func ModelStreamProgress(frame []byte) (progress, terminal bool) { return sseProgress(frame) }

// Failure events are not successful completions, even when HTTP was 200.
func streamFrameError(frame []byte) error {
	var lines []string
	var kind string
	for _, line := range strings.Split(string(frame), "\n") {
		if strings.HasPrefix(line, "event:") {
			kind = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if strings.HasPrefix(line, "data:") {
			lines = append(lines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	return streamPayloadError(kind, []byte(strings.Join(lines, "\n")))
}

// 转换器在丢弃未知 chunk 前也必须识别错误；[DONE] 不会覆盖先前的失败。
func streamPayloadError(kind string, payload []byte) error {
	var event struct {
		Type     string          `json:"type"`
		Error    json.RawMessage `json:"error"`
		Response struct {
			Error json.RawMessage `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal(payload, &event) != nil {
		return nil
	}
	if event.Type != "" {
		kind = event.Type
	}
	errorPayload := event.Error
	if len(errorPayload) == 0 || string(errorPayload) == "null" {
		errorPayload = event.Response.Error
	}
	if kind != "error" && kind != "response.failed" && (len(errorPayload) == 0 || string(errorPayload) == "null") {
		return nil
	}
	var details struct {
		Type   string          `json:"type"`
		Code   json.RawMessage `json:"code"`
		Status string          `json:"status"`
	}
	_ = json.Unmarshal(errorPayload, &details)
	status := 500
	var code string
	_ = json.Unmarshal(details.Code, &code)
	if details.Type == "rate_limit_error" || strings.Contains(code, "rate_limit") || string(details.Code) == "429" || details.Status == "RESOURCE_EXHAUSTED" {
		status = 429
	}
	return &ProviderError{StatusCode: status, Message: "upstream stream failed"}
}
