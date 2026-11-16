package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/adapters/fakes"
	"github.com/koraai/koraai/internal/agent"
	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/packagebuilder"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/session"
)

// emitter records events for assertions.
type emitter struct {
	mu    sync.Mutex
	types []string
}

func (e *emitter) Emit(_ context.Context, typ string, _ bool, _ string, _ any, _ *agent.EventMeta) {
	e.mu.Lock()
	e.types = append(e.types, typ)
	e.mu.Unlock()
}

func (e *emitter) count(t string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, x := range e.types {
		if x == t {
			n++
		}
	}
	return n
}

// echoTool registers a flight/stay/plan when called, to satisfy completion.
type echoTool struct {
	name string
	fn   func(rc *agent.RunContext)
}

func (t echoTool) Name() string { return t.name }
func (t echoTool) Spec() ports.ToolSpec {
	return ports.ToolSpec{Name: t.name, Parameters: json.RawMessage(`{"type":"object"}`)}
}
func (t echoTool) Label(json.RawMessage, *agent.RunContext) string { return t.name }
func (t echoTool) Execute(_ context.Context, _ json.RawMessage, rc *agent.RunContext) (agent.ToolResult, error) {
	if t.fn != nil {
		rc.Lock()
		t.fn(rc)
		rc.Unlock()
	}
	return agent.ToolResult{OK: true, Digest: `{"ok":true}`}, nil
}

func setup(steps []fakes.Step, maxSteps int) (agent.LoopInput, *fakes.Model, *emitter, *fakes.Guard) {
	rc := agent.NewRunContext(uuid.New(), uuid.New(), uuid.New(), domain.TripRequest{Nights: 2, Pax: domain.Pax{Adults: 1}}, time.Now)
	em := &emitter{}
	rc.Emitter = em
	m := &fakes.Model{Steps: steps}
	g := &fakes.Guard{Deny: map[string]bool{}}
	reg := agent.NewRegistry(
		echoTool{name: "search_flights", fn: func(rc *agent.RunContext) { rc.Flights[uuid.New()] = domain.FlightOffer{} }},
		echoTool{name: "search_stays", fn: func(rc *agent.RunContext) { rc.Stays[uuid.New()] = domain.StayOffer{} }},
		echoTool{name: "propose_day_plan", fn: func(rc *agent.RunContext) { rc.DayPlan = &packagebuilder.DayPlan{ID: uuid.New()} }},
	)
	return agent.LoopInput{
		RC: rc, Model: m, Guard: g, Registry: reg, Builder: session.Builder{B: session.DefaultBudgets},
		Limits: agent.Limits{MaxSteps: maxSteps, ToolTimeout: time.Second}, Context: session.ContextInput{Instructions: "sys", Task: "plan"},
	}, m, em, g
}

func TestLoop_StopsNormally(t *testing.T) {
	in, _, em, _ := setup([]fakes.Step{
		{ToolCalls: []ports.ToolCall{fakes.Call("1", "search_flights", map[string]any{}), fakes.Call("2", "search_stays", map[string]any{})}},
		{ToolCalls: []ports.ToolCall{fakes.Call("3", "propose_day_plan", map[string]any{})}},
		{Text: "done"},
	}, 12)
	res, err := agent.Loop{}.Run(context.Background(), in)
	if err != nil || res.StopReason != agent.StopDone || res.Steps != 3 {
		t.Fatalf("%v %+v", err, res)
	}
	if em.count("tool.called") != 3 || em.count("model.called") != 3 {
		t.Fatalf("events %v", em.types)
	}
}

func TestLoop_StepLimit(t *testing.T) {
	var steps []fakes.Step
	for i := 0; i < 10; i++ {
		steps = append(steps, fakes.Step{ToolCalls: []ports.ToolCall{fakes.Call("x", "search_flights", map[string]any{"i": i})}})
	}
	in, _, em, _ := setup(steps, 3)
	res, _ := agent.Loop{}.Run(context.Background(), in)
	if res.StopReason != agent.StopSteps || res.Steps != 3 || em.count("limit.reached") != 1 {
		t.Fatalf("%+v %v", res, em.types)
	}
}

func TestLoop_NudgesOnceWhenCandidatesMissing(t *testing.T) {
	in, m, _, _ := setup([]fakes.Step{{Text: "I am done"}, {Text: "still done"}}, 12)
	res, _ := agent.Loop{}.Run(context.Background(), in)
	if res.StopReason != agent.StopDone || res.Steps != 2 {
		t.Fatalf("%+v", res)
	}
	last := m.Requests[1].Input[len(m.Requests[1].Input)-1]
	if !strings.Contains(last.Content, "still need") {
		t.Fatalf("no nudge: %+v", last)
	}
}

func TestLoop_UnknownToolAndRefusal_AreStructuredErrors(t *testing.T) {
	in, m, _, g := setup([]fakes.Step{
		{ToolCalls: []ports.ToolCall{fakes.Call("1", "send_email", map[string]any{}), fakes.Call("2", "search_stays", map[string]any{})}},
		{Text: "ok"}, {Text: "ok"},
	}, 12)
	g.Deny["search_stays"] = true
	if _, err := (agent.Loop{}).Run(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	var outputs []string
	for _, it := range m.Requests[1].Input {
		if it.Type == "function_call_output" {
			outputs = append(outputs, it.Content)
		}
	}
	if len(outputs) != 2 || !strings.Contains(outputs[0], "unknown_tool") || !strings.Contains(outputs[1], "refused_by_guardrail") {
		t.Fatalf("outputs %v", outputs)
	}
}

func TestLoop_ModelFailure_ReturnsError(t *testing.T) {
	in, _, _, _ := setup([]fakes.Step{{Err: fakes.ErrUnavailable}}, 12)
	res, err := agent.Loop{}.Run(context.Background(), in)
	if err == nil || res.StopReason != agent.StopModelFail {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestLoop_HungModelCall_StopsAtDeadlineWithoutError(t *testing.T) {
	in, m, em, _ := setup(nil, 12)
	m.Hang = true
	in.Limits.Deadline = time.Now().Add(150 * time.Millisecond)
	start := time.Now()
	res, err := agent.Loop{}.Run(context.Background(), in)
	if err != nil || res.StopReason != agent.StopTime {
		t.Fatalf("%v %+v", err, res)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("deadline not enforced on the in-flight call")
	}
	if em.count("limit.reached") != 1 {
		t.Fatalf("events %v", em.types)
	}
}
