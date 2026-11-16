// Command eval runs the golden set (eval/golden/*.yaml) through the real
// pipeline and checks each case with code (AI_SYSTEM_DESIGN §12). Suppliers
// are the deterministic sandbox, so runs are repeatable; models are real.
//
//	eval [-filter id-substring] [-limit N] [-out eval/reports]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/koraai/koraai/internal/app"
	"github.com/koraai/koraai/internal/config"
	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/pipeline"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/store/postgres"
)

// Case is one golden case.
type Case struct {
	ID       string   `yaml:"id"`
	Category string   `yaml:"category"`
	Messages []string `yaml:"messages"`
	Expect   Expect   `yaml:"expect"`
}

// Expect lists the checks; empty fields are not checked.
type Expect struct {
	Intent         string   `yaml:"intent"`
	Status         string   `yaml:"status"`
	Origin         string   `yaml:"origin"`
	Destination    string   `yaml:"destination"`
	BudgetIDR      int64    `yaml:"budget_idr"`
	Season         string   `yaml:"season"`
	Month          int      `yaml:"month"`
	Year           int      `yaml:"year"`
	Nights         int      `yaml:"nights"`
	Adults         int      `yaml:"adults"`
	Children       int      `yaml:"children"`
	PackagesMin    int      `yaml:"packages_min"`
	PackagesMax    *int     `yaml:"packages_max"`
	Feasibility    []string `yaml:"feasibility_any"`
	Clarification  bool     `yaml:"clarification"`
	Destinations   bool     `yaml:"destinations"`
	TextContains   []string `yaml:"text_contains_any"`
	TextNotContain []string `yaml:"text_not_contains"`
	Special        string   `yaml:"special_kind"`
}

// Result is one case outcome.
type Result struct {
	ID         string   `json:"id"`
	Category   string   `json:"category"`
	Passed     bool     `json:"passed"`
	Failures   []string `json:"failures"`
	RunID      string   `json:"run_id"`
	Status     string   `json:"status"`
	Mode       string   `json:"mode"`
	Packages   int      `json:"packages"`
	Flags      []string `json:"flags"`
	CostMicros int64    `json:"cost_usd_micros"`
	DurationMs int64    `json:"duration_ms"`
}

func main() {
	filter := flag.String("filter", "", "only cases whose id contains this")
	limit := flag.Int("limit", 0, "max cases")
	out := flag.String("out", "eval/reports", "report directory")
	dir := flag.String("golden", "eval/golden", "golden set directory")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	log := platform.NewLogger("warn")
	ctx := context.Background()
	zero := time.Duration(0)
	if cfg.Tracer.ARL.Service == "kora/api" {
		cfg.Tracer.ARL.Service = "kora/eval" // golden-set runs get their own service in the Lab
	}
	a, err := app.Build(ctx, cfg, log, app.Options{SupplierLatency: &zero})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer a.Close()
	a.BuildPipeline(nil)

	cases, err := load(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var results []Result
	for _, c := range cases {
		if *filter != "" && !strings.Contains(c.ID, *filter) {
			continue
		}
		if *limit > 0 && len(results) >= *limit {
			break
		}
		r := runCase(ctx, a, c)
		mark := "PASS"
		if !r.Passed {
			mark = "FAIL"
		}
		fmt.Printf("%s %-32s %-22s mode=%-22s pkgs=%d cost=$%.4f %dms %s\n", mark, c.ID, c.Category, r.Mode, r.Packages, float64(r.CostMicros)/1e6, r.DurationMs, strings.Join(r.Failures, "; "))
		results = append(results, r)
	}
	report(*out, results)
}

func load(dir string) ([]Case, error) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	sort.Strings(files)
	var all []Case
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var cs []Case
		if err := yaml.Unmarshal(b, &cs); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		all = append(all, cs...)
	}
	return all, nil
}

