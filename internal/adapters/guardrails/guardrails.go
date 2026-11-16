// Package guardrails adapts guard-rails-engine (in process) to ports.Guard.
// Engine types never leave this package (rule D6).
package guardrails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	gr "go.vicoworks.com/guard-rails-engine/pkg/guardrails"

	"github.com/koraai/koraai/internal/ports"
)

// Capabilities granted to the agent session; they match guardrails/tools.yaml.
var Capabilities = []string{"travel:search", "knowledge:read", "memory:read", "web:read", "planning:compute"}

// Engine wraps one guardrails.Engine built at start-up.
type Engine struct {
	g             *gr.Engine
	policyVersion string
}

// New builds the engine from the policy and tool catalog files. The secret
// must be the same on every instance so vault placeholders and sessions are
// valid across instances (MODULE_SPEC §16).
func New(policyFile, toolsFile string, secret []byte) (*Engine, error) {
	pol, err := gr.LoadPolicy(policyFile)
	if err != nil {
		return nil, fmt.Errorf("guardrails: load policy: %w", err)
	}
	g, err := gr.New(gr.Options{Policy: pol, Secret: secret, ToolCatalogFile: toolsFile})
	if err != nil {
		return nil, err
	}
	return &Engine{g: g, policyVersion: pol.Name + "@" + pol.Version}, nil
}

// PolicyVersion identifies the loaded policy (runs.config_snapshot).
func (e *Engine) PolicyVersion() string { return e.policyVersion }

// NewSession returns a fresh agent session granting the catalog capabilities.
func (e *Engine) NewSession() []byte {
	b, _ := json.Marshal(&gr.Session{Granted: Capabilities})
	return b
}

func decision(point string, d *gr.Decision) ports.GuardDecision {
	out := ports.GuardDecision{Point: point, Action: "allow"}
	if d == nil {
		return out
	}
	out.Action = string(d.Action)
	out.PolicyVersion = d.PolicyVersion
	out.DurationMs = float64(d.Duration.Microseconds()) / 1000
	out.Degraded = d.Degraded
	for _, f := range d.Findings {
		out.Findings = append(out.Findings, ports.GuardFinding{Guard: f.Guard, Category: f.Category, Severity: f.SeverityName, Score: f.Score})
	}
	if d.Blocked() {
		out.Reason = d.Reason()
	}
	return out
}

// Inbound masks PII and blocks attacks before any model sees the message.
func (e *Engine) Inbound(ctx context.Context, in ports.GuardInbound) (ports.GuardInboundResult, error) {
	msgs := make([]gr.Message, 0, len(in.Messages))
	for _, m := range in.Messages {
		msgs = append(msgs, gr.Message{Role: gr.Role(m.Role), Content: m.Content})
	}
	res, err := e.g.Inbound(ctx, gr.Request{System: in.System, Messages: msgs})
	out := ports.GuardInboundResult{}
	if res != nil {
		out.Decision = decision("input", res.Decision)
		out.Vault = res.Vault
		for _, m := range res.Messages {
			out.Messages = append(out.Messages, ports.GuardMessage{Role: string(m.Role), Content: m.Content})
		}
	}
	if err != nil {
		if errors.Is(err, gr.ErrBlocked) {
			return out, fmt.Errorf("%w: %s", ports.ErrGuardBlocked, out.Decision.Reason)
		}
		return out, err
	}
	return out, nil
}

