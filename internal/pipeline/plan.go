package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/koraai/koraai/internal/agent"
	"github.com/koraai/koraai/internal/agent/prompts"
	"github.com/koraai/koraai/internal/costnorm"
	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/packagebuilder"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/session"
	"github.com/koraai/koraai/internal/tools"
	"github.com/koraai/koraai/internal/verifier"
)

// retrieve runs the pipeline retrieval: five persona queries in parallel,
// the cost norms and FX for the trip, and the user's memory (§4.2).
func (r *Runner) retrieve(ctx context.Context, st *runState) error {
	d := &r.d
	st.status(ctx, "retrieving_knowledge")
	if d.Persona == nil {
		return fail("kg_unavailable", false, errors.New("persona pack not loaded"))
	}
	req := st.req
	dest := req.Destination.Label
	period := windowLabel(req.Window)
	queries := []ports.KnowledgeQuery{
		{Purpose: "destination_period", Text: fmt.Sprintf("%s %s", dest, period)},
		{Purpose: "stay_area", Text: fmt.Sprintf("area menginap %s dekat stasiun hemat nyaman", dest)},
		{Purpose: "duration_budget", Text: fmt.Sprintf("berapa malam %s budget durasi ideal", dest)},
		{Purpose: "documents", Text: "visa Jepang paspor Indonesia e-paspor"},
	}
	var special []string
	for _, s := range req.Special {
		special = append(special, s.Text, synonyms[s.Kind])
	}
	for _, i := range req.Interests {
		special = append(special, i, synonyms[i])
	}
	if len(special) > 0 {
		// Retrieval is lexical: variant terms recover units a single word
		// misses (AI_SYSTEM_DESIGN §4.4).
		queries = append(queries, ports.KnowledgeQuery{Purpose: "special_requests", Units: 6, Text: strings.Join(special, " ") + " " + dest + " " + period})
	}
	start := time.Now()
	pack, err := d.Persona.Retrieve(ctx, queries, d.Cfg.Persona.BudgetTokens)
	if err != nil {
		return fail("kg_unavailable", true, err)
	}
	st.pack = pack
	if d.Cfg.Memory.Enabled && st.user.MemoryEnabled && !st.user.IsGuest && d.Memory != nil {
		if b, err := d.Memory.Recall(ctx, st.in.UserID, st.text, d.Cfg.Memory.BudgetTokens); err == nil {
			st.memory = b.Text
		} else {
			st.flag("memory_unavailable")
		}
	}
	var refs []string
	for _, u := range pack.Units {
		refs = append(refs, u.Ref)
	}
	st.emit(ctx, "retrieval.done", false, "retrieve", map[string]any{"refs": refs, "gaps": pack.Gaps, "tokens": pack.Tokens,
		"pack_version": d.Persona.Version(), "memory_tokens": len(st.memory) / 4}, &agent.EventMeta{Latency: time.Since(start)})
	return nil
}

// synonyms expands interest tags and request kinds into the Indonesian and
// Japanese terms the persona pack is written in.
var synonyms = map[string]string{
	"festival":       "acara matsuri festival perayaan parade",
	"autumn_foliage": "momiji dedaunan musim gugur koyo",
	"food":           "kuliner makanan restoran jajanan",
	"halal":          "makanan halal restoran halal muslim",
	"children":       "anak keluarga stroller",
	"family":         "keluarga anak",
	"temple":         "kuil shrine jinja tera",
	"shopping":       "belanja",
	"theme_park":     "taman hiburan Universal Studios",
	"nature":         "alam taman gunung",
	"onsen":          "onsen pemandian air panas",
	"history":        "sejarah kastil",
	"accessibility":  "lift kursi roda aksesibilitas",
	"area":           "area menginap",
}

func knowledgeBlock(version string, units []ports.KnowledgeUnit, gaps []string) string {
	var b strings.Builder
	b.WriteString(`<knowledge pack="` + version + `">` + "\n")
	for _, u := range units {
		fmt.Fprintf(&b, "[%s] (%s) %s\n%s\n\n", u.Ref, u.Kind, u.Title, u.Body)
	}
	b.WriteString("</knowledge>")
	if len(gaps) > 0 {
		b.WriteString("\n<knowledge_gaps>" + strings.Join(gaps, ", ") + "</knowledge_gaps>")
	}
	return b.String()
}

