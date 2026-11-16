package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/session"
)

// LoopInput configures one loop.
type LoopInput struct {
	RC        *RunContext
	Model     ports.ModelProvider
	Guard     ports.Guard
	Registry  *Registry
	Limits    Limits
	Builder   session.Builder
	Context   session.ContextInput // layers 1–5 + task; loop work is appended
	ModelName string
	Fallback  string
	Effort    string
	// Policy and Decision enable auto effort: one escalation after a
	// verified tool failure (research: start low, escalate once on failure).
	Policy   *EffortPolicy
	Decision Decision
	CacheKey  string
	PromptVer string
	// Prefill are tool calls the pipeline already ran (prefetch); the
	// planner sees them as its own.
	Prefill []ports.InputItem
}

// LoopResult is what the loop produced.
type LoopResult struct {
	StopReason StopReason
	Steps      int
	Usage      ports.Usage
	FinalText  string
}

// Loop is the planner loop.
type Loop struct{}

// Run executes the planner until it stops calling tools, a limit is hit, or
// the model fails. Completion criteria are checked by code, not delegated to
// the model: one nudge is injected if candidates are missing (§5).
func (Loop) Run(ctx context.Context, in LoopInput) (LoopResult, error) {
	rc := in.RC
	work := append([]ports.InputItem(nil), in.Prefill...)
	res := LoopResult{}
	effort := in.Effort
	if in.Policy != nil && in.Decision.Effort != "" {
		effort = string(in.Decision.Effort)
	}
	escalated := false
	nudged := false
	retriedTruncation := false
	seen := map[string]int{}
	for {
		if res.Steps >= in.Limits.MaxSteps {
			res.StopReason = StopSteps
			break
		}
		if in.Limits.CostMicros > 0 && res.Usage.CostMicros >= in.Limits.CostMicros {
			res.StopReason = StopCost
			break
		}
		if !in.Limits.Deadline.IsZero() && time.Now().After(in.Limits.Deadline) {
			res.StopReason = StopTime
			break
		}
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		res.Steps++
		cin := in.Context
		cin.LoopWork = work
		instr, input, stats := in.Builder.Build(cin)
		rc.Emitter.Emit(ctx, "context.built", false, "agent_loop", map[string]any{
			"step": res.Steps, "tokens": map[string]int{"prefix": stats.Prefix, "trip_state": stats.TripState, "context_pack": stats.ContextPack,
				"summary": stats.Summary, "window": stats.Window, "loop_work": stats.LoopWork}, "truncated": stats.Truncated,
		}, nil)
		// The loop deadline also bounds a call already in flight: a hung
		// model call must not consume the time narration needs. Hitting it
		// ends the loop (time_limit), not the run.
		callCtx, cancelCall := ctx, context.CancelFunc(func() {})
		if !in.Limits.Deadline.IsZero() {
			callCtx, cancelCall = context.WithDeadline(ctx, in.Limits.Deadline)
		}
		gen, err := in.Model.Generate(callCtx, ports.GenerateRequest{
			Role: "planner", Model: in.ModelName, FallbackModel: in.Fallback, Instructions: instr, Input: input,
			Tools: in.Registry.Specs(), ParallelTools: true, MaxOutputTokens: in.Limits.MaxOutput,
			ReasoningEffort: effort, CacheKey: in.CacheKey,
		})
		res.Usage = res.Usage.Add(gen.Usage)
		rc.Lock()
		rc.Usage = rc.Usage.Add(gen.Usage)
		rc.Unlock()
		u := gen.Usage
		rc.Emitter.Emit(ctx, "model.called", false, "agent_loop", map[string]any{
			"role": "planner", "prompt_version": in.PromptVer, "response_id": gen.ResponseID, "stop_reason": gen.StopReason,
			"tool_calls": len(gen.ToolCalls), "used_fallback": gen.UsedFallback, "error": errString(err),
		}, &EventMeta{Model: gen.Model, Usage: &u, Latency: gen.Latency})
		timedOut := callCtx.Err() != nil && ctx.Err() == nil
		cancelCall()
		if err != nil && timedOut {
			res.StopReason = StopTime
			break
		}
		if err != nil {
			res.StopReason = StopModelFail
			return res, fmt.Errorf("planner: %w", err)
		}
		if len(gen.ToolCalls) == 0 {
			if strings.HasPrefix(gen.StopReason, "incomplete") && !retriedTruncation {
				retriedTruncation = true // truncated response: retry the step once
				continue
			}
			missing := missingCandidates(rc)
			if len(missing) > 0 && !nudged {
				nudged = true
				if gen.Text != "" {
					work = append(work, ports.Message(ports.RoleAssistant, gen.Text))
				}
				work = append(work, ports.Message(ports.RoleUser,
					"Before finishing you still need: "+strings.Join(missing, "; ")+". Use the tools to get them, then call build_packages."))
				continue
			}
			res.StopReason = StopDone
			res.FinalText = gen.Text
			break
		}
		// Record the calls, then run them (in parallel; they are independent
		// read-only tools) and append outputs in call order.
		for _, c := range gen.ToolCalls {
			work = append(work, ports.InputItem{Type: "function_call", CallID: c.CallID, Name: c.Name, Arguments: string(c.Arguments)})
		}
		outputs := make([]string, len(gen.ToolCalls))
		results := make([]ToolResult, len(gen.ToolCalls))
		var wg sync.WaitGroup
		for i, c := range gen.ToolCalls {
			key := c.Name + string(c.Arguments)
			seen[key]++
			repeat := seen[key]
			wg.Add(1)
			go func(i int, c ports.ToolCall, repeat int) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						results[i] = Fail("internal_error", "The tool crashed; try a different approach.", false)
						outputs[i] = results[i].Digest
					}
				}()
				outputs[i], results[i] = runTool(ctx, in, c, repeat)
			}(i, c, repeat)
		}
		wg.Wait()
		// Escalate effort once after a verified failure the model can fix
		// (bad arguments, no combination, expired offers).
		if in.Policy != nil && !escalated {
			for _, r := range results {
				if !r.OK && recoverable(r.Code) {
					d := in.Policy.EscalateAfter(in.Decision, "tool failure: "+r.Code)
					if string(d.Effort) != effort {
						effort = string(d.Effort)
						rc.Emitter.Emit(ctx, "effort.decided", true, "agent_loop", d, nil)
					}
					escalated = true
					break
				}
			}
		}
		builtNow := false
		for i, c := range gen.ToolCalls {
			work = append(work, ports.InputItem{Type: "function_call_output", CallID: c.CallID, Content: outputs[i]})
			if c.Name == "build_packages" {
				builtNow = true
			}
		}
		// Completion is decided by code (§5): once the builder produced a
		// package that fits (or there is no budget) and every candidate kind
		// exists, the loop ends without another model round trip.
		if builtNow && len(missingCandidates(rc)) == 0 && previewUsable(rc) {
			res.StopReason = StopDone
			break
		}
	}
	if res.StopReason != StopDone {
		rc.Emitter.Emit(ctx, "limit.reached", false, "agent_loop", map[string]any{"limit": string(res.StopReason), "steps": res.Steps}, nil)
	}
	return res, nil
}

