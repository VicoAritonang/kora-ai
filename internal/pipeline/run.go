package pipeline

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/koraai/koraai/internal/agent/prompts"
	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/session"
)

// run executes the ten steps; short paths skip to narration (§3).
func (r *Runner) run(ctx context.Context, st *runState) error {
	d := &r.d
	q := d.DB.Q()
	var err error
	if st.run, err = q.GetRun(ctx, st.in.RunID); err != nil {
		return fail("internal_error", true, fmt.Errorf("load run: %w", err))
	}
	if st.conv, err = q.GetConversationByID(ctx, st.in.ConversationID); err != nil {
		return fail("internal_error", true, err)
	}
	if st.user, err = q.GetUser(ctx, st.in.UserID); err != nil {
		return fail("internal_error", true, err)
	}
	if st.userMsg, err = q.GetMessage(ctx, st.in.MessageID); err != nil {
		return fail("internal_error", true, err)
	}
	st.state, st.before = st.conv.TripState, st.conv.TripState
	if st.guardSession, _ = q.GuardSession(ctx, st.in.ConversationID); len(st.guardSession) == 0 {
		st.guardSession = d.Guard.NewSession()
	}

	r.wsReset(ctx, st)
	st.emit(ctx, "run.started", false, "start", map[string]any{"config": st.run.ConfigSnapshot, "message_id": st.userMsg.ID, "message_chars": len(st.userMsg.Content)}, nil)
	st.status(ctx, "understanding")

	// 1. Guardrail in.
	if err := r.guardIn(ctx, st); err != nil {
		return err
	}
	// 2–3. Route and extract.
	if err := r.route(ctx, st); err != nil {
		return err
	}
	st.emit(ctx, "run.started", true, "start", map[string]any{"run_id": st.in.RunID, "mode": st.route.Intent}, nil)
	_ = q.SetRunMode(ctx, st.in.RunID, st.route.Intent)

	switch st.route.Intent {
	case domain.IntentChitchat:
		return r.finishShort(ctx, st, r.chitchat(ctx, st))
	case domain.IntentOutOfScope:
		return r.finishShort(ctx, st, outOfScopeMessage(st.lang(), st.unsupported))
	}
	if err := r.extract(ctx, st); err != nil {
		return err
	}
	if st.unsupported != "" {
		st.route.Intent = domain.IntentOutOfScope
		_ = q.SetRunMode(ctx, st.in.RunID, st.route.Intent)
		return r.finishShort(ctx, st, outOfScopeMessage(st.lang(), st.unsupported))
	}
	if st.route.Intent == domain.IntentInfo {
		text, err := r.info(ctx, st)
		if err != nil {
			return err
		}
		return r.finishShort(ctx, st, text)
	}

	// 4. Clarify or assume.
	ask, err := r.clarify(ctx, st)
	if err != nil {
		return err
	}
	if ask {
		return r.finishClarification(ctx, st)
	}
	_ = q.SetRunMode(ctx, st.in.RunID, st.route.Intent)

	if st.route.Intent == domain.IntentBudgetToDestination {
		text, err := r.budgetToDestination(ctx, st)
		if err != nil {
			return err
		}
		return r.finishShort(ctx, st, text)
	}

	// 5. Retrieval pipeline.
	if err := r.retrieve(ctx, st); err != nil {
		return err
	}
	// 6. Agent loop.
	if err := r.plan(ctx, st, nil); err != nil {
		return err
	}
	// 7–8. Build and verify.
	if err := r.buildAndVerify(ctx, st); err != nil {
		return err
	}
	// Verified failure → one retry at escalated effort, if time allows.
	if len(st.packages) == 0 && st.rc != nil && time.Since(st.started) < r.d.Cfg.Limits.RunTimeout*2/5 {
		prev := r.policy.For("planner", signals(st))
		if next := r.policy.EscalateAfter(prev, "no packages built"); next.Effort != prev.Effort || prev.Mode == "fixed" {
			st.emit(ctx, "effort.decided", true, "planner", next, nil)
			st.flag("planner_retry")
			if err := r.plan(ctx, st, &next); err != nil {
				return err
			}
			if err := r.buildAndVerify(ctx, st); err != nil {
				return err
			}
		}
	}
	// 9–10. Narrate with guard-out per paragraph, then the soft rubric.
	text := r.narrate(ctx, st)
	r.rubric(ctx, st, text)
	return r.finish(ctx, st, text, map[string]any{"packages": packageBlocks(st.packages), "assumptions": st.req.Assumptions, "notices": notices(st)})
}

// guardIn runs Guard.Inbound; the rest of the pipeline only ever sees the
// rewritten message (§9.1 point 1).
func (r *Runner) guardIn(ctx context.Context, st *runState) error {
	res, err := r.d.Guard.Inbound(ctx, ports.GuardInbound{
		System:   "Kora travel planner",
		Messages: []ports.GuardMessage{{Role: "user", Content: st.userMsg.Content}},
	})
	st.emit(ctx, "guardrail.checked", false, "guard_in", res.Decision, nil)
	if errors.Is(err, ports.ErrGuardBlocked) {
		return &runError{Code: "blocked_by_guardrail", Status: "blocked", Err: err}
	}
	if err != nil {
		// Fail closed at the input point (SYSTEM_ARCHITECTURE §9).
		return &runError{Code: "blocked_by_guardrail", Status: "blocked", Err: fmt.Errorf("guard unavailable: %w", err)}
	}
	st.vault = res.Vault
	st.text = st.userMsg.Content
	if len(res.Messages) > 0 {
		st.text = res.Messages[len(res.Messages)-1].Content
	}
	// The masked text only: traces never carry what the guard rewrote away.
	st.emit(ctx, "input.received", false, "guard_in", map[string]any{"text": st.text, "chars": len([]rune(st.text))}, nil)
	return nil
}

