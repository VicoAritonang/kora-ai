// Package fakes provides in-memory implementations of ports for unit and
// pipeline tests. Never wired into a running environment.
package fakes

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/koraai/koraai/internal/ports"
)

// Step is one scripted model response.
type Step struct {
	Text      string
	ToolCalls []ports.ToolCall
	Err       error
}

// Call builds a tool call with JSON args.
func Call(id, name string, args any) ports.ToolCall {
	b, _ := json.Marshal(args)
	return ports.ToolCall{CallID: id, Name: name, Arguments: b}
}

// Model returns scripted responses in order and records requests.
type Model struct {
	// Hang makes every call block until its context ends (timeout tests).
	Hang     bool
	mu       sync.Mutex
	Steps    []Step
	Requests []ports.GenerateRequest
}

// Generate implements ports.ModelProvider.
func (m *Model) Generate(ctx context.Context, req ports.GenerateRequest) (ports.GenerateResult, error) {
	if m.Hang {
		<-ctx.Done()
		return ports.GenerateResult{Model: "fake"}, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Requests = append(m.Requests, req)
	if len(m.Steps) == 0 {
		return ports.GenerateResult{Model: "fake", StopReason: "completed", Text: "done"}, nil
	}
	s := m.Steps[0]
	m.Steps = m.Steps[1:]
	stop := "completed"
	if len(s.ToolCalls) > 0 {
		stop = "tool_calls"
	}
	return ports.GenerateResult{Model: "fake", Text: s.Text, ToolCalls: s.ToolCalls, StopReason: stop, Usage: ports.Usage{InputTokens: 100, OutputTokens: 10, CostMicros: 50}}, s.Err
}

// Stream implements ports.ModelProvider by delivering the text in one delta.
func (m *Model) Stream(ctx context.Context, req ports.GenerateRequest, onDelta func(string)) (ports.GenerateResult, error) {
	r, err := m.Generate(ctx, req)
	if err == nil && onDelta != nil {
		onDelta(r.Text)
	}
	return r, err
}

// Guard allows everything and records tool names.
type Guard struct {
	mu    sync.Mutex
	Tools []string
	Deny  map[string]bool
}

// Inbound implements ports.Guard.
func (g *Guard) Inbound(_ context.Context, in ports.GuardInbound) (ports.GuardInboundResult, error) {
	return ports.GuardInboundResult{Messages: in.Messages, Decision: ports.GuardDecision{Point: "input", Action: "allow"}}, nil
}

// RunTool implements ports.Guard.
func (g *Guard) RunTool(ctx context.Context, run ports.GuardToolRun, exec ports.ToolExec) (ports.GuardToolOutcome, error) {
	g.mu.Lock()
	g.Tools = append(g.Tools, run.Name)
	deny := g.Deny[run.Name]
	g.mu.Unlock()
	if deny {
		return ports.GuardToolOutcome{PreDecision: ports.GuardDecision{Point: "pre_tool", Action: "block"}}, ports.ErrToolRefused
	}
	r, err := exec(ctx)
	return ports.GuardToolOutcome{Executed: true, Content: r.Content, Session: run.Session, PreDecision: ports.GuardDecision{Point: "pre_tool", Action: "allow"}}, err
}

// Outbound implements ports.Guard.
func (g *Guard) Outbound(_ context.Context, in ports.GuardOutbound) (ports.GuardOutboundResult, error) {
	return ports.GuardOutboundResult{Completion: in.Completion}, nil
}

// PolicyVersion implements ports.Guard.
func (g *Guard) PolicyVersion() string { return "fake" }

// NewSession implements ports.Guard.
func (g *Guard) NewSession() []byte { return []byte(`{}`) }

// Tracer keeps events in memory.
type Tracer struct {
	mu     sync.Mutex
	Events []ports.RunEvent
}

// Emit implements ports.Tracer.
func (t *Tracer) Emit(_ context.Context, ev ports.RunEvent) error {
	t.mu.Lock()
	t.Events = append(t.Events, ev)
	t.mu.Unlock()
	return nil
}

// Types returns the event types in order.
func (t *Tracer) Types() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []string
	for _, e := range t.Events {
		out = append(out, e.Type)
	}
	return out
}

// Decider answers every question with fixed values.
type Decider struct {
	Answers map[string]ports.Answer
	Err     error
}

// Decide implements ports.Decider.
func (d *Decider) Decide(context.Context, ports.DecideRequest) (ports.DecideResult, error) {
	if d.Err != nil {
		return ports.DecideResult{}, d.Err
	}
	return ports.DecideResult{Model: "fake-jev", Answers: d.Answers}, nil
}

// ErrUnavailable is a convenience error for failure-path tests.
var ErrUnavailable = errors.Join(ports.ErrUnavailable, errors.New("fake outage"))

var (
	_ ports.ModelProvider = (*Model)(nil)
	_ ports.Guard         = (*Guard)(nil)
	_ ports.Tracer        = (*Tracer)(nil)
	_ ports.Decider       = (*Decider)(nil)
)
