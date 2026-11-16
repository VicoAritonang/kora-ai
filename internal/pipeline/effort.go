package pipeline

import (
	"context"

	"github.com/koraai/koraai/internal/agent"
	"github.com/koraai/koraai/internal/config"
	"github.com/koraai/koraai/internal/domain"
)

// effortPolicy builds the auto/fixed policy from configuration. A role set
// to a level is fixed; "auto" (default) is decided per task.
func effortPolicy(cfg *config.Config) *agent.EffortPolicy {
	p := &agent.EffortPolicy{Fixed: map[string]agent.Effort{}, Max: agent.EffortMedium}
	if e, ok := agent.ParseEffort(cfg.Models.EffortMax); ok {
		p.Max = e
	}
	set := func(v string, roles ...string) {
		if e, ok := agent.ParseEffort(v); ok {
			for _, r := range roles {
				p.Fixed[r] = e
			}
		}
	}
	set(cfg.Models.PlannerEffort, "planner")
	set(cfg.Models.ExtractorEffort, "extractor", "router_fallback", "chitchat", "summarizer")
	set(cfg.Models.NarratorEffort, "narrator", "info")
	return p
}

// signals derives the task signals from the request and results so far.
func signals(st *runState) agent.Signals {
	s := agent.Signals{
		SpecialRequests: len(st.req.Special), Interests: len(st.req.Interests), Children: st.req.Pax.Children > 0,
		Refine: st.route.Intent == domain.IntentRefine, OutOfWindow: st.req.OutOfBookingWindow(),
	}
	for _, p := range st.packages {
		if p.Feasibility == domain.FeasibilityOverBudget {
			s.OverBudget = true
		}
	}
	return s
}

// effort decides and records the effort for a role.
func (r *Runner) effort(ctx context.Context, st *runState, role string) agent.Decision {
	d := r.policy.For(role, signals(st))
	st.emit(ctx, "effort.decided", true, role, d, nil)
	return d
}
