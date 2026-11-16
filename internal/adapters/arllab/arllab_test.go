package arllab

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	arl "go.vicoworks.com/agent-reliability-lab/sdk/go"

	"github.com/koraai/koraai/internal/ports"
)

type sent struct {
	run, event, phase string
	data              arl.Object
	opt               arl.EventOptions
}

type fakeSink struct {
	mu     sync.Mutex
	events []sent
	closed bool
}

func (f *fakeSink) Emit(_ context.Context, runID, event, phase string, data arl.Object, opts ...arl.EventOptions) (arl.Ack, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o := arl.EventOptions{}
	if len(opts) > 0 {
		o = opts[0]
	}
	f.events = append(f.events, sent{runID, event, phase, data, o})
	return arl.Ack{Type: "ack"}, nil
}
func (f *fakeSink) Enabled() bool { return true }
func (f *fakeSink) Close() error  { f.closed = true; return nil }

func (f *fakeSink) names() []string {
	var n []string
	for _, e := range f.events {
		n = append(n, e.event)
	}
	return n
}

func (f *fakeSink) find(event string) []sent {
	var out []sent
	for _, e := range f.events {
		if e.event == event {
			out = append(out, e)
		}
	}
	return out
}

func i64(v int64) *int64 { return &v }

func run(t *testing.T, events []ports.RunEvent) *fakeSink {
	t.Helper()
	sink := &fakeSink{}
	tr := NewWithSink(sink, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, ev := range events {
		_ = tr.Emit(context.Background(), ev)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := tr.Close(ctx); err != nil {
		t.Fatal(err)
	}
	return sink
}

func TestCompletedRunMapsToProtocol(t *testing.T) {
	id := uuid.New()
	ev := func(seq int, typ, step string, payload any) ports.RunEvent {
		return ports.RunEvent{RunID: id, Seq: seq, Type: typ, Step: step, Payload: payload}
	}
	model := ev(6, "model.called", "route", map[string]any{"role": "router_fallback", "prompt_version": "router@3"})
	model.Model, model.InputTokens, model.OutputTokens, model.CostMicros, model.LatencyMs = "gpt-5-nano", i64(120), i64(30), i64(42), i64(800)
	tool := ev(8, "tool.called", "agent_loop", map[string]any{"name": "search_flights", "call_id": "c1", "arguments": map[string]any{"to": "OSA"}, "digest": "3 offers", "status": "ok"})
	sink := run(t, []ports.RunEvent{
		ev(1, "run.started", "start", map[string]any{"config": map[string]any{"model": "gpt-5-mini"}, "message_id": "m1", "message_chars": 40}),
		ev(2, "status", "understanding", map[string]any{"phase": "understanding"}),
		ev(3, "guardrail.checked", "guard_in", map[string]any{"action": "allow"}),
		ev(4, "input.received", "guard_in", map[string]any{"text": "Ke Osaka 5 malam, budget [MASKED]", "chars": 33}),
		ev(5, "run.started", "start", map[string]any{"run_id": id, "mode": "plan_trip"}),
		model,
		ev(7, "state.patch", "workspace", map[string]any{"ops": []any{}}),
		tool,
		ev(9, "verify.done", "verify", map[string]any{"passed": true}),
		ev(10, "message.delta", "narrate", map[string]string{"text": "Paragraf satu."}),
		ev(11, "message.delta", "narrate", map[string]string{"text": "Paragraf dua."}),
		ev(12, "message.completed", "finish", map[string]any{"message_id": "m2"}),
		ev(13, "run.finished", "finish", map[string]any{"status": "completed"}),
		ev(14, "run.completed", "finish", map[string]any{"run_id": id}),
	})

	names := sink.names()
	if names[0] != "run.start" || names[len(names)-1] != "run.end" {
		t.Fatalf("run.start must be first and run.end last: %v", names)
	}
	for _, skip := range []string{"state.patch", "message.delta", "run.started", "run.completed"} {
		if slices.Contains(names, skip) {
			t.Errorf("%s must not be forwarded raw: %v", skip, names)
		}
	}
	for _, e := range sink.events {
		if !slices.Contains(Phases, e.phase) {
			t.Errorf("%s uses undeclared phase %q", e.event, e.phase)
		}
		if e.run != id.String() {
			t.Errorf("%s has run %q", e.event, e.run)
		}
	}
	if s := sink.find("state.snapshot"); len(s) != 1 || s[0].data["config"] == nil {
		t.Errorf("config snapshot missing: %+v", s)
	}
	if in := sink.find("input"); len(in) != 1 || !strings.Contains(textOf(in[0].data), "[MASKED]") {
		t.Errorf("input should carry the masked text: %+v", in)
	}
	req, res := sink.find("model.request"), sink.find("model.response")
	if len(req) != 1 || len(res) != 1 || req[0].opt.SpanID != res[0].opt.SpanID {
		t.Fatalf("model request/response must share a span: %+v %+v", req, res)
	}
	if !strings.HasPrefix(req[0].opt.ParentSpanID, "step-") || !strings.HasSuffix(req[0].opt.ParentSpanID, "route") {
		t.Errorf("model call should nest under the route step span, got %q", req[0].opt.ParentSpanID)
	}
	if res[0].data["tokens_in"] != int64(120) || res[0].data["duration_ms"] != int64(800) {
		t.Errorf("usage missing on model.response: %+v", res[0].data)
	}
	if c := sink.find("cost.record"); len(c) != 1 || c[0].data["amount_usd"] != 0.000042 || c[0].data["source"] != "estimated" {
		t.Errorf("cost record wrong: %+v", c)
	}
	if call, out := sink.find("tool.call"), sink.find("tool.result"); len(call) != 1 || len(out) != 1 || call[0].data["call_id"] != "call-c1" || out[0].data["status"] != "ok" {
		t.Errorf("tool exchange wrong: %+v %+v", call, out)
	}
	if e := sink.find("evaluation"); len(e) != 1 || e[0].data["passed"] != true {
		t.Errorf("rubric evaluation missing: %+v", e)
	}
	if o := sink.find("output"); len(o) != 1 || textOf(o[0].data) != "Paragraf satu.\n\nParagraf dua." {
		t.Errorf("output should join the narrated paragraphs: %+v", o)
	}
	if m := sink.find("run.mode"); len(m) != 1 || m[0].data["mode"] != "plan_trip" {
		t.Errorf("routed mode missing: %+v", m)
	}
	if end := sink.find("run.end"); end[0].data["status"] != "completed" {
		t.Errorf("run.end status: %+v", end[0].data)
	}
	// Every opened step span is closed.
	if starts, ends := len(sink.find("span.start")), len(sink.find("span.end")); starts == 0 || starts != ends {
		t.Errorf("span.start=%d span.end=%d", starts, ends)
	}
	if !sink.closed {
		t.Error("Close must close the sink")
	}
}

func TestBlockedRunEndsFailed(t *testing.T) {
	id := uuid.New()
	sink := run(t, []ports.RunEvent{
		{RunID: id, Seq: 1, Type: "guardrail.checked", Step: "guard_in", Payload: map[string]any{"action": "block"}},
		{RunID: id, Seq: 2, Type: "run.finished", Step: "finish", Payload: map[string]any{"status": "blocked", "code": "blocked_by_guardrail"}},
		{RunID: id, Seq: 3, Type: "run.failed", Step: "finish", Payload: map[string]any{"code": "blocked_by_guardrail", "message": "Diblokir", "retryable": false}},
	})
	names := sink.names()
	if names[0] != "run.start" {
		t.Fatalf("a run without run.started still opens with run.start: %v", names)
	}
	if e := sink.find("error"); len(e) != 1 || e[0].data["reason"] != "blocked_by_guardrail" {
		t.Errorf("error event: %+v", e)
	}
	end := sink.find("run.end")
	if len(end) != 1 || end[0].data["status"] != "failed" || end[0].data["kora_status"] != "blocked" {
		t.Errorf("run.end: %+v", end)
	}
}

func TestEmitAfterCloseAndOversizedPayload(t *testing.T) {
	sink := &fakeSink{}
	tr := NewWithSink(sink, slog.New(slog.NewTextHandler(io.Discard, nil)))
	big := strings.Repeat("x", maxPayload+1)
	_ = tr.Emit(context.Background(), ports.RunEvent{RunID: uuid.New(), Seq: 1, Type: "notice", Step: "build", Payload: map[string]string{"text": big}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tr.Close(ctx)
	_ = tr.Close(ctx)                                                                    // idempotent
	_ = tr.Emit(context.Background(), ports.RunEvent{RunID: uuid.New(), Type: "notice"}) // must not panic
	n := sink.find("notice")
	if len(n) != 1 || n[0].data["truncated"] != true {
		t.Errorf("oversized payload should be truncated: %+v", n)
	}
}

func textOf(d arl.Object) string {
	parts, _ := d["content"].([]arl.Object)
	if len(parts) == 0 {
		return ""
	}
	s, _ := parts[0]["text"].(string)
	return s
}
