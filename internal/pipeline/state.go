package pipeline

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/koraai/koraai/internal/agent"
	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/priceindex"
	"github.com/koraai/koraai/internal/store/postgres"
)

// RouteDecision is the router's output (MODULE_SPEC §5).
type RouteDecision struct {
	Intent         domain.Intent `json:"intent"`
	Confidence     float64       `json:"confidence"`
	HasBudget      bool          `json:"has_budget"`
	HasDestination bool          `json:"has_destination"`
	HasDates       bool          `json:"has_dates"`
	ChangesSlots   bool          `json:"changes_slots"`
	NeedsFreshInfo bool          `json:"needs_fresh_info"`
	Language       string        `json:"language"`
	UsedFallback   bool          `json:"used_fallback"`
	Overridden     string        `json:"overridden,omitempty"`
}

// runState is everything one run accumulates (RunState in MODULE_SPEC §4).
type runState struct {
	in      RunInput
	d       *Deps
	started time.Time
	seq     atomic.Int32

	run     postgres.Run
	conv    postgres.Conversation
	user    postgres.User
	userMsg postgres.Message

	text         string // user message after Guard.Inbound (PII masked)
	vault        ports.GuardVault
	guardSession []byte

	state  domain.TripState
	before domain.TripState
	route  RouteDecision
	req    domain.TripRequest

	pack        ports.KnowledgePack
	memory      string
	rc          *agent.RunContext
	packages    []domain.Package
	dests       []priceindex.DestinationEstimate
	plannerNote string

	clarification string
	unsupported   string

	mu    sync.Mutex
	flags []string
	usage ports.Usage
	steps int
	sent  []string // narration paragraphs sent to the client

	ws      workspace
	onEvent func(ctx context.Context, typ string, payload any)
}

func (s *runState) lang() string {
	if s.route.Language == "en" {
		return "en"
	}
	return "id"
}

func (s *runState) flag(f string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.flags {
		if x == f {
			return
		}
	}
	s.flags = append(s.flags, f)
}

func (s *runState) addUsage(u ports.Usage) {
	s.mu.Lock()
	s.usage = s.usage.Add(u)
	s.mu.Unlock()
}

func (s *runState) totals() postgres.RunTotals {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.usage
	if s.rc != nil {
		// rc.Usage already includes the loop; s.usage holds the rest.
		u = u.Add(s.rc.Usage)
	}
	return postgres.RunTotals{Steps: s.steps, InputTokens: u.InputTokens, CachedTokens: u.CachedTokens, OutputTokens: u.OutputTokens, CostMicros: u.CostMicros, Flags: append([]string{}, s.flags...)}
}

// emit records one event through the tracer; it never fails the run
// (SYSTEM_ARCHITECTURE §9: tracer failures do not fail runs).
func (s *runState) emit(ctx context.Context, typ string, public bool, step string, payload any, meta *agent.EventMeta) {
	ev := ports.RunEvent{RunID: s.in.RunID, Seq: int(s.seq.Add(1)), Type: typ, Public: public, Step: step, Payload: payload, At: s.d.Clock.Now()}
	if meta != nil {
		ev.Model = meta.Model
		if meta.Usage != nil {
			in, ca, out, c := meta.Usage.InputTokens, meta.Usage.CachedTokens, meta.Usage.OutputTokens, meta.Usage.CostMicros
			ev.InputTokens, ev.CachedTokens, ev.OutputTokens, ev.CostMicros = &in, &ca, &out, &c
		}
		if meta.Latency > 0 {
			l := meta.Latency.Milliseconds()
			ev.LatencyMs = &l
		}
	}
	if err := s.d.Tracer.Emit(context.WithoutCancel(ctx), ev); err != nil {
		s.d.Log.Error("emit event failed", "type", typ, "err", err)
	}
	// Project public progress onto the web workspace (state.patch / ui.action).
	if public && s.onEvent != nil && typ != "state.patch" && typ != "state.reset" && typ != "ui.action" {
		s.onEvent(ctx, typ, payload)
	}
}

// Emit implements agent.Emitter for the loop and tools.
func (s *runState) Emit(ctx context.Context, typ string, public bool, step string, payload any, meta *agent.EventMeta) {
	s.emit(ctx, typ, public, step, payload, meta)
}

// status emits a progress phase shown to the user.
func (s *runState) status(ctx context.Context, phase string) {
	s.emit(ctx, "status", true, phase, map[string]string{"phase": phase, "label": phaseLabel(phase, s.lang())}, nil)
}