// finishShort completes a run that answers without packages.
func (r *Runner) finishShort(ctx context.Context, st *runState, text string) error {
	if len(st.sent) == 0 && text != "" {
		text = r.streamParagraphs(ctx, st, text, nil)
	} else {
		text = joinSent(st)
	}
	blocks := map[string]any{}
	if len(st.dests) > 0 {
		blocks["destinations"] = st.dests
	}
	if len(st.req.Assumptions) > 0 {
		blocks["assumptions"] = st.req.Assumptions
	}
	if n := notices(st); len(n) > 0 {
		blocks["notices"] = n
	}
	return r.finish(ctx, st, text, blocks)
}

func (r *Runner) finishClarification(ctx context.Context, st *runState) error {
	question, options := clarifyQuestion(st.lang())
	st.clarification = question
	st.emit(ctx, "clarification", true, "clarify", map[string]any{"question": question, "options": options}, nil)
	st.state.LastTurnClarified = true
	text := r.streamParagraphs(ctx, st, question, nil)
	return r.finish(ctx, st, text, map[string]any{"clarification": map[string]any{"question": question, "options": options}})
}

// finish saves the assistant message and state, then completes the run.
func (r *Runner) finish(ctx context.Context, st *runState, text string, blocks map[string]any) error {
	d := &r.d
	ctx = context.WithoutCancel(ctx)
	q := d.DB.Q()
	if st.clarification == "" {
		st.state.LastTurnClarified = false
	}
	if saved, err := (session.StateStore{DB: d.DB}).Save(ctx, st.in.ConversationID, st.state); err == nil {
		st.state = saved
		st.emit(ctx, "trip_state.updated", true, "finish", map[string]any{"trip_state": st.state}, nil)
	} else if errors.Is(err, session.ErrStateConflict) {
		st.flag("trip_state_conflict")
	}
	msg, err := q.InsertMessage(ctx, platform.NewID(), st.in.ConversationID, "assistant", text, blocks, &st.in.RunID)
	if err != nil {
		return fail("internal_error", true, err)
	}
	if st.guardSession != nil {
		_ = q.SaveGuardSession(ctx, st.in.ConversationID, st.guardSession)
	}
	if st.req.Destination != nil && st.route.Intent == domain.IntentPlanTrip {
		_ = q.SetTitleIfEmpty(ctx, st.in.ConversationID, st.req.Destination.Label+" · "+windowLabel(st.req.Window))
	} else if st.route.Intent == domain.IntentBudgetToDestination && st.req.BudgetIDR != nil {
		_ = q.SetTitleIfEmpty(ctx, st.in.ConversationID, "Budget "+shortJuta(*st.req.BudgetIDR)+" · ide destinasi")
	}
	st.emit(ctx, "narration.done", false, "narrate", map[string]any{"message_id": msg.ID, "paragraphs": len(st.sent)}, nil)
	st.emit(ctx, "message.completed", true, "finish", map[string]any{"message_id": msg.ID}, nil)
	tot := st.totals()
	st.emit(ctx, "run.finished", false, "finish", map[string]any{"status": "completed", "flags": tot.Flags, "usage": map[string]int64{
		"input_tokens": tot.InputTokens, "cached_tokens": tot.CachedTokens, "output_tokens": tot.OutputTokens, "cost_usd_micros": tot.CostMicros},
		"duration_ms": time.Since(st.started).Milliseconds(), "steps": tot.Steps}, nil)
	if err := q.FinishRun(ctx, st.in.RunID, "completed", nil, tot); err != nil {
		d.Log.Error("finish run failed", "err", err)
	}
	st.emit(ctx, "run.completed", true, "finish", map[string]any{"run_id": st.in.RunID}, nil)
	if d.Jobs != nil {
		_ = d.Jobs.Enqueue(ctx, "summarize", map[string]string{"conversation_id": st.in.ConversationID.String()})
		if d.Cfg.Memory.Enabled && st.user.MemoryEnabled && !st.user.IsGuest {
			_ = d.Jobs.Enqueue(ctx, "memory_write", map[string]string{"conversation_id": st.in.ConversationID.String(), "run_id": st.in.RunID.String()})
		}
	}
	return nil
}

func shortJuta(idr int64) string {
	if idr >= 1_000_000 {
		j := float64(idr) / 1_000_000
		if j == float64(int64(j)) {
			return fmt.Sprintf("%d juta", int64(j))
		}
		return fmt.Sprintf("%.1f juta", j)
	}
	return fmt.Sprintf("Rp %d", idr)
}

func joinSent(st *runState) string {
	out := ""
	for i, p := range st.sent {
		if i > 0 {
			out += "\n\n"
		}
		out += p
	}
	return out
}

func notices(st *runState) []string {
	var out []string
	if st.rc != nil {
		st.rc.Lock()
		for _, n := range st.rc.Notices {
			out = append(out, n.Text)
		}
		st.rc.Unlock()
	}
	return out
}

func packageBlocks(pkgs []domain.Package) []map[string]any {
	var out []map[string]any
	for _, p := range pkgs {
		out = append(out, map[string]any{"id": p.ID, "tier": p.Tier, "title": p.Title, "total": domain.IDR(p.TotalIDR), "feasibility": p.Feasibility})
	}
	return out
}

func configSnapshotPrompts() map[string]string { return prompts.Versions() }