// plan runs the agent loop (step 6).
//
// retry is set on the second attempt after a verified failure (no packages):
// the candidates found so far are kept and effort is escalated once.
func (r *Runner) plan(ctx context.Context, st *runState, retry *agent.Decision) error {
	d := &r.d
	st.status(ctx, "planning")
	if retry != nil && st.rc != nil {
		return r.loop(ctx, st, st.rc, *retry, "\nNOTE: the previous attempt built no packages ("+strings.Join(st.flags, ", ")+"). Check that stay dates equal the flight arrival and return dates, search again if offers expired, then call build_packages.")
	}
	rc := agent.NewRunContext(st.in.RunID, st.in.ConversationID, st.in.UserID, st.req, d.Clock.Now)
	rc.Emitter = st
	rc.GuardSession = st.guardSession
	rc.Knowledge = st.pack.Units
	st.rc = rc

	norms, valid, err := d.Norms.ForTrip(ctx, st.req.Destination.Code, st.req.DepartDate)
	if err != nil && !errors.Is(err, costnorm.ErrNoNorms) {
		return fail("internal_error", true, err)
	}
	if errors.Is(err, costnorm.ErrNoNorms) {
		st.flag("no_cost_norms")
	}
	fx, err := d.FX.Table(ctx, append(costnorm.Currencies(valid), "JPY", "USD")...)
	if err != nil {
		return fail("internal_error", true, fmt.Errorf("fx: %w", err))
	}
	rc.Norms, rc.NormList, rc.FX = norms, valid, fx

	return r.loop(ctx, st, rc, r.effort(ctx, st, "planner"), "")
}

// loop runs the planner loop once with the given effort decision.
func (r *Runner) loop(ctx context.Context, st *runState, rc *agent.RunContext, dec agent.Decision, note string) error {
	d := &r.d
	cfg := d.Cfg
	pack := knowledgeBlock(d.Persona.Version(), st.pack.Units, st.pack.Gaps)
	if st.memory != "" {
		pack += "\n<memory>\n" + st.memory + "\n</memory>"
	}
	window, summary := r.window(ctx, st)
	limits := agent.Limits{
		MaxSteps: cfg.Limits.MaxSteps, FlightSearches: cfg.Limits.FlightSearches, StaySearches: cfg.Limits.StaySearches,
		ActivitySearch: cfg.Limits.ActivitySearch, WebResearch: cfg.Limits.WebResearch, CostMicros: cfg.Limits.CostUSDMicros,
		Deadline: st.started.Add(cfg.Limits.RunTimeout * 2 / 3), ToolTimeout: cfg.Limits.ToolTimeout, WebTimeout: cfg.Limits.WebTimeout,
		MaxOutput: cfg.Limits.MaxOutputTokens,
	}
	li := agent.LoopInput{
		RC: rc, Model: d.Model, Guard: d.Guard, Registry: d.Registry, Limits: limits,
		Builder: session.Builder{B: session.DefaultBudgets},
		Context: session.ContextInput{
			Instructions: prompts.Planner(d.Persona.Core()), TripState: session.CompactState(st.state), ContextPack: pack,
			Summary: summary, Window: window, Task: plannerTask(st, cfg.Policy.BookingWindowDays) + note,
		},
		ModelName: cfg.Models.Planner, Fallback: cfg.Models.PlannerFallback,
		Policy: r.policy, Decision: dec,
		CacheKey: "kora-planner-" + prompts.PlannerVersion, PromptVer: prompts.PlannerVersion,
	}
	if note == "" {
		li.Prefill = r.prefetch(ctx, st, li)
	}
	res, err := agent.Loop{}.Run(ctx, li)
	st.mu.Lock()
	st.steps = res.Steps
	st.mu.Unlock()
	rc.Lock()
	st.guardSession = rc.GuardSession
	rc.Unlock()
	st.plannerNote = res.FinalText
	if res.StopReason != agent.StopDone && res.StopReason != "" {
		st.flag(string(res.StopReason))
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Build from what exists when the planner fails mid-way (§3 step 6).
		st.flag("planner_failed")
		if !rc.HasFlights() && !rc.HasStays() {
			return fail("model_unavailable", true, err)
		}
	}
	return nil
}

