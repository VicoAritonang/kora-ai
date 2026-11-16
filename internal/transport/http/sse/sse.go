// Package sse writes Server-Sent Events (API_SPEC §6).
package sse

import (
	"fmt"
	"net/http"
	"strings"
)

// Writer streams events to one client.
type Writer struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

// New prepares the response for streaming.
func New(w http.ResponseWriter) *Writer {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	s := &Writer{w: w, rc: http.NewResponseController(w)}
	_ = s.rc.Flush()
	return s
}

// Event writes one event with id = run sequence.
func (s *Writer) Event(id int, event string, data []byte) error {
	var b strings.Builder
	fmt.Fprintf(&b, "id: %d\nevent: %s\n", id, event)
	for _, line := range strings.Split(string(data), "\n") {
		b.WriteString("data: " + line + "\n")
	}
	b.WriteString("\n")
	if _, err := s.w.Write([]byte(b.String())); err != nil {
		return err
	}
	return s.rc.Flush()
}

// Ping writes a comment to keep proxies from closing the stream.
func (s *Writer) Ping() error {
	if _, err := s.w.Write([]byte(": ping\n\n")); err != nil {
		return err
	}
	return s.rc.Flush()
}

// Retry tells the browser how long to wait before reconnecting.
func (s *Writer) Retry(ms int) error {
	_, err := fmt.Fprintf(s.w, "retry: %d\n\n", ms)
	return err
}
