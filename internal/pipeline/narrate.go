package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/koraai/koraai/internal/agent"
	"github.com/koraai/koraai/internal/agent/prompts"
	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/store/postgres"
	"github.com/koraai/koraai/internal/verifier"
)

type postgresQ = postgres.Q

// narrate writes the explanation (gpt-5-mini, streaming). Paragraphs are
// released one at a time after a deterministic number check and
// Guard.Outbound (§9.1 point 5). On model failure a template is used.
func (r *Runner) narrate(ctx context.Context, st *runState) string {
	d := &r.d
	st.status(ctx, "writing")
	if len(st.packages) == 0 {
		text := userMessage("no_packages", st.lang())
		for _, n := range notices(st) {
			text += "\n\n" + n
		}
		return r.streamParagraphs(ctx, st, text, nil)
	}
	materials := r.narrationMaterials(st)
	instr := prompts.Narrator(d.Persona.Core())
	dec := r.effort(ctx, st, "narrator")
	sp := newParagraphStream(ctx, r, st, st.packages, instr)
	res, err := d.Model.Stream(ctx, ports.GenerateRequest{
		Role: "narrator", Model: d.Cfg.Models.Narrator, FallbackModel: d.Cfg.Models.NarratorFallback, Instructions: instr,
		Input:           []ports.InputItem{ports.Message(ports.RoleUser, materials)},
		MaxOutputTokens: 2500, ReasoningEffort: string(dec.Effort), CacheKey: "kora-narrator-" + prompts.NarratorVersion,
	}, sp.feed)
	st.addUsage(res.Usage)
	u := res.Usage
	st.emit(ctx, "model.called", false, "narrate", map[string]any{"role": "narrator", "prompt_version": prompts.NarratorVersion, "response_id": res.ResponseID,
		"used_fallback": res.UsedFallback, "error": errStr(err)}, &agent.EventMeta{Model: res.Model, Usage: &u, Latency: res.Latency})
	sp.flush()
	// Verified failure (every paragraph withheld by the checks) → one retry
	// at escalated effort with the reason, before the template.
	if err == nil && len(st.sent) == 0 {
		if next := r.policy.EscalateAfter(dec, "narration withheld by number check"); next.Effort != dec.Effort {
			st.emit(ctx, "effort.decided", true, "narrator", next, nil)
			sp = newParagraphStream(ctx, r, st, st.packages, instr)
			res2, err2 := d.Model.Stream(ctx, ports.GenerateRequest{
				Role: "narrator", Model: d.Cfg.Models.Narrator, FallbackModel: d.Cfg.Models.NarratorFallback, Instructions: instr,
				Input:           []ports.InputItem{ports.Message(ports.RoleUser, materials+"\n\nIMPORTANT: your previous answer was rejected because it contained amounts not present in <packages>. Copy every amount exactly from <packages>.")},
				MaxOutputTokens: 2500, ReasoningEffort: string(next.Effort),
			}, sp.feed)
			sp.flush()
			st.addUsage(res2.Usage)
			err = err2
		}
	}
	if err != nil || len(st.sent) == 0 {
		st.flag("narration_template")
		if len(st.sent) == 0 {
			return r.streamParagraphs(ctx, st, templateNarration(st.packages, notices(st), isSandbox(st.packages)), st.packages)
		}
	}
	return joinSent(st)
}

func isSandbox(pkgs []domain.Package) bool {
	for _, p := range pkgs {
		for _, it := range p.Items {
			if strings.HasPrefix(it.SourceLabel, "Sandbox") {
				return true
			}
		}
	}
	return false
}

