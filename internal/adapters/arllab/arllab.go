// Package arllab streams run events to Agent Reliability Lab (protocol arl/1)
// through the official Go SDK, go.vicoworks.com/agent-reliability-lab/sdk/go.
//
// It is a development tool: the SDK only connects when STAGE=dev and
// LAB_WS_URL is set, and app wiring never enables it outside APP_ENV=local.
// Delivery is fail-open and asynchronous. The SDK waits for the Lab's ACK on
// every event, so events are queued and sent by one background goroutine in
// order; a full queue drops events (Postgres stays the authoritative trace).
//
// Mapping (Kora RunEvent → arl/1):
//
//	run.started (private, first)  → run.start + state.snapshot (config)
//	input.received                → input (text after Guard.Inbound, PII masked)
//	model.called                  → model.request + model.response + cost.record
//	tool.called                   → tool.call + tool.result
//	retrieval.done                → retrieval.result
//	verify.done {passed|withheld} → evaluation (+ the raw verify.done)
//	message.delta …               → buffered, sent once as output
//	run.finished / completed / failed → run.end
//	pipeline step changes         → span.start / span.end (model and tool events nest under them)
//	state.patch, state.reset, tool.started/finished → skipped (UI projections of the events above)
//	everything else               → custom event with the same name and payload
package arllab

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	arl "go.vicoworks.com/agent-reliability-lab/sdk/go"

	"github.com/koraai/koraai/internal/ports"
)

// Phases are declared in the hello handshake; every event uses one of them.
var Phases = []string{"input", "guard", "understand", "retrieve", "plan", "build", "verify", "narrate", "output"}

// stepPhase maps a pipeline step to its Lab phase. Steps that open a span are
// the ones listed here with span=true.
var stepPhase = map[string]struct {
	phase string
	span  bool
}{
	"start":      {"input", true},
	"guard_in":   {"guard", true},
	"guard_out":  {"guard", false}, // per paragraph, nested in narrate
	"route":      {"understand", true},
	"extract":    {"understand", true},
	"clarify":    {"understand", true},
	"retrieve":   {"retrieve", true},
	"agent_loop": {"plan", true},
	"estimate":   {"build", true},
	"build":      {"build", true},
	"verify":     {"verify", true},
	"narrate":    {"narrate", true},
	"finish":     {"output", true},
}

// skipped events are UI projections already described by other events.
var skipped = map[string]bool{"state.patch": true, "state.reset": true, "tool.started": true, "tool.finished": true}

// maxPayload keeps one event well under the Lab's 1 MiB frame limit.
const maxPayload = 256 << 10

// Sink is the subset of the SDK client the tracer uses (a fake in tests).
type Sink interface {
	Emit(ctx context.Context, runID, event, phase string, data arl.Object, opts ...arl.EventOptions) (arl.Ack, error)
	Enabled() bool
	Close() error
}

// Config is the Lab connection; empty fields fall back to the SDK's
// environment variables (LAB_WS_URL, STAGE, LAB_TOKEN, LAB_PROJECT_ID).
type Config struct {
	URL, Stage, Token, Project string
	Required                   bool
	Service                    string
	Metadata                   map[string]any
}

// Tracer implements ports.Tracer.
type Tracer struct {
	mu     sync.Mutex // guards ch against send-after-close
	closed bool
	sink   Sink
	log    *slog.Logger
	ch     chan ports.RunEvent
	done   chan struct{}
	runs   map[string]*runTrace
	clock  func() time.Time
}

type runTrace struct {
	started bool
	span    string // current step span
	step    string
	phase   string
	spans   int
	output  strings.Builder
	outSent bool
	status  string
	lastAt  time.Time
}

// New connects the SDK client and starts the background sender. It returns
// nil when telemetry is not enabled (STAGE≠dev or no URL), so callers can
// skip it entirely.
func New(ctx context.Context, cfg Config, log *slog.Logger) *Tracer {
	meta := arl.Object{"app": "kora", "pipeline": "guard-in → route → extract → clarify → retrieve → agent loop → build → verify → narrate"}
	for k, v := range cfg.Metadata {
		meta[k] = v
	}
	c := arl.New(cfg.Service, arl.Options{
		URL: cfg.URL, Stage: cfg.Stage, Token: cfg.Token, Project: cfg.Project, Required: cfg.Required,
		Profile: "agent", Phases: Phases, Metadata: meta,
		Capabilities: []string{"tools", "costs", "retrieval", "guardrails", "evaluations", "spans"},
	})
	if !c.Enabled() {
		return nil
	}
	if err := c.Connect(ctx); err != nil {
		log.Warn("agent reliability lab: connect failed; continuing without lab traces", "err", err)
	}
	return NewWithSink(c, log)
}

