// Package ports declares every interface to the outside world (rule D2: it
// imports only domain). Adapters implement these; application modules depend
// on them and never on an adapter.
package ports

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/domain"
)

// Typed errors understood by every caller (MODULE_SPEC §3).
var (
	ErrUnavailable        = errors.New("upstream unavailable")
	ErrTimeout            = errors.New("upstream timeout")
	ErrRateLimited        = errors.New("upstream rate limited")
	ErrNotFound           = errors.New("not found")
	ErrOutOfBookingWindow = errors.New("dates are outside the supplier sales window")
	ErrInvalidQuery       = errors.New("invalid query")
	ErrNotSupported       = errors.New("not supported by this adapter")
	ErrGuardBlocked       = errors.New("blocked by guardrail")
	ErrToolRefused        = errors.New("tool call refused by guardrail")
)

// ---------------------------------------------------------------- AI ------

// Role of a message given to a generative model.
type Role string

// Roles.
const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// InputItem is one element of a model's input: a message, a function call the
// model made earlier, or the output of that call.
type InputItem struct {
	Type    string // message | function_call | function_call_output
	Role    Role   // for messages
	Content string // message text or function output

	CallID    string // function_call / function_call_output
	Name      string // function_call
	Arguments string // function_call (raw JSON)
}

// Message builds a message input item.
func Message(role Role, content string) InputItem {
	return InputItem{Type: "message", Role: role, Content: content}
}

// ToolSpec describes a function tool to the model.
type ToolSpec struct {
	Name        string
	Description string
	Parameters  json.RawMessage // JSON Schema
}

// JSONSchema requests strict structured output.
type JSONSchema struct {
	Name   string
	Schema json.RawMessage
}

// GenerateRequest is one model call.
type GenerateRequest struct {
	Role            string // planner | extractor | narrator | ... (for metering)
	Model           string
	FallbackModel   string
	Instructions    string // system prompt (stable prefix first)
	Input           []InputItem
	Tools           []ToolSpec
	WebSearch       bool // enable the provider's web search tool
	ParallelTools   bool
	MaxOutputTokens int
	ReasoningEffort string // minimal | low | medium | high
	Schema          *JSONSchema
	CacheKey        string // provider prompt-cache routing key
}

// ToolCall is a function call the model proposed.
type ToolCall struct {
	CallID    string
	Name      string
	Arguments json.RawMessage
}

