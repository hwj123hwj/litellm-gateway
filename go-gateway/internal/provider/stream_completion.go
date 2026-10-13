package provider

import (
	"bytes"
	"errors"
	"io"
	"net/http"
)

var ErrIncompleteStream = errors.New("upstream stream ended without terminal event")

func incompleteStreamError(name string) error {
	return &ProviderError{Provider: name, StatusCode: http.StatusBadGateway, Message: ErrIncompleteStream.Error(), Cause: ErrIncompleteStream}
}

// EOF 只表示传输结束，不能作为模型成功完成的证据。直通路径同样检查协议终态。
func copyValidatedStream(name string, dst io.Writer, src io.Reader) error {
	w := &terminalTrackingWriter{dst: dst}
	if _, err := io.Copy(w, src); err != nil {
		return err
	}
	if !w.terminal {
		return incompleteStreamError(name)
	}
	return nil
}

type terminalTrackingWriter struct {
	dst      io.Writer
	pending  []byte
	terminal bool
}

func (w *terminalTrackingWriter) Write(p []byte) (int, error) {
	w.pending = append(w.pending, p...)
	for {
		end, width := bytes.Index(w.pending, []byte("\n\n")), 2
		if crlf := bytes.Index(w.pending, []byte("\r\n\r\n")); crlf >= 0 && (end < 0 || crlf < end) {
			end, width = crlf, 4
		}
		if end < 0 {
			break
		}
		if err := streamFrameError(w.pending[:end]); err != nil {
			return 0, err
		}
		_, terminal := sseProgress(w.pending[:end])
		w.terminal = w.terminal || terminal
		w.pending = w.pending[end+width:]
	}
	if len(w.pending) > 2<<20 {
		return 0, errors.New("upstream SSE event exceeds 2 MiB")
	}
	return w.dst.Write(p)
}

func (w *terminalTrackingWriter) Flush() {
	if f, ok := w.dst.(interface{ Flush() }); ok {
		f.Flush()
	}
}
