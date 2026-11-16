package arllab

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/ports"
)

// labServer is a minimal arl/1 endpoint that enforces the envelope rules of
// the Lab's connection.md, so the real SDK and this adapter are exercised
// over a real WebSocket.
type labServer struct {
	mu       sync.Mutex
	hello    map[string]any
	events   []map[string]any
	auth     string
	problems []string
}

var eventName = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)

func (l *labServer) fail(format string) {
	l.mu.Lock()
	l.problems = append(l.problems, format)
	l.mu.Unlock()
}

func (l *labServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	l.auth = r.Header.Get("Authorization")
	l.mu.Unlock()
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := context.Background()
	var phases []string
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			return
		}
		var m map[string]any
		if json.Unmarshal(b, &m) != nil {
			l.fail("frame is not JSON")
			return
		}
		reply := map[string]any{}
		switch m["type"] {
		case "hello":
			if m["protocol"] != "arl/1" || m["stage"] != "dev" {
				l.fail("bad hello")
			}
			for _, p := range m["phases"].([]any) {
				phases = append(phases, p.(string))
			}
			l.mu.Lock()
			l.hello = m
			l.mu.Unlock()
			reply = map[string]any{"type": "hello_ack", "protocol": "arl/1", "session_id": "session_test", "resume_token": "secret", "phases": m["phases"]}
		case "ping":
			reply = map[string]any{"type": "pong"}
		case "event":
			name, _ := m["event"].(string)
			if !eventName.MatchString(name) || len(name) > 100 {
				l.fail("invalid event name " + name)
			}
			if id, _ := m["event_id"].(string); id == "" {
				l.fail("missing event_id on " + name)
			}
			if p, _ := m["phase"].(string); !slices.Contains(phases, p) {
				l.fail("undeclared phase " + p + " on " + name)
			}
			if _, ok := m["data"].(map[string]any); !ok {
				l.fail("data is not an object on " + name)
			}
			if (name == "span.start" || name == "span.end") && m["span_id"] == nil {
				l.fail(name + " without span_id")
			}
			for k := range m {
				if !slices.Contains([]string{"type", "event_id", "run_id", "event", "phase", "span_id", "parent_span_id", "timestamp", "data"}, k) {
					l.fail("unknown envelope field " + k)
				}
			}
			l.mu.Lock()
			l.events = append(l.events, m)
			n := len(l.events)
			l.mu.Unlock()
			reply = map[string]any{"type": "ack", "event_id": m["event_id"], "run_id": m["run_id"], "trace_id": "trace_1", "ordinal": n}
		}
		out, _ := json.Marshal(reply)
		if err := c.Write(ctx, websocket.MessageText, out); err != nil {
			return
		}
	}
}

func TestStreamsRunToLabOverWebSocket(t *testing.T) {
	lab := &labServer{}
	srv := httptest.NewServer(lab)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/agent-lab/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tr := New(ctx, Config{URL: url, Stage: "dev", Token: "lab-token", Service: "kora/test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if tr == nil {
		t.Fatal("tracer should be enabled with STAGE=dev and a URL")
	}
	id := uuid.New()
	model := ports.RunEvent{RunID: id, Seq: 3, Type: "model.called", Step: "agent_loop", Model: "gpt-5-mini", Payload: map[string]any{"role": "planner"}}
	for _, ev := range []ports.RunEvent{
		{RunID: id, Seq: 1, Type: "run.started", Step: "start", Payload: map[string]any{"config": map[string]any{"model": "gpt-5-mini"}}},
		{RunID: id, Seq: 2, Type: "input.received", Step: "guard_in", Payload: map[string]any{"text": "Osaka 5 malam"}},
		model,
		{RunID: id, Seq: 4, Type: "effort.decided", Step: "planner", Payload: map[string]any{"effort": "low"}},
		{RunID: id, Seq: 5, Type: "message.delta", Step: "narrate", Payload: map[string]string{"text": "Halo."}},
		{RunID: id, Seq: 6, Type: "message.completed", Step: "finish", Payload: map[string]any{}},
		{RunID: id, Seq: 7, Type: "run.finished", Step: "finish", Payload: map[string]any{"status": "completed"}},
		{RunID: id, Seq: 8, Type: "run.completed", Step: "finish", Payload: map[string]any{}},
	} {
		_ = tr.Emit(ctx, ev)
	}
	if err := tr.Close(ctx); err != nil {
		t.Logf("close: %v", err)
	}

	lab.mu.Lock()
	defer lab.mu.Unlock()
	for _, p := range lab.problems {
		t.Error("protocol violation: " + p)
	}
	if lab.auth != "Bearer lab-token" {
		t.Errorf("LAB_TOKEN must be sent as a bearer header, got %q", lab.auth)
	}
	if lab.hello["service"] != "kora/test" || len(lab.hello["phases"].([]any)) != len(Phases) {
		t.Errorf("hello: %+v", lab.hello)
	}
	var names []string
	for _, e := range lab.events {
		names = append(names, e["event"].(string))
	}
	if len(names) == 0 || names[0] != "run.start" || names[len(names)-1] != "run.end" {
		t.Fatalf("event order: %v", names)
	}
	for _, want := range []string{"state.snapshot", "input", "model.request", "model.response", "effort.decided", "output", "run.summary"} {
		if !slices.Contains(names, want) {
			t.Errorf("missing %s in %v", want, names)
		}
	}
}

func TestDisabledWithoutDevStage(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Setenv("LAB_WS_URL", "")
	t.Setenv("STAGE", "")
	if New(context.Background(), Config{URL: "ws://127.0.0.1:1/api/agent-lab/ws", Stage: "prod"}, log) != nil {
		t.Error("STAGE other than dev must disable the tracer")
	}
	if New(context.Background(), Config{Stage: "dev"}, log) != nil {
		t.Error("STAGE=dev without LAB_WS_URL must disable the tracer")
	}
}