// Usage is token accounting for one or more calls.
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	CachedTokens int64 `json:"cached_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	CostMicros   int64 `json:"cost_usd_micros"`
}

// Add sums two usages.
func (u Usage) Add(o Usage) Usage {
	return Usage{u.InputTokens + o.InputTokens, u.CachedTokens + o.CachedTokens, u.OutputTokens + o.OutputTokens, u.CostMicros + o.CostMicros}
}

// Citation is a URL a web-search answer relied on.
type Citation struct {
	URL   string
	Title string
}

// GenerateResult is a completed model call.
type GenerateResult struct {
	Model      string
	ResponseID string
	Text       string
	ToolCalls  []ToolCall
	Citations  []Citation
	Usage      Usage
	StopReason string // completed | incomplete | tool_calls
	Latency    time.Duration
	UsedFallback bool
}

// ModelProvider is a generative model (OpenAI).
type ModelProvider interface {
	Generate(ctx context.Context, req GenerateRequest) (GenerateResult, error)
	// Stream calls onDelta with text fragments as they arrive and returns the
	// complete result.
	Stream(ctx context.Context, req GenerateRequest, onDelta func(string)) (GenerateResult, error)
}

// Question is one JEV question.
type Question struct {
	Type         string            // choice | noul | score
	Criteria     map[string]string // choice: option → description
	Levels       []string          // score: ordered levels, worst first
	Instructions string            // noul
}

// DecideRequest is one decision call with many questions over one state.
type DecideRequest struct {
	Role      string
	State     map[string]any
	Questions map[string]Question
}

// Answer is a JEV answer.
type Answer struct {
	Type          string
	Choice        string
	Probabilities map[string]float64
	Confidence    float64
	Yes           float64 // noul: probability of "yes"
	Score         float64 // score: 0..1
}

// DecideResult is the decision response.
type DecideResult struct {
	Model   string
	ID      string
	Answers map[string]Answer
	Usage   Usage
	Latency time.Duration
}

// Decider is a decision model (JEV).
type Decider interface {
	Decide(ctx context.Context, req DecideRequest) (DecideResult, error)
}

// ------------------------------------------------------------ Persona -----

// KnowledgeQuery is one text query to the persona pack.
type KnowledgeQuery struct {
	Purpose string
	Text    string
	Units   int
}

// KnowledgeUnit is a retrieved unit. Ref is "<pack_version>:<node_id>".
type KnowledgeUnit struct {
	Ref     string  `json:"ref"`
	Kind    string  `json:"kind"`
	Title   string  `json:"title"`
	Body    string  `json:"body"`
	Score   float64 `json:"score"`
	Purpose string  `json:"purpose"`
}

// KnowledgePack is the merged result of several queries.
type KnowledgePack struct {
	Units  []KnowledgeUnit `json:"units"`
	Gaps   []string        `json:"gaps"`
	Tokens int             `json:"tokens"`
}

// PersonaKnowledge is the read-only persona pack (OpenPersona).
type PersonaKnowledge interface {
	Core() string
	Version() string
	Retrieve(ctx context.Context, queries []KnowledgeQuery, budgetTokens int) (KnowledgePack, error)
}

// ------------------------------------------------------------- Memory -----

// MemoryMessage is a turn sent to the memory service (already redacted).
type MemoryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	At      time.Time `json:"at"`
}

// MemoryBlock is a prompt-ready, token-budgeted memory block.
type MemoryBlock struct {
	Text   string
	Tokens int
}

// MemoryFact is one remembered fact.
type MemoryFact struct {
	ID   string    `json:"id"`
	Text string    `json:"text"`
	At   time.Time `json:"valid_at"`
}

// UserMemory is cross-session memory (OpenPersona memory service).
type UserMemory interface {
	Remember(ctx context.Context, userID uuid.UUID, msgs []MemoryMessage) error
	Recall(ctx context.Context, userID uuid.UUID, query string, maxTokens int) (MemoryBlock, error)
	List(ctx context.Context, userID uuid.UUID) ([]MemoryFact, error)
	Forget(ctx context.Context, userID uuid.UUID) error
}

// --------------------------------------------------------- Guardrails -----

// GuardMessage is a chat turn as the guard sees it.
type GuardMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GuardDecision summarizes one stage decision for run_events.
type GuardDecision struct {
	Point         string         `json:"point"` // input | pre_tool | post_tool | output
	Action        string         `json:"action"` // allow | flag | redact | block
	Reason        string         `json:"reason,omitempty"`
	Findings      []GuardFinding `json:"findings,omitempty"`
	PolicyVersion string         `json:"policy_version,omitempty"`
	DurationMs    float64        `json:"duration_ms"`
	Degraded      bool           `json:"degraded,omitempty"`
}

// GuardFinding is one detection.
type GuardFinding struct {
	Guard    string  `json:"guard"`
	Category string  `json:"category"`
	Severity string  `json:"severity"`
	Score    float64 `json:"score"`
}

// GuardVault is an opaque handle to the redaction vault of one exchange. It
// lives only in memory for the duration of a run.
type GuardVault any

// GuardInbound is the Inbound request.
type GuardInbound struct {
	System   string
	Messages []GuardMessage
}

// GuardInboundResult carries the rewritten messages and the vault.
type GuardInboundResult struct {
	Messages []GuardMessage
	Vault    GuardVault
	Decision GuardDecision
}

// GuardToolRun is one tool call submitted to the guard.
type GuardToolRun struct {
	CallID  string
	Name    string
	Args    map[string]any
	Session []byte // opaque, stored in conversations.guard_session
	// Trace scopes per-trace breakers to one run; the session itself (taint,
	// effects) persists across the conversation.
	Trace string
}

// ToolExecResult is what our tool returned to the guard.
type ToolExecResult struct {
	Content   string
	Origin    string // tool | web
	Sensitive bool
}

// ToolExec runs the actual tool once the guard allows it.
type ToolExec func(ctx context.Context) (ToolExecResult, error)

// GuardToolOutcome is what the guard did with a tool call.
type GuardToolOutcome struct {
	Executed       bool
	Withheld       bool   // result blocked by tool-result rails
	Content        string // checked content, safe to show the model
	Session        []byte
	PreDecision    GuardDecision
	ResultDecision *GuardDecision
	Reason         string
}

// GuardOutbound is the Outbound request.
type GuardOutbound struct {
	System     string
	Completion string
	Context    []string
	Vault      GuardVault
}

// GuardOutboundResult carries the restored completion.
type GuardOutboundResult struct {
	Completion string
	Decision   GuardDecision
}

// Guard is the guardrails engine at the four hook points. Blocks are
// reported as errors wrapping ErrGuardBlocked / ErrToolRefused, with the
// result still populated.
type Guard interface {
	Inbound(ctx context.Context, in GuardInbound) (GuardInboundResult, error)
	RunTool(ctx context.Context, run GuardToolRun, exec ToolExec) (GuardToolOutcome, error)
	Outbound(ctx context.Context, out GuardOutbound) (GuardOutboundResult, error)
	PolicyVersion() string
	// NewSession returns an initial session granting the agent's capabilities.
	NewSession() []byte
}

// ------------------------------------------------------------- Tracer -----

// RunEvent is one recorded step of a run (AI_SYSTEM_DESIGN §11).
type RunEvent struct {
	RunID        uuid.UUID      `json:"run_id"`
	Seq          int            `json:"seq"`
	Type         string         `json:"type"`
	Public       bool           `json:"public"`
	Step         string         `json:"step,omitempty"`
	Payload      any            `json:"payload"`
	Model        string         `json:"model,omitempty"`
	InputTokens  *int64         `json:"input_tokens,omitempty"`
	CachedTokens *int64         `json:"cached_tokens,omitempty"`
	OutputTokens *int64         `json:"output_tokens,omitempty"`
	CostMicros   *int64         `json:"cost_usd_micros,omitempty"`
	LatencyMs    *int64         `json:"latency_ms,omitempty"`
	At           time.Time      `json:"created_at"`
}

// Tracer receives run events. Postgres is always a sink; AI Lab is another.
type Tracer interface {
	Emit(ctx context.Context, ev RunEvent) error
}

// ----------------------------------------------------------- Suppliers ----

// FlightQuery is a round-trip search.
type FlightQuery struct {
	Origin      string // city or airport code
	Destination string
	DepartDate  domain.Date
	ReturnDate  domain.Date
	Pax         domain.Pax
	Cabin       string // economy | premium_economy | business
	MaxStops    int    // -1 = any
}

// StayQuery is an accommodation search.
type StayQuery struct {
	City             string
	Area             string
	CheckIn          domain.Date
	CheckOut         domain.Date
	Guests           domain.Pax
	MaxPerNightIDR   int64
	MinRating        float64
	Limit            int
}

// ActivityQuery is an activity search.
type ActivityQuery struct {
	City      string
	Date      domain.Date
	Interests []string
}

// FlightSupplier searches and refreshes flights.
type FlightSupplier interface {
	Name() string
	Search(ctx context.Context, q FlightQuery) ([]domain.FlightOffer, error)
	Refresh(ctx context.Context, offer domain.FlightOffer) (domain.FlightOffer, error)
}

// StaySupplier searches and refreshes stays.
type StaySupplier interface {
	Name() string
	Search(ctx context.Context, q StayQuery) ([]domain.StayOffer, error)
	Refresh(ctx context.Context, offer domain.StayOffer) (domain.StayOffer, error)
}

// ActivitySupplier searches activities.
type ActivitySupplier interface {
	Name() string
	Search(ctx context.Context, q ActivityQuery) ([]domain.ActivityOffer, error)
}

// FXProvider returns exchange rates.
type FXProvider interface {
	Rate(ctx context.Context, base, quote string, on domain.Date) (domain.FXQuote, error)
}

// --------------------------------------------------------------- Email ----

// EmailSender delivers transactional email (magic links, reminders).
type EmailSender interface {
	Send(ctx context.Context, to, subject, text string) error
}