func (r *Runner) narrationMaterials(st *runState) string {
	var b strings.Builder
	b.WriteString("<packages>\n")
	for _, p := range st.packages {
		b.WriteString(verifier.PackageFacts(p) + "\n")
	}
	b.WriteString("</packages>\n\n<assumptions>\n")
	for _, a := range st.req.Assumptions {
		fmt.Fprintf(&b, "- %s: %v — %s\n", a.Slot, a.Value, a.Reason)
	}
	b.WriteString("</assumptions>\n\n")
	if len(st.req.Special) > 0 {
		b.WriteString("<special_requests>\n")
		for _, s := range st.req.Special {
			fmt.Fprintf(&b, "- %s: %s\n", s.Kind, s.Text)
		}
		b.WriteString("</special_requests>\n\n")
	}
	units := st.pack.Units
	if st.rc != nil {
		st.rc.Lock()
		units = append(units, st.rc.Knowledge...)
		web := append([]agent.WebFact{}, st.rc.WebFacts...)
		st.rc.Unlock()
		if len(web) > 0 {
			wb, _ := json.Marshal(web)
			b.WriteString(`<web_facts><untrusted source="web">` + string(wb) + "</untrusted></web_facts>\n\n")
		}
	}
	b.WriteString(knowledgeBlock(r.d.Persona.Version(), dedupeUnits(units, 14), nil) + "\n\n")
	if n := notices(st); len(n) > 0 {
		b.WriteString("<notices>\n" + strings.Join(n, "\n") + "\n</notices>\n\n")
	}
	if st.plannerNote != "" {
		b.WriteString("<planner_note>" + trimRunes(st.plannerNote, 800) + "</planner_note>\n\n")
	}
	lang := "Bahasa Indonesia"
	if st.lang() == "en" {
		lang = "English"
	}
	b.WriteString("<traveller_message>" + trimRunes(st.text, 800) + "</traveller_message>\n\nWrite the answer in " + lang + ".")
	return b.String()
}

func dedupeUnits(units []ports.KnowledgeUnit, n int) []ports.KnowledgeUnit {
	seen := map[string]bool{}
	var out []ports.KnowledgeUnit
	for _, u := range units {
		if seen[u.Ref] {
			continue
		}
		seen[u.Ref] = true
		out = append(out, u)
		if len(out) >= n {
			break
		}
	}
	return out
}

// paragraphStream buffers deltas and releases complete paragraphs.
type paragraphStream struct {
	ctx  context.Context
	r    *Runner
	st   *runState
	pkgs []domain.Package
	sys  string
	buf  strings.Builder
}

func newParagraphStream(ctx context.Context, r *Runner, st *runState, pkgs []domain.Package, sys string) *paragraphStream {
	return &paragraphStream{ctx: ctx, r: r, st: st, pkgs: pkgs, sys: sys}
}

func (p *paragraphStream) feed(delta string) {
	p.buf.WriteString(delta)
	for {
		s := p.buf.String()
		i := strings.Index(s, "\n\n")
		if i < 0 {
			return
		}
		para := strings.TrimSpace(s[:i])
		p.buf.Reset()
		p.buf.WriteString(s[i+2:])
		if para != "" {
			p.r.releaseParagraph(p.ctx, p.st, para, p.pkgs, p.sys)
		}
	}
}

func (p *paragraphStream) flush() {
	if para := strings.TrimSpace(p.buf.String()); para != "" {
		p.r.releaseParagraph(p.ctx, p.st, para, p.pkgs, p.sys)
	}
	p.buf.Reset()
}