// NewWithSink starts the tracer over any sink (tests use a fake).
func NewWithSink(s Sink, log *slog.Logger) *Tracer {
	t := &Tracer{sink: s, log: log, ch: make(chan ports.RunEvent, 4096), done: make(chan struct{}), runs: map[string]*runTrace{}, clock: time.Now}
	go t.loop()
	return t
}

// Emit enqueues the event and never blocks the pipeline.
func (t *Tracer) Emit(_ context.Context, ev ports.RunEvent) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	select {
	case t.ch <- ev:
	default:
		t.log.Warn("agent reliability lab queue full; event kept in postgres only", "type", ev.Type)
	}
	return nil
}

// Close drains queued events (bounded by ctx) and closes the connection.
func (t *Tracer) Close(ctx context.Context) error {
	t.mu.Lock()
	if !t.closed {
		t.closed = true
		close(t.ch)
	}
	t.mu.Unlock()
	select {
	case <-t.done:
	case <-ctx.Done():
	}
	return t.sink.Close()
}

func (t *Tracer) loop() {
	defer close(t.done)
	sweep := time.NewTicker(time.Minute)
	defer sweep.Stop()
	for {
		select {
		case ev, ok := <-t.ch:
			if !ok {
				return
			}
			t.handle(ev)
		case <-sweep.C:
			// Runs whose end event was dropped are forgotten after 30 minutes.
			for id, r := range t.runs {
				if t.clock().Sub(r.lastAt) > 30*time.Minute {
					delete(t.runs, id)
				}
			}
		}
	}
}

func (t *Tracer) send(runID, event, phase string, data arl.Object, opts ...arl.EventOptions) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := t.sink.Emit(ctx, runID, event, phase, data, opts...); err != nil {
		t.log.Warn("agent reliability lab: event not delivered", "event", event, "err", err)
	}
}

func (t *Tracer) handle(ev ports.RunEvent) {
	if skipped[ev.Type] {
		return
	}
	id := ev.RunID.String()
	r := t.runs[id]
	if r == nil {
		r = &runTrace{phase: "input"}
		t.runs[id] = r
	}
	r.lastAt = t.clock()
	data := object(ev.Payload)

	// run.start must be the first event of a run.
	if !r.started {
		r.started = true
		start := arl.Object{"name": "Kora run", "seq": ev.Seq}
		if mode, ok := data["mode"]; ok {
			start["mode"] = mode
		}
		t.send(id, "run.start", "input", start)
		if cfg, ok := data["config"]; ok && ev.Type == "run.started" {
			t.send(id, "state.snapshot", "input", arl.Object{"state": arl.Object{"message_id": data["message_id"], "message_chars": data["message_chars"]}, "config": cfg})
		}
		if ev.Type == "run.started" {
			t.enterStep(id, r, ev.Step)
			return
		}
	}
	t.enterStep(id, r, ev.Step)
	phase := r.phase
	span := arl.EventOptions{ParentSpanID: r.span}

	switch ev.Type {
	case "run.started":
		// The public run.started carries the routed mode.
		t.send(id, "run.mode", phase, data, span)
	case "input.received":
		t.send(id, "input", "input", arl.Object{"content": []arl.Object{arl.Text(str(data["text"]))}, "chars": data["chars"]}, span)
	case "model.called":
		t.modelCall(id, r, ev, data)
	case "tool.called":
		call := "call-" + str(data["call_id"])
		if call == "call-" {
			call = fmt.Sprintf("call-%d", ev.Seq)
		}
		opts := arl.EventOptions{SpanID: call, ParentSpanID: r.span}
		t.send(id, "tool.call", phase, arl.Object{"tool": data["name"], "call_id": call, "arguments": data["arguments"]}, opts)
		res := arl.Object{"call_id": call, "status": "ok", "result": data["digest"], "cache_hit": data["cache_hit"], "artifact_id": data["artifact_id"]}
		if s := str(data["status"]); s != "" && s != "ok" {
			res["status"], res["error"] = "error", firstNonEmpty(str(data["code"]), s)
		}
		if ev.LatencyMs != nil {
			res["duration_ms"] = *ev.LatencyMs
		}
		t.send(id, "tool.result", phase, res, opts)
	case "retrieval.done":
		data["strategy"] = "persona_pack"
		t.send(id, "retrieval.result", phase, data, span)
		t.cost(id, phase, ev, "retrieval")
	case "verify.done":
		t.send(id, "verify.done", phase, data, span)
		if p, ok := data["passed"].(bool); ok {
			t.send(id, "evaluation", phase, arl.Object{"name": "narration_rubric", "passed": p, "evidence": data}, span)
		}
		if w, _ := data["withheld_paragraph"].(bool); w {
			t.send(id, "evaluation", phase, arl.Object{"name": "narration_numbers", "passed": false, "evidence": data}, span)
		}
		t.cost(id, phase, ev, "rubric")
	case "message.delta":
		r.output.WriteString(str(data["text"]))
		r.output.WriteString("\n\n")
	case "message.completed":
		t.flushOutput(id, r)
	case "run.finished":
		r.status = str(data["status"])
		t.send(id, "run.summary", "output", data, span)
	case "run.completed":
		t.end(id, r, "completed", nil)
	case "run.failed":
		t.end(id, r, "failed", data)
	default:
		t.send(id, ev.Type, phase, data, span)
	}
}