func missingCandidates(rc *RunContext) []string {
	var m []string
	if !rc.HasFlights() {
		m = append(m, "at least one flight candidate (search_flights)")
	}
	if !rc.HasStays() {
		m = append(m, "stay candidates (search_stays)")
	}
	rc.Lock()
	noPlan := rc.DayPlan == nil
	rc.Unlock()
	if noPlan {
		m = append(m, "a day plan (propose_day_plan)")
	}
	return m
}

// previewUsable reports whether the last build produced a package worth
// keeping. When everything is over budget the planner may still try one
// alternative search, as its prompt allows.
func previewUsable(rc *RunContext) bool {
	rc.Lock()
	defer rc.Unlock()
	if rc.Preview == nil {
		return false
	}
	for _, p := range rc.Preview.Packages {
		if p.Feasibility != "over_budget" {
			return true
		}
	}
	return rc.Count.Flights >= 2 // already tried an alternative
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// RunToolCall executes one tool call outside the loop (short paths such as
// info questions) with the same guard, events and limits.
func RunToolCall(ctx context.Context, in LoopInput, c ports.ToolCall) string {
	out, _ := runTool(ctx, in, c, 1)
	return out
}

// runTool executes one call through the guard and returns the model-facing
// output. Every outcome is an event; failures are structured, never panics.
func runTool(ctx context.Context, in LoopInput, c ports.ToolCall, repeat int) (string, ToolResult) {
	rc := in.RC
	start := time.Now()
	tool, ok := in.Registry.Get(c.Name)
	if !ok {
		out := Fail("unknown_tool", "No tool named "+c.Name+". Use only the declared tools.", false)
		emitTool(ctx, rc, c, out, "unknown", start, nil)
		return out.Digest, out
	}
	var args map[string]any
	if err := json.Unmarshal(c.Arguments, &args); err != nil {
		out := Fail("invalid_arguments", "Arguments must be a JSON object matching the schema.", true)
		emitTool(ctx, rc, c, out, "invalid", start, nil)
		return out.Digest, out
	}
	label := tool.Label(c.Arguments, rc)
	rc.Emitter.Emit(ctx, "tool.started", true, "agent_loop", map[string]any{"tool": c.Name, "label": label}, nil)

	timeout := in.Limits.ToolTimeout
	if c.Name == "web_research" && in.Limits.WebTimeout > 0 {
		timeout = in.Limits.WebTimeout
	}
	var result ToolResult
	exec := func(ctx context.Context) (ports.ToolExecResult, error) {
		tctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		r, err := tool.Execute(tctx, c.Arguments, rc)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				r = Fail("timeout", "The tool timed out; try once more or continue with what you have.", true)
			} else {
				r = Fail("tool_error", "The tool failed: "+trim(err.Error(), 160), true)
			}
		}
		result = r
		origin := r.Origin
		if origin == "" {
			origin = "tool"
		}
		return ports.ToolExecResult{Content: r.Digest, Origin: origin, Sensitive: c.Name == "get_user_memory"}, nil
	}
	rc.Lock()
	sess := rc.GuardSession
	rc.Unlock()
	outcome, err := in.Guard.RunTool(ctx, ports.GuardToolRun{CallID: c.CallID, Name: c.Name, Args: args, Session: sess, Trace: rc.RunID.String()}, exec)
	if len(outcome.Session) > 0 {
		rc.Lock()
		rc.GuardSession = outcome.Session
		rc.Unlock()
	}
	content := outcome.Content
	status := "ok"
	switch {
	case errors.Is(err, ports.ErrToolRefused):
		result = Fail("refused_by_guardrail", "This call was refused by a safety policy. Do not retry it; continue without it.", false)
		content, status = result.Digest, "refused"
	case errors.Is(err, ports.ErrGuardBlocked):
		result = Fail("result_withheld", "The tool returned content that failed a safety check and was withheld. Treat the tool as failed.", false)
		content, status = result.Digest, "withheld"
	case err != nil:
		result = Fail("tool_error", "The tool failed.", true)
		content, status = result.Digest, "error"
	case !result.OK:
		status = "failed:" + result.Code
	}
	if content == "" {
		content = result.Digest
	}
	if repeat >= 3 {
		content += "\nNOTE: you already made this exact call; the result will not change. Move on."
	}
	emitTool(ctx, rc, c, result, status, start, []ports.GuardDecision{outcome.PreDecision})
	if outcome.ResultDecision != nil {
		rc.Emitter.Emit(ctx, "guardrail.checked", false, "agent_loop", outcome.ResultDecision, nil)
	}
	if outcome.PreDecision.Action != "" && outcome.PreDecision.Action != "allow" {
		rc.Emitter.Emit(ctx, "guardrail.checked", false, "agent_loop", outcome.PreDecision, nil)
	}
	summary := result.Summary
	if summary == "" {
		if result.OK {
			summary = "Selesai"
		} else {
			summary = "Tidak berhasil; dilanjutkan dengan data yang ada"
		}
	}
	rc.Emitter.Emit(ctx, "tool.finished", true, "agent_loop", map[string]any{"tool": c.Name, "ok": result.OK, "summary": summary}, nil)
	return content, result
}

func emitTool(ctx context.Context, rc *RunContext, c ports.ToolCall, r ToolResult, status string, start time.Time, _ []ports.GuardDecision) {
	rc.Emitter.Emit(ctx, "tool.called", false, "agent_loop", map[string]any{
		"name": c.Name, "call_id": c.CallID, "arguments": json.RawMessage(validJSON(c.Arguments)), "artifact_id": r.ArtifactID,
		"digest": trim(r.Digest, 4000), "status": status, "cache_hit": r.CacheHit, "code": r.Code,
	}, &EventMeta{Latency: time.Since(start)})
}

func validJSON(b []byte) []byte {
	if json.Valid(b) {
		return b
	}
	q, _ := json.Marshal(string(b))
	return q
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// recoverable reports failures a stronger reasoning pass can fix. Quota,
// sales-window and supplier outages are not the model's fault: escalating
// would only cost time.
func recoverable(code string) bool {
	switch code {
	case "invalid_arguments", "no_combination", "offers_expired", "unknown_ids", "unknown_day_plan", "invalid_plan", "unknown_area", "unknown_tool":
		return true
	}
	return false
}