func runCase(ctx context.Context, a *app.App, c Case) Result {
	res := Result{ID: c.ID, Category: c.Category, Failures: []string{}}
	start := time.Now()
	q := a.DB.Q()
	user, err := q.CreateUser(ctx, platform.NewID(), nil, true)
	if err != nil {
		res.Failures = append(res.Failures, "setup: "+err.Error())
		return res
	}
	defer func() { _, _ = a.DB.Pool.Exec(context.Background(), `delete from users where id = $1`, user.ID) }()
	conv, err := q.CreateConversation(ctx, platform.NewID(), user.ID, nil, domain.TripState{})
	if err != nil {
		res.Failures = append(res.Failures, "setup: "+err.Error())
		return res
	}
	var last postgres.Run
	for _, m := range c.Messages {
		msg, err := q.InsertMessage(ctx, platform.NewID(), conv.ID, "user", m, nil, nil)
		if err != nil {
			res.Failures = append(res.Failures, "setup: "+err.Error())
			return res
		}
		run, err := q.InsertRun(ctx, platform.NewID(), conv.ID, msg.ID, a.Cfg.Snapshot())
		if err != nil {
			res.Failures = append(res.Failures, "setup: "+err.Error())
			return res
		}
		a.Runner.RunSync(ctx, pipeline.RunInput{RunID: run.ID, ConversationID: conv.ID, UserID: user.ID, MessageID: msg.ID})
		if last, err = q.GetRun(ctx, run.ID); err != nil {
			res.Failures = append(res.Failures, "load run: "+err.Error())
			return res
		}
	}
	res.DurationMs = time.Since(start).Milliseconds()
	res.RunID, res.Status, res.Flags, res.CostMicros = last.ID.String(), last.Status, last.Flags, last.CostMicros
	if last.Mode != nil {
		res.Mode = *last.Mode
	}
	conv, _ = q.GetConversationByID(ctx, conv.ID)
	st := conv.TripState
	pkgs, _ := q.RunPackages(ctx, last.ID)
	res.Packages = len(pkgs)
	msgs, _ := q.LastMessages(ctx, conv.ID, 1)
	text, blocks := "", ""
	if len(msgs) == 1 {
		text, blocks = msgs[0].Content, string(msgs[0].Blocks)
	}
	e := c.Expect
	fail := func(format string, args ...any) { res.Failures = append(res.Failures, fmt.Sprintf(format, args...)) }
	wantStatus := e.Status
	if wantStatus == "" {
		wantStatus = "completed"
	}
	if last.Status != wantStatus {
		fail("status %s≠%s (%s)", last.Status, wantStatus, deref(last.ErrorCode))
	}
	if e.Intent != "" && res.Mode != e.Intent {
		fail("intent %s≠%s", res.Mode, e.Intent)
	}
	if e.Origin != "" && (st.Origin == nil || st.Origin.Code != e.Origin) {
		fail("origin %v≠%s", code(st.Origin), e.Origin)
	}
	if e.Destination != "" && (st.Destination == nil || st.Destination.Code != e.Destination) {
		fail("destination %v≠%s", code(st.Destination), e.Destination)
	}
	if e.BudgetIDR > 0 && (st.Budget == nil || st.Budget.Amount != e.BudgetIDR || st.Budget.Currency != "IDR") {
		fail("budget %v≠%d", st.Budget, e.BudgetIDR)
	}
	if e.Season != "" && (st.DateWindow == nil || st.DateWindow.Season != e.Season) {
		fail("season mismatch %+v", st.DateWindow)
	}
	if e.Month > 0 && (st.DateWindow == nil || st.DateWindow.Month != e.Month) {
		fail("month mismatch %+v", st.DateWindow)
	}
	if e.Year > 0 && (st.DateWindow == nil || st.DateWindow.Year != e.Year) {
		fail("year mismatch %+v", st.DateWindow)
	}
	if e.Nights > 0 && (st.Nights == nil || *st.Nights != e.Nights) {
		fail("nights %v≠%d", derefInt(st.Nights), e.Nights)
	}
	if e.Adults > 0 && st.Pax.Adults != e.Adults {
		fail("adults %d≠%d", st.Pax.Adults, e.Adults)
	}
	if e.Children > 0 && st.Pax.Children != e.Children {
		fail("children %d≠%d", st.Pax.Children, e.Children)
	}
	if e.Special != "" {
		found := false
		for _, s := range st.Special {
			if s.Kind == e.Special {
				found = true
			}
		}
		if !found {
			fail("special request %s not captured", e.Special)
		}
	}
	if len(pkgs) < e.PackagesMin {
		fail("packages %d<%d", len(pkgs), e.PackagesMin)
	}
	if e.PackagesMax != nil && len(pkgs) > *e.PackagesMax {
		fail("packages %d>%d", len(pkgs), *e.PackagesMax)
	}
	if len(e.Feasibility) > 0 {
		for _, p := range pkgs {
			ok := false
			for _, f := range e.Feasibility {
				if string(p.Feasibility) == f {
					ok = true
				}
			}
			if !ok {
				fail("feasibility %s not in %v", p.Feasibility, e.Feasibility)
			}
		}
	}
	for _, f := range last.Flags {
		if f == "invariant_violation" {
			fail("invariant violated")
		}
	}
	if e.Clarification && !strings.Contains(blocks, "clarification") {
		fail("expected a clarification")
	}
	if e.Destinations && !strings.Contains(blocks, "destinations") {
		fail("expected destination estimates")
	}
	if len(e.TextContains) > 0 {
		ok := false
		for _, s := range e.TextContains {
			if strings.Contains(strings.ToLower(text), strings.ToLower(s)) {
				ok = true
			}
		}
		if !ok {
			fail("text lacks any of %v", e.TextContains)
		}
	}
	for _, s := range e.TextNotContain {
		if strings.Contains(strings.ToLower(text), strings.ToLower(s)) {
			fail("text contains %q", s)
		}
	}
	res.Passed = len(res.Failures) == 0
	return res
}

func code(p *domain.Place) string {
	if p == nil {
		return "<nil>"
	}
	return p.Code
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func report(dir string, rs []Result) {
	_ = os.MkdirAll(dir, 0o755)
	pass := 0
	var cost, dur []int64
	for _, r := range rs {
		if r.Passed {
			pass++
		}
		cost = append(cost, r.CostMicros)
		dur = append(dur, r.DurationMs)
	}
	sort.Slice(dur, func(i, j int) bool { return dur[i] < dur[j] })
	p95 := int64(0)
	if len(dur) > 0 {
		p95 = dur[(len(dur)*95+99)/100-1]
	}
	var total int64
	for _, c := range cost {
		total += c
	}
	summary := map[string]any{
		"cases": len(rs), "passed": pass, "pass_rate": ratio(pass, len(rs)),
		"latency_p95_ms": p95, "cost_total_usd": float64(total) / 1e6, "cost_avg_usd": float64(total) / 1e6 / float64(max(1, len(rs))),
		"results": rs, "at": time.Now().UTC(),
	}
	b, _ := json.MarshalIndent(summary, "", "  ")
	name := filepath.Join(dir, "eval-"+time.Now().UTC().Format("20060102-150405")+".json")
	_ = os.WriteFile(name, b, 0o644)
	fmt.Printf("\n%d/%d passed (%.0f%%), p95 %dms, avg cost $%.4f → %s\n", pass, len(rs), ratio(pass, len(rs))*100, p95, summary["cost_avg_usd"], name)
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func derefInt(p *int) any {
	if p == nil {
		return "<nil>"
	}
	return *p
}