// plannerTask states the resolved request; dates and codes come from code.
func plannerTask(st *runState, window int) string {
	req := st.req
	var b strings.Builder
	b.WriteString("Plan this trip (resolved by code; use these codes and dates):\n")
	fmt.Fprintf(&b, "- origin: %s (%s)\n- destination: %s (%s)\n", req.Origin.Code, req.Origin.Label, req.Destination.Code, req.Destination.Label)
	fmt.Fprintf(&b, "- date window: %s .. %s (%s)\n", req.WindowStart, req.WindowEnd, windowLabel(req.Window))
	fmt.Fprintf(&b, "- suggested departure: %s; nights at destination: %d; search return_date = %s (Indonesia→Japan flights usually land the next day). Use each offer's arrival_date as check_in.\n",
		req.DepartDate, req.Nights, req.ReturnDate)
	fmt.Fprintf(&b, "- party: %d adult(s), %d child(ren)\n", req.Pax.Adults, req.Pax.Children)
	if req.BudgetIDR != nil {
		fmt.Fprintf(&b, "- budget: %s total (IDR)\n", verifier.Rupiah(*req.BudgetIDR))
	} else {
		b.WriteString("- budget: not given (show what each tier costs)\n")
	}
	if len(req.Interests) > 0 {
		b.WriteString("- interests: " + strings.Join(req.Interests, ", ") + "\n")
	}
	for _, s := range req.Special {
		fmt.Fprintf(&b, "- SPECIAL REQUEST (%s): %s\n", s.Kind, s.Text)
	}
	fmt.Fprintf(&b, "- suppliers sell flights up to %s (about %d days ahead).", req.LiveWindowEnd, window)
	if req.OutOfBookingWindow() {
		b.WriteString(" These dates are beyond that: search_flights will return an estimate; still search stays and plan days.")
	}
	b.WriteString("\n- traveller's latest message: " + trimRunes(st.text, 600))
	return b.String()
}

func (r *Runner) window(ctx context.Context, st *runState) ([]session.Turn, string) {
	msgs, err := r.d.DB.Q().LastMessages(ctx, st.in.ConversationID, session.DefaultBudgets.WindowTurns*2+1)
	if err != nil {
		return nil, ""
	}
	var turns []session.Turn
	for _, m := range msgs {
		if m.ID == st.userMsg.ID || m.Role == "notice" {
			continue
		}
		turns = append(turns, session.Turn{Role: m.Role, Content: trimRunes(m.Content, 1500)})
	}
	summary := ""
	if st.conv.Summary != nil {
		summary = *st.conv.Summary
	}
	return turns, summary
}