// releaseParagraph checks one paragraph and sends it as message.delta.
// A paragraph with a money amount found in no package is withheld (P1).
func (r *Runner) releaseParagraph(ctx context.Context, st *runState, para string, pkgs []domain.Package, sys string) {
	if pkgs != nil {
		if bad := verifier.NumbersNotInPackages(para, pkgs); len(bad) > 0 {
			st.flag("narration_number_withheld")
			st.emit(ctx, "verify.done", false, "narrate", map[string]any{"withheld_paragraph": true, "unknown_amounts": bad}, nil)
			return
		}
	}
	out, err := r.d.Guard.Outbound(ctx, ports.GuardOutbound{System: sys, Completion: para, Vault: st.vault})
	if out.Decision.Action != "" && out.Decision.Action != "allow" {
		st.emit(ctx, "guardrail.checked", false, "guard_out", out.Decision, nil)
	}
	if errors.Is(err, ports.ErrGuardBlocked) {
		st.flag("output_paragraph_blocked")
		return
	}
	text := out.Completion
	if err != nil {
		// Fail closed at the output point: drop the paragraph.
		st.flag("guard_out_unavailable")
		return
	}
	st.mu.Lock()
	first := len(st.sent) == 0
	st.sent = append(st.sent, text)
	st.mu.Unlock()
	if !first {
		text = "\n\n" + text
	}
	st.emit(ctx, "message.delta", true, "narrate", map[string]string{"text": text}, nil)
}

// streamParagraphs sends a fixed text (template, clarification) through the
// same paragraph checks.
func (r *Runner) streamParagraphs(ctx context.Context, st *runState, text string, pkgs []domain.Package) string {
	for _, para := range strings.Split(text, "\n\n") {
		if para = strings.TrimSpace(para); para != "" {
			r.releaseParagraph(ctx, st, para, pkgs, "")
		}
	}
	return joinSent(st)
}

// rubric runs the JEV rubric on the final narration (soft check, §10).
func (r *Runner) rubric(ctx context.Context, st *runState, text string) {
	if len(st.packages) == 0 || r.d.Verifier == nil {
		return
	}
	var know, untrusted []string
	for _, u := range dedupeUnits(st.pack.Units, 14) {
		know = append(know, u.Title+": "+u.Body)
	}
	if st.rc != nil {
		st.rc.Lock()
		for _, f := range st.rc.WebFacts {
			untrusted = append(untrusted, f.Claim+" ("+f.URL+")")
		}
		st.rc.Unlock()
	}
	answers, usage, err := r.d.Verifier.CheckRubric(ctx, verifier.RubricInput{Narration: text, Packages: st.packages, Special: st.req.Special, Knowledge: know, Untrusted: untrusted})
	st.addUsage(usage)
	if err != nil {
		st.flag("verification_skipped")
	}
	passed := verifier.Passed(answers)
	if !passed && !verifier.Serious(answers) {
		st.flag("verification_low_confidence") // for AI Lab review, not shown
	} else if !passed {
		st.flag("verification_failed")
		msg := "Catatan: pemeriksaan otomatis menemukan bagian penjelasan yang mungkin tidak sepenuhnya didukung data. Angka di kartu paket adalah acuan yang pasti."
		if st.lang() == "en" {
			msg = "Note: an automatic check found parts of this explanation that may not be fully supported. The package cards are the reference."
		}
		st.emit(ctx, "notice", true, "verify", map[string]any{"code": "verification_warning", "text": msg}, nil)
	}
	st.emit(ctx, "verify.done", false, "verify", map[string]any{"rubric": answers, "passed": passed, "error": errStr(err)}, &agent.EventMeta{Usage: &usage})
}

// ------------------------------------------------------------ short paths

func (r *Runner) chitchat(ctx context.Context, st *runState) string {
	d := &r.d
	st.status(ctx, "writing")
	res, err := d.Model.Generate(ctx, ports.GenerateRequest{
		Role: "chitchat", Model: d.Cfg.Models.Extractor, FallbackModel: d.Cfg.Models.ExtractorFallback, Instructions: prompts.Chitchat(),
		Input: []ports.InputItem{ports.Message(ports.RoleUser, st.text)}, MaxOutputTokens: 600, ReasoningEffort: string(r.effort(ctx, st, "chitchat").Effort),
	})
	st.addUsage(res.Usage)
	u := res.Usage
	st.emit(ctx, "model.called", false, "narrate", map[string]any{"role": "chitchat", "prompt_version": prompts.ChitchatVersion, "error": errStr(err)}, &agent.EventMeta{Model: res.Model, Usage: &u, Latency: res.Latency})
	if err != nil || strings.TrimSpace(res.Text) == "" {
		q, _ := clarifyQuestion(st.lang())
		return "Halo! Saya Kora. " + q
	}
	return res.Text
}