// enterStep closes the current step span and opens the next one.
func (t *Tracer) enterStep(id string, r *runTrace, step string) {
	sp, ok := stepPhase[step]
	if !ok {
		return // status phases, workspace, effort roles: stay in the current span
	}
	r.phase = sp.phase
	if !sp.span || step == r.step {
		return
	}
	if r.span != "" {
		t.send(id, "span.end", stepPhase[r.step].phase, arl.Object{"name": r.step}, arl.EventOptions{SpanID: r.span})
	}
	r.spans++
	r.step, r.span = step, fmt.Sprintf("step-%02d-%s", r.spans, step)
	t.send(id, "span.start", sp.phase, arl.Object{"name": step}, arl.EventOptions{SpanID: r.span})
}

func (t *Tracer) modelCall(id string, r *runTrace, ev ports.RunEvent, data arl.Object) {
	spanID := fmt.Sprintf("model-%d", ev.Seq)
	opts := arl.EventOptions{SpanID: spanID, ParentSpanID: r.span}
	role := str(data["role"])
	req := arl.Object{"model": ev.Model, "role": role, "prompt_version": data["prompt_version"], "content": []arl.Object{arl.Text("(prompt not captured by Kora; role=" + role + ", prompt_version=" + str(data["prompt_version"]) + ")")}}
	if mode, ok := data["mode"]; ok {
		req["mode"] = mode
	}
	t.send(id, "model.request", r.phase, req, opts)
	res := arl.Object{"model": ev.Model, "role": role, "request_id": data["response_id"], "finish_reason": data["stop_reason"], "tool_calls": data["tool_calls"], "used_fallback": data["used_fallback"]}
	if ev.InputTokens != nil && ev.OutputTokens != nil {
		res["tokens_in"], res["tokens_out"] = *ev.InputTokens, *ev.OutputTokens
		if ev.CachedTokens != nil {
			res["tokens_cached"] = *ev.CachedTokens
		}
	}
	if ev.LatencyMs != nil {
		res["duration_ms"] = *ev.LatencyMs
	}
	if e := str(data["error"]); e != "" {
		res["status"], res["error"] = "error", e
	}
	t.send(id, "model.response", r.phase, res, opts)
	t.cost(id, r.phase, ev, role, opts)
}

// cost records spend reported by the pipeline (computed from token prices,
// so the Lab labels it "estimated").
func (t *Tracer) cost(id, phase string, ev ports.RunEvent, label string, opts ...arl.EventOptions) {
	if ev.CostMicros == nil || *ev.CostMicros == 0 {
		return
	}
	c := arl.Object{"billing_id": fmt.Sprintf("%s-%d", label, ev.Seq), "model": ev.Model, "amount_usd": float64(*ev.CostMicros) / 1e6, "source": "estimated", "label": label}
	if ev.InputTokens != nil && ev.OutputTokens != nil {
		c["tokens_in"], c["tokens_out"] = *ev.InputTokens, *ev.OutputTokens
	}
	t.send(id, "cost.record", phase, c, opts...)
}

func (t *Tracer) flushOutput(id string, r *runTrace) {
	if r.outSent {
		return
	}
	r.outSent = true
	text := strings.TrimSpace(r.output.String())
	if text == "" {
		return
	}
	t.send(id, "output", "output", arl.Object{"content": []arl.Object{arl.Text(text)}})
}

func (t *Tracer) end(id string, r *runTrace, status string, failure arl.Object) {
	t.flushOutput(id, r)
	if r.span != "" {
		t.send(id, "span.end", stepPhase[r.step].phase, arl.Object{"name": r.step}, arl.EventOptions{SpanID: r.span})
	}
	if failure != nil {
		t.send(id, "error", "output", arl.Object{"reason": firstNonEmpty(str(failure["code"]), "failed"), "message": failure["message"], "retryable": failure["retryable"]})
	}
	switch r.status {
	case "cancelled":
		status = "cancelled"
	case "blocked":
		status = "failed"
	}
	t.send(id, "run.end", "output", arl.Object{"status": status, "kora_status": r.status})
	delete(t.runs, id)
}

// object turns any payload into a JSON object, truncating oversized ones.
func object(v any) arl.Object {
	b, err := json.Marshal(v)
	if err != nil {
		return arl.Object{"unencodable": err.Error()}
	}
	if len(b) > maxPayload {
		return arl.Object{"truncated": true, "bytes": len(b)}
	}
	var o arl.Object
	if json.Unmarshal(b, &o) != nil || o == nil {
		return arl.Object{"value": json.RawMessage(b)}
	}
	return o
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
