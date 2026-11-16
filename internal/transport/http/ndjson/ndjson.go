// Package ndjson streams run events as newline-delimited JSON
// (application/x-ndjson): one {"seq","type","data"} object per line. The web
// app reads it with fetch + ReadableStream and applies state.patch ops to its
// store; reconnects resume with ?after=<last seq>.
package ndjson

import (
	"encoding/json"
	"net/http"
)

// Writer streams NDJSON lines.
type Writer struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

// New prepares the response.
func New(w http.ResponseWriter) *Writer {
	h := w.Header()
	h.Set("Content-Type", "application/x-ndjson; charset=utf-8")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	n := &Writer{w: w, rc: http.NewResponseController(w)}
	_ = n.rc.Flush()
	return n
}

type line struct {
	Seq  int             `json:"seq,omitempty"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Event writes one event line.
func (n *Writer) Event(seq int, typ string, data []byte) error {
	b, err := json.Marshal(line{Seq: seq, Type: typ, Data: data})
	if err != nil {
		return err
	}
	if _, err := n.w.Write(append(b, '\n')); err != nil {
		return err
	}
	return n.rc.Flush()
}

// Ping writes a keep-alive line clients ignore.
func (n *Writer) Ping() error {
	if _, err := n.w.Write([]byte(`{"type":"ping"}` + "\n")); err != nil {
		return err
	}
	return n.rc.Flush()
}