// info answers a travel question from the persona pack, with one isolated
// web research when the pack has a gap and fresh information is needed.
func (r *Runner) info(ctx context.Context, st *runState) (string, error) {
	d := &r.d
	if d.Persona == nil {
		return "", fail("kg_unavailable", false, errors.New("persona pack not loaded"))
	}
	st.status(ctx, "retrieving_knowledge")
	q := []ports.KnowledgeQuery{{Purpose: "question", Text: st.text, Units: 6}}
	if st.state.Destination != nil {
		q = append(q, ports.KnowledgeQuery{Purpose: "destination", Text: st.state.Destination.Label + " " + st.text, Units: 4})
	}
	pack, err := d.Persona.Retrieve(ctx, q, 2500)
	if err != nil {
		return "", fail("kg_unavailable", true, err)
	}
	st.pack = pack
	st.emit(ctx, "retrieval.done", false, "retrieve", map[string]any{"units": len(pack.Units), "gaps": pack.Gaps}, nil)
	materials := knowledgeBlock(d.Persona.Version(), pack.Units, pack.Gaps)
	if len(pack.Gaps) > 0 && st.route.NeedsFreshInfo {
		st.status(ctx, "researching_web")
		req := domain.TripRequest{Destination: st.state.Destination}
		rc := agent.NewRunContext(st.in.RunID, st.in.ConversationID, st.in.UserID, req, d.Clock.Now)
		rc.Emitter, rc.GuardSession = st, st.guardSession
		st.rc = rc
		args, _ := json.Marshal(map[string]string{"question": trimRunes(st.text, 380), "reason": "Traveller asked a time-bound question the knowledge base does not cover."})
		out := agent.RunToolCall(ctx, agent.LoopInput{RC: rc, Guard: d.Guard, Registry: d.Registry,
			Limits: agent.Limits{WebResearch: d.Cfg.Limits.WebResearch, ToolTimeout: d.Cfg.Limits.ToolTimeout, WebTimeout: d.Cfg.Limits.WebTimeout}},
			ports.ToolCall{CallID: "info-web", Name: "web_research", Arguments: args})
		st.guardSession = rc.GuardSession
		if strings.Contains(out, `"ok":true`) {
			materials += "\n<web_facts>" + out + "</web_facts>"
		}
	}
	st.status(ctx, "writing")
	lang := "Bahasa Indonesia"
	if st.lang() == "en" {
		lang = "English"
	}
	res, err := d.Model.Generate(ctx, ports.GenerateRequest{
		Role: "info", Model: d.Cfg.Models.Narrator, FallbackModel: d.Cfg.Models.NarratorFallback, Instructions: prompts.Info(d.Persona.Core()),
		Input:           []ports.InputItem{ports.Message(ports.RoleUser, materials+"\n\n<question>"+st.text+"</question>\nAnswer in "+lang+".")},
		MaxOutputTokens: 1500, ReasoningEffort: string(r.effort(ctx, st, "info").Effort),
	})
	st.addUsage(res.Usage)
	u := res.Usage
	st.emit(ctx, "model.called", false, "narrate", map[string]any{"role": "info", "prompt_version": prompts.InfoVersion, "error": errStr(err)}, &agent.EventMeta{Model: res.Model, Usage: &u, Latency: res.Latency})
	if err != nil {
		return "", fail("model_unavailable", true, err)
	}
	return res.Text, nil
}