// RunTool assesses the call, runs exec only if allowed, checks the result
// with the tool-result rails and returns the updated session.
func (e *Engine) RunTool(ctx context.Context, run ports.GuardToolRun, exec ports.ToolExec) (ports.GuardToolOutcome, error) {
	sess := &gr.Session{}
	if len(run.Session) > 0 {
		if err := json.Unmarshal(run.Session, sess); err != nil {
			sess = &gr.Session{}
		}
	}
	if len(sess.Granted) == 0 {
		sess.Granted = Capabilities
	}
	if run.Trace != "" && sess.TraceID != run.Trace {
		sess.TraceID = run.Trace
		sess.ToolCalls = nil // per-run counts; Kora's own quotas also apply
	}
	raw, _ := json.Marshal(run.Args)
	call := gr.ToolCall{ID: run.CallID, Name: run.Name, Arguments: run.Args, Raw: string(raw)}
	var execErr error
	o, err := e.g.ExecuteTool(ctx, gr.ToolRun{Call: call, Session: sess}, gr.ToolFuncs{
		ExecuteFunc: func(ctx context.Context, c gr.ToolCall) (gr.ToolResult, error) {
			r, err := exec(ctx)
			if err != nil {
				execErr = err
				// The model still receives a structured error; the guard sees
				// it as the tool's output.
				return gr.ToolResult{CallID: c.ID, Name: c.Name, Content: r.Content, Error: err.Error()}, nil
			}
			origin := gr.ToolResult{}.Origin
			switch r.Origin {
			case "web":
				origin = "web"
			default:
				origin = "tool"
			}
			return gr.ToolResult{CallID: c.ID, Name: c.Name, Content: r.Content, Origin: origin, Sensitive: r.Sensitive}, nil
		},
	})
	out := ports.GuardToolOutcome{}
	if o != nil {
		out.Executed = o.Executed
		out.PreDecision = ports.GuardDecision{Point: "pre_tool", Action: "allow"}
		if !o.Assessment.Allowed() {
			out.PreDecision.Action = "block"
		}
		for _, f := range o.Assessment.Findings {
			out.PreDecision.Findings = append(out.PreDecision.Findings, ports.GuardFinding{Guard: f.Guard, Category: f.Category, Severity: f.SeverityName, Score: f.Score})
		}
		if o.ResultDecision != nil {
			d := decision("post_tool", o.ResultDecision)
			out.ResultDecision = &d
		}
		if o.Result != nil {
			out.Content = o.Result.Content
		}
		if o.Session != nil {
			out.Session, _ = json.Marshal(o.Session)
		}
	}
	var refused *gr.ToolRefusedError
	switch {
	case errors.As(err, &refused):
		out.Reason = refused.Reason
		out.PreDecision.Reason = refused.Reason
		return out, fmt.Errorf("%w: %s", ports.ErrToolRefused, refused.Reason)
	case errors.Is(err, gr.ErrBlocked):
		out.Withheld = true
		out.Content = ""
		if out.ResultDecision != nil {
			out.Reason = out.ResultDecision.Reason
		}
		return out, fmt.Errorf("%w: tool result withheld", ports.ErrGuardBlocked)
	case err != nil && execErr == nil:
		return out, err
	}
	if execErr != nil {
		return out, execErr
	}
	return out, nil
}

// Outbound checks a completion and restores masked values from the vault.
func (e *Engine) Outbound(ctx context.Context, in ports.GuardOutbound) (ports.GuardOutboundResult, error) {
	var vault *gr.Vault
	if v, ok := in.Vault.(*gr.Vault); ok {
		vault = v
	}
	res, err := e.g.Outbound(ctx, gr.Request{System: in.System, Completion: in.Completion, Context: in.Context, Vault: vault})
	out := ports.GuardOutboundResult{Completion: in.Completion}
	if res != nil {
		out.Decision = decision("output", res.Decision)
		out.Completion = res.Completion
	}
	if err != nil {
		if errors.Is(err, gr.ErrBlocked) {
			return out, fmt.Errorf("%w: %s", ports.ErrGuardBlocked, out.Decision.Reason)
		}
		return out, err
	}
	return out, nil
}

// Off is a pass-through guard for local debugging only (GUARDRAILS_MODE=off).
// Config refuses it outside APP_ENV=local.
type Off struct{}

// PolicyVersion reports the disabled guard.
func (Off) PolicyVersion() string { return "off" }

// NewSession returns an empty session.
func (Off) NewSession() []byte { return []byte(`{}`) }

// Inbound passes the messages through.
func (Off) Inbound(_ context.Context, in ports.GuardInbound) (ports.GuardInboundResult, error) {
	return ports.GuardInboundResult{Messages: in.Messages, Decision: ports.GuardDecision{Point: "input", Action: "allow"}}, nil
}

// RunTool runs the tool.
func (Off) RunTool(ctx context.Context, run ports.GuardToolRun, exec ports.ToolExec) (ports.GuardToolOutcome, error) {
	r, err := exec(ctx)
	return ports.GuardToolOutcome{Executed: true, Content: r.Content, Session: run.Session,
		PreDecision: ports.GuardDecision{Point: "pre_tool", Action: "allow"}}, err
}

// Outbound passes the completion through.
func (Off) Outbound(_ context.Context, in ports.GuardOutbound) (ports.GuardOutboundResult, error) {
	return ports.GuardOutboundResult{Completion: in.Completion, Decision: ports.GuardDecision{Point: "output", Action: "allow"}}, nil
}
