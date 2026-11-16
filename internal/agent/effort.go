package agent

import "fmt"

// Effort is a provider-neutral reasoning effort. Adapters map it to what each
// model supports (OpenAI reasoning.effort, Anthropic output_config.effort,
// Gemini thinking_level).
type Effort string

// Effort levels, lowest first.
const (
	EffortMinimal Effort = "minimal"
	EffortLow     Effort = "low"
	EffortMedium  Effort = "medium"
	EffortHigh    Effort = "high"
)

var effortRank = map[Effort]int{EffortMinimal: 0, EffortLow: 1, EffortMedium: 2, EffortHigh: 3}

// ParseEffort reads a configured level; "auto" or unknown returns ok=false.
func ParseEffort(s string) (Effort, bool) {
	e := Effort(s)
	_, ok := effortRank[e]
	return e, ok
}

// Escalate returns the next level up, capped at max.
func Escalate(e, max Effort) Effort {
	next := e
	switch e {
	case EffortMinimal:
		next = EffortLow
	case EffortLow:
		next = EffortMedium
	case EffortMedium:
		next = EffortHigh
	}
	if effortRank[next] > effortRank[max] {
		return max
	}
	return next
}

// Signals describe the task, computed by code before a model call. Current
// research finds models poor at choosing their own effort up front, so the
// choice is made here and refined on verified failures (escalate once).
type Signals struct {
	SpecialRequests int  // festival, halal, children, accessibility, area…
	Interests       int
	Children        bool
	Refine          bool // changes an existing plan
	OutOfWindow     bool // estimates instead of live prices
	OverBudget      bool // narrator must explain alternatives
	Unsupported     bool
}

// EffortPolicy chooses effort per role. Mode "auto" derives it from Signals;
// a fixed level in Fixed[role] overrides auto for that role.
type EffortPolicy struct {
	Fixed map[string]Effort // role → level; absent = auto
	Max   Effort            // ceiling for auto and escalation (latency guard)
}

// Decision is one effort choice, recorded as an event for AI Lab.
type Decision struct {
	Role   string `json:"role"`
	Effort Effort `json:"effort"`
	Mode   string `json:"mode"` // auto | fixed | escalated
	Reason string `json:"reason"`
	Score  int    `json:"score,omitempty"`
}

// For returns the effort for a role.
//
//	extraction, routing, small talk, summaries → minimal (OpenAI: low/none for
//	  extraction and classification; minimal on the original GPT-5 family)
//	web research                               → low (web search needs ≥ low)
//	planner                                    → low, medium when the task
//	  carries several constraints
//	narrator                                   → low, medium when it must
//	  explain an over-budget result or special requests
func (p EffortPolicy) For(role string, s Signals) Decision {
	if e, ok := p.Fixed[role]; ok {
		return Decision{Role: role, Effort: e, Mode: "fixed", Reason: "configured"}
	}
	max := p.Max
	if max == "" {
		max = EffortMedium
	}
	cap := func(e Effort) Effort {
		if effortRank[e] > effortRank[max] {
			return max
		}
		return e
	}
	switch role {
	case "extractor", "router_fallback", "chitchat", "summarizer":
		return Decision{Role: role, Effort: EffortMinimal, Mode: "auto", Reason: "extraction/classification"}
	case "web_researcher":
		return Decision{Role: role, Effort: EffortLow, Mode: "auto", Reason: "web search requires at least low"}
	case "planner":
		score := 2*s.SpecialRequests + boolInt(s.Children) + boolInt(s.Refine) + boolInt(s.OutOfWindow)
		if s.Interests >= 2 {
			score++
		}
		e, why := EffortLow, "simple request"
		if score >= 2 {
			e, why = EffortMedium, "several constraints"
		}
		return Decision{Role: role, Effort: cap(e), Mode: "auto", Score: score, Reason: fmt.Sprintf("%s (score %d)", why, score)}
	case "narrator", "info":
		if s.OverBudget || s.SpecialRequests > 0 {
			return Decision{Role: role, Effort: cap(EffortMedium), Mode: "auto", Reason: "explains alternatives or special requests"}
		}
		return Decision{Role: role, Effort: EffortLow, Mode: "auto", Reason: "writes from given facts"}
	}
	return Decision{Role: role, Effort: cap(EffortLow), Mode: "auto", Reason: "default"}
}

// EscalateAfter returns the decision for a retry after a verified failure.
func (p EffortPolicy) EscalateAfter(d Decision, why string) Decision {
	max := p.Max
	if max == "" {
		max = EffortMedium
	}
	if d.Mode == "fixed" {
		return d // a configured level is respected
	}
	return Decision{Role: d.Role, Effort: Escalate(d.Effort, max), Mode: "escalated", Reason: why, Score: d.Score}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