// buildAndVerify runs the package builder on the planner's selection, checks
// every invariant, and stores the packages that pass (steps 7–8).
func (r *Runner) buildAndVerify(ctx context.Context, st *runState) error {
	d := &r.d
	rc := st.rc
	st.status(ctx, "building_packages")
	policy := builderPolicy(d)
	now := d.Clock.Now()

	// Without live candidates, fall back to the cost index (NFR-12, FR-14).
	if !rc.HasFlights() && d.Index != nil {
		if e, err := d.Index.FlightEstimate(ctx, st.req.Origin.Code, st.req.Destination.Code, st.req.DepartDate.FirstOfMonth()); err == nil {
			rc.Lock()
			rc.FlightEstimate = &e
			rc.Unlock()
		}
	}
	if !rc.HasStays() && d.Index != nil {
		for _, t := range domain.Tiers {
			if e, err := d.Index.StayEstimate(ctx, st.req.Destination.Code, st.req.DepartDate.FirstOfMonth(), t); err == nil {
				rc.Lock()
				rc.StayEstimates[t] = e
				rc.Unlock()
			}
		}
	}
	rc.Lock()
	sel := rc.Selection
	rc.Unlock()
	in := tools.BuilderInput(rc, sel, policy, now)
	if st.req.BudgetIDR != nil && d.Index != nil {
		if dests, err := d.Index.EstimateTrip(ctx, st.req.Origin.Code, st.req.DepartDate, st.req.Nights, st.req.Pax, nil, policy.ContingencyPct); err == nil {
			for _, x := range dests {
				in.Destinations = append(in.Destinations, packagebuilder.DestinationOption{Code: x.Code, Label: x.Label, TotalIDR: x.TotalIDR["hemat"]})
			}
		}
	}
	out, err := packagebuilder.Build(in)
	if errors.Is(err, packagebuilder.ErrNoCandidates) && sel != nil {
		in = tools.BuilderInput(rc, nil, policy, now) // the selection did not line up; use every candidate
		in.Destinations = nil
		out, err = packagebuilder.Build(in)
	}
	st.emit(ctx, "packages.built", false, "build", map[string]any{"packages": summarize(out.Packages), "rejected": out.Rejected, "gaps": out.Gaps, "error": errStr(err)}, nil)
	if err != nil {
		st.flag("no_packages")
		rc.AddNotice("no_packages", "Saya belum menemukan kombinasi penerbangan dan penginapan yang tanggalnya cocok. Coba geser tanggal atau ubah durasi.")
		return nil
	}
	for _, g := range out.Gaps {
		st.flag("norm_gap:" + g)
	}

	st.status(ctx, "verifying")
	var passed []domain.Package
	var report []map[string]any
	for _, p := range out.Packages {
		v := verifier.CheckInvariants(p, st.req, d.Clock.Now(), verifier.Options{FXMaxAgeDays: d.Cfg.Policy.FXMaxAgeDays})
		report = append(report, map[string]any{"package_id": p.ID, "tier": p.Tier, "violations": v})
		if len(v) > 0 {
			d.Log.Error("package invariant violated", "tier", p.Tier, "violations", v)
			st.flag("invariant_violation")
			continue
		}
		passed = append(passed, p)
	}
	st.emit(ctx, "verify.done", false, "verify", map[string]any{"invariants": report}, nil)
	if len(passed) == 0 {
		rc.AddNotice("no_packages", "Paket yang tersusun gagal pemeriksaan otomatis, jadi tidak saya tampilkan. Silakan coba lagi.")
		return nil
	}
	// Persist: trip request + packages, older packages become stale.
	err = d.DB.WithTx(ctx, func(q *postgresQ) error {
		if err := q.InsertTripRequest(ctx, st.in.ConversationID, st.in.RunID, st.req); err != nil {
			return err
		}
		for _, p := range passed {
			if err := q.InsertPackage(ctx, st.in.ConversationID, st.in.RunID, st.req.ID, p); err != nil {
				return err
			}
		}
		return q.MarkConversationPackagesStale(ctx, st.in.ConversationID, st.in.RunID)
	})
	if err != nil {
		return fail("internal_error", true, fmt.Errorf("save packages: %w", err))
	}
	st.packages = passed
	var cards []map[string]any
	for _, p := range passed {
		cards = append(cards, map[string]any{"id": p.ID, "tier": p.Tier, "title": p.Title, "total": domain.IDR(p.TotalIDR), "feasibility": p.Feasibility})
	}
	st.emit(ctx, "packages.ready", true, "build", map[string]any{"packages": cards}, nil)
	for _, n := range notices(st) {
		st.emit(ctx, "notice", true, "build", map[string]any{"code": "info", "text": n}, nil)
	}
	return nil
}

func builderPolicy(d *Deps) packagebuilder.Policy {
	p := packagebuilder.DefaultPolicy()
	p.ContingencyPct = d.Cfg.Policy.ContingencyPct
	p.FXBufferPct = d.Cfg.Policy.FXBufferPct
	p.MinStayRating = d.Cfg.Policy.MinStayRating
	p.Version = d.Cfg.Policy.BuilderVersion
	return p
}

func summarize(pkgs []domain.Package) []map[string]any {
	var out []map[string]any
	for _, p := range pkgs {
		out = append(out, map[string]any{"id": p.ID, "tier": p.Tier, "total_idr": p.TotalIDR, "buffer_idr": p.BufferIDR, "feasibility": p.Feasibility, "items": len(p.Items)})
	}
	return out
}
