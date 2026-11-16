// Package agent runs the planner loop: model + tools within hard limits
// (AI_SYSTEM_DESIGN §5). It is a thin, hand-written loop so guardrails and
// the event log hook into every step.
package agent

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/packagebuilder"
	"github.com/koraai/koraai/internal/ports"
)

// Tool is one capability the planner may call.
type Tool interface {
	Name() string
	Spec() ports.ToolSpec
	// Label is the user-facing progress text (no raw arguments).
	Label(args json.RawMessage, rc *RunContext) string
	Execute(ctx context.Context, args json.RawMessage, rc *RunContext) (ToolResult, error)
}

// ToolResult is what the model sees (Digest) plus bookkeeping.
type ToolResult struct {
	OK         bool      `json:"ok"`
	Code       string    `json:"code,omitempty"`
	Digest     string    `json:"digest"`
	ArtifactID uuid.UUID `json:"artifact_id,omitzero"`
	Retryable  bool      `json:"retryable,omitempty"`
	CacheHit   bool      `json:"cache_hit,omitempty"`
	Origin     string    `json:"origin,omitempty"` // tool | web
	Summary    string    `json:"summary,omitempty"` // public, user-facing
}

// Fail builds a structured error result the model can act on (§6).
func Fail(code, hint string, retryable bool) ToolResult {
	b, _ := json.Marshal(map[string]any{"ok": false, "code": code, "retryable": retryable, "hint": hint})
	return ToolResult{OK: false, Code: code, Digest: string(b), Retryable: retryable}
}

// EventMeta carries model accounting for an event.
type EventMeta struct {
	Model   string
	Usage   *ports.Usage
	Latency time.Duration
}

// Emitter records run events (pipeline implements it).
type Emitter interface {
	Emit(ctx context.Context, typ string, public bool, step string, payload any, meta *EventMeta)
}

// Counters track quotas within one run.
type Counters struct {
	Flights, Stays, Activities, Web int
}

// RunContext is the run state tools share. Tools register candidates here;
// the package builder only ever sees offers that came through a tool.
type RunContext struct {
	mu sync.Mutex

	RunID          uuid.UUID
	ConversationID uuid.UUID
	UserID         uuid.UUID
	Request        domain.TripRequest
	Now            func() time.Time

	Flights    map[uuid.UUID]domain.FlightOffer
	Stays      map[uuid.UUID]domain.StayOffer
	Activities map[uuid.UUID]domain.ActivityOffer

	FlightEstimate *domain.Estimate
	StayEstimates  map[domain.Tier]domain.Estimate

	Norms     packagebuilder.CostNorms
	NormList  []domain.Norm
	FX        domain.FXTable
	Knowledge []ports.KnowledgeUnit
	WebFacts  []WebFact

	DayPlan *packagebuilder.DayPlan
	// Selection is the planner's last build_packages choice.
	Selection *Selection
	// Presentation is the planner's instruction for the web canvas
	// (present_to_user): which view to show and which tier to recommend.
	Presentation *Presentation
	Preview   *packagebuilder.Output

	Count   Counters
	Notices []Notice

	GuardSession []byte
	Usage        ports.Usage
	Emitter      Emitter
}

// Selection is the set of ids the planner chose.
type Selection struct {
	Flights []uuid.UUID
	Stays   []uuid.UUID
}

// Presentation directs the web canvas (the agent as actuator).
type Presentation struct {
	View   string `json:"view"`
	Tier   string `json:"tier,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// WebFact is one researched fact (untrusted).
type WebFact struct {
	Claim       string `json:"claim"`
	URL         string `json:"url"`
	PublishedAt string `json:"published_at,omitempty"`
}

// Notice is shown to the user (e.g. supplier down, item estimated).
type Notice struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// NewRunContext initializes maps.
func NewRunContext(runID, convID, userID uuid.UUID, req domain.TripRequest, now func() time.Time) *RunContext {
	return &RunContext{
		RunID: runID, ConversationID: convID, UserID: userID, Request: req, Now: now,
		Flights: map[uuid.UUID]domain.FlightOffer{}, Stays: map[uuid.UUID]domain.StayOffer{},
		Activities: map[uuid.UUID]domain.ActivityOffer{}, StayEstimates: map[domain.Tier]domain.Estimate{},
		FX: domain.FXTable{},
	}
}

// Lock guards RunContext fields when tools run in parallel.
func (rc *RunContext) Lock() { rc.mu.Lock() }

// Unlock releases the lock.
func (rc *RunContext) Unlock() { rc.mu.Unlock() }

// AddNotice records a user-facing notice once per code.
func (rc *RunContext) AddNotice(code, text string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	for _, n := range rc.Notices {
		if n.Code == code {
			return
		}
	}
	rc.Notices = append(rc.Notices, Notice{code, text})
}

// HasFlights reports whether a flight candidate or estimate exists.
func (rc *RunContext) HasFlights() bool {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return len(rc.Flights) > 0 || rc.FlightEstimate != nil
}

// HasStays reports whether a stay candidate or estimate exists.
func (rc *RunContext) HasStays() bool {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return len(rc.Stays) > 0 || len(rc.StayEstimates) > 0
}

// Registry holds tools by name.
type Registry struct {
	tools map[string]Tool
	order []string
}

// NewRegistry registers tools.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: map[string]Tool{}}
	for _, t := range tools {
		r.tools[t.Name()] = t
		r.order = append(r.order, t.Name())
	}
	return r
}

// Get returns a tool.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Specs returns the tool definitions in registration order (stable prefix).
func (r *Registry) Specs() []ports.ToolSpec {
	out := make([]ports.ToolSpec, 0, len(r.order))
	for _, n := range r.order {
		out = append(out, r.tools[n].Spec())
	}
	return out
}