// budgetToDestination answers "Rp Y — where?" from the Destination Cost
// Index only; no live supplier calls (FR-13).
func (r *Runner) budgetToDestination(ctx context.Context, st *runState) (string, error) {
	d := &r.d
	st.status(ctx, "estimating")
	req := st.req
	dests, err := d.Index.EstimateTrip(ctx, req.Origin.Code, req.DepartDate, req.Nights, req.Pax, req.BudgetIDR, d.Cfg.Policy.ContingencyPct)
	if err != nil || len(dests) == 0 {
		msg := "Indeks Biaya Destinasi belum terisi untuk bulan ini, jadi saya belum bisa membandingkan destinasi dari budget saja. Sebutkan tujuan (misalnya Osaka) dan saya hitungkan paketnya."
		return msg, nil
	}
	st.dests = dests
	st.emit(ctx, "destinations.ready", true, "estimate", map[string]any{"destinations": dests, "nights": req.Nights, "month": req.DepartDate.FirstOfMonth()}, nil)

	var b strings.Builder
	b.WriteString("<packages>\nAll amounts below are ESTIMATES from the Destination Cost Index (no live prices).\n")
	var known []domain.Package
	for _, x := range dests {
		fmt.Fprintf(&b, "- %s (%s, %d malam): flight %s;", x.Label, windowLabel(req.Window), x.Nights, verifier.Rupiah(x.FlightIDR))
		for _, t := range domain.Tiers {
			if v, ok := x.TotalIDR[string(t)]; ok {
				fits := ""
				if req.BudgetIDR != nil {
					fits = map[bool]string{true: " (muat)", false: " (tidak muat)"}[x.Fits[string(t)]]
				}
				fmt.Fprintf(&b, " %s total %s%s;", t.Label(), verifier.Rupiah(v), fits)
				known = append(known, domain.Package{TotalIDR: v})
			}
		}
		known = append(known, domain.Package{TotalIDR: x.FlightIDR})
		b.WriteString("\n")
	}
	if req.BudgetIDR != nil {
		fmt.Fprintf(&b, "Budget: %s\n", verifier.Rupiah(*req.BudgetIDR))
		known = append(known, domain.Package{TotalIDR: *req.BudgetIDR})
	}
	b.WriteString("</packages>\n<assumptions>\n")
	for _, a := range req.Assumptions {
		fmt.Fprintf(&b, "- %s: %v — %s\n", a.Slot, a.Value, a.Reason)
	}
	b.WriteString("</assumptions>\nTask: the traveller gave a budget and asked where they can go. Recommend the destinations that fit, ordered by total, say all amounts are estimates and that exact packages with live prices come once they pick one. 120–220 words.")
	instr := prompts.Narrator(d.Persona.Core())
	sp := newParagraphStream(ctx, r, st, known, instr)
	res, err := d.Model.Stream(ctx, ports.GenerateRequest{
		Role: "narrator", Model: d.Cfg.Models.Narrator, FallbackModel: d.Cfg.Models.NarratorFallback, Instructions: instr,
		Input: []ports.InputItem{ports.Message(ports.RoleUser, b.String())}, MaxOutputTokens: 1800, ReasoningEffort: string(r.effort(ctx, st, "narrator").Effort),
	}, sp.feed)
	sp.flush()
	st.addUsage(res.Usage)
	u := res.Usage
	st.emit(ctx, "model.called", false, "narrate", map[string]any{"role": "narrator", "mode": "budget_to_destination", "error": errStr(err)}, &agent.EventMeta{Model: res.Model, Usage: &u, Latency: res.Latency})
	if len(st.sent) == 0 {
		var t strings.Builder
		t.WriteString("Perkiraan total (estimasi dari Indeks Biaya Destinasi):\n")
		for _, x := range dests {
			fmt.Fprintf(&t, "- %s: mulai %s (Hemat)\n", x.Label, verifier.Rupiah(x.TotalIDR["hemat"]))
		}
		t.WriteString("\nPilih salah satu dan saya susunkan paket dengan harga live.")
		return r.streamParagraphs(ctx, st, t.String(), known), nil
	}
	return joinSent(st), nil
}
