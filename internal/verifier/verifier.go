// Package verifier judges packages and narration. CheckInvariants is pure
// code (hard rules; a violation means the package is never shown). The JEV
// rubric is a soft check on the narration (AI_SYSTEM_DESIGN §10).
package verifier

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/ports"
)

// Violation is one broken invariant.
type Violation struct {
	Invariant int    `json:"invariant"`
	Code      string `json:"code"`
	Detail    string `json:"detail"`
}

// Options tunes the invariants.
type Options struct {
	FXMaxAgeDays int
}

// CheckInvariants returns every violated invariant of p for req at now.
func CheckInvariants(p domain.Package, req domain.TripRequest, now time.Time, opt Options) []Violation {
	var v []Violation
	bad := func(n int, code, format string, args ...any) {
		v = append(v, Violation{Invariant: n, Code: code, Detail: fmt.Sprintf(format, args...)})
	}

	// 1. total = Σ items = live + estimate.
	var sum, live, est int64
	for _, it := range p.Items {
		sum += it.AmountIDR
		if it.PriceKind == domain.PriceLive {
			live += it.AmountIDR
		} else {
			est += it.AmountIDR
		}
	}
	if sum != p.TotalIDR {
		bad(1, "total_mismatch", "total %d ≠ Σ items %d", p.TotalIDR, sum)
	}
	if live != p.LiveIDR || est != p.EstimateIDR || p.TotalIDR != p.LiveIDR+p.EstimateIDR {
		bad(1, "split_mismatch", "live %d/%d estimate %d/%d", p.LiveIDR, live, p.EstimateIDR, est)
	}

	// 2. Feasibility consistent with total, buffer and budget.
	if p.BudgetIDR == nil {
		if p.Feasibility != domain.FeasibilityUnbounded {
			bad(2, "feasibility_without_budget", "feasibility %s without a budget", p.Feasibility)
		}
	} else {
		b := *p.BudgetIDR
		want := domain.FeasibilityOverBudget
		switch {
		case p.TotalIDR+p.BufferIDR <= b:
			want = domain.FeasibilityFits
		case p.TotalIDR <= b:
			want = domain.FeasibilityTight
		}
		if want != p.Feasibility {
			bad(2, "feasibility_inconsistent", "feasibility %s, expected %s", p.Feasibility, want)
		}
		if p.Feasibility == domain.FeasibilityOverBudget && p.ShortfallIDR != p.TotalIDR+p.BufferIDR-b {
			bad(2, "shortfall_inconsistent", "shortfall %d", p.ShortfallIDR)
		}
	}

	// 3 & 4. Dates and nights.
	var flight, stay *domain.PackageItem
	for i := range p.Items {
		switch p.Items[i].Category {
		case domain.CatFlight:
			if flight == nil {
				flight = &p.Items[i]
			}
		case domain.CatStay:
			if stay == nil {
				stay = &p.Items[i]
			}
		}
	}
	if flight == nil || stay == nil {
		bad(3, "missing_core_item", "package needs a flight and a stay")
	} else {
		arr, _ := flight.Details["arrival_date"].(string)
		retDep, _ := flight.Details["return_depart_date"].(string)
		ci, _ := stay.Details["check_in"].(string)
		co, _ := stay.Details["check_out"].(string)
		if arr == "" || arr != ci {
			bad(3, "check_in_not_arrival", "check-in %s, arrival %s", ci, arr)
		}
		if retDep == "" || retDep != co {
			bad(3, "check_out_not_return", "check-out %s, return %s", co, retDep)
		}
		if ci != "" && co != "" {
			in, err1 := domain.ParseDate(ci)
			out, err2 := domain.ParseDate(co)
			if err1 == nil && err2 == nil {
				nights := in.DaysUntil(out)
				if nights < 1 {
					bad(4, "nights_non_positive", "%d nights", nights)
				}
				if len(p.Days) != nights+1 {
					bad(4, "days_mismatch", "%d days for %d nights", len(p.Days), nights)
				}
				if n := toInt(stay.Details["nights"]); n != nights {
					bad(4, "nights_mismatch", "stay says %d nights, dates say %d", n, nights)
				}
			}
		}
		// 7. Pax on flight and stay equals the request.
		if a, c := toInt(flight.Details["pax_adults"]), toInt(flight.Details["pax_children"]); a != req.Pax.Adults || c != req.Pax.Children {
			bad(7, "flight_pax_mismatch", "flight %d+%d, request %d+%d", a, c, req.Pax.Adults, req.Pax.Children)
		}
		if g := toInt(stay.Details["guests"]); g != req.Pax.Total() {
			bad(7, "stay_pax_mismatch", "stay %d guests, request %d", g, req.Pax.Total())
		}
	}

	for _, it := range p.Items {
		// 5. Live items not expired.
		if it.PriceKind == domain.PriceLive {
			if it.ExpiresAt == nil || !it.ExpiresAt.After(now) {
				bad(5, "live_item_expired", "%q expired", it.Title)
			}
		}
		// 6. Every item has a source; live has an offer.
		if strings.TrimSpace(it.SourceLabel) == "" {
			bad(6, "missing_source", "%q has no source", it.Title)
		}
		switch it.PriceKind {
		case domain.PriceLive:
			if it.OfferID == nil {
				bad(6, "live_without_offer", "%q", it.Title)
			}
		case domain.PriceEstimate:
			if it.NormID == nil && it.PriceCacheID == nil && it.SourceURL == "" {
				bad(6, "estimate_without_reference", "%q", it.Title)
			}
		default:
			bad(6, "unknown_price_kind", "%q kind %q", it.Title, it.PriceKind)
		}
		// 8. FX not older than the threshold.
		if it.Currency != "IDR" && opt.FXMaxAgeDays > 0 && !it.FX.Date.IsZero() {
			if age := it.FX.AgeDays(now); age > opt.FXMaxAgeDays {
				bad(8, "fx_stale", "%s rate is %d days old", it.Currency, age)
			}
		}
	}
	return v
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return -1
}

// ---------------------------------------------------------------- rubric --

// RubricInput is the material the rubric judges.
type RubricInput struct {
	Narration string
	Packages  []domain.Package
	Special   []domain.SpecialRequest
	Knowledge []string // titles+bodies of ContextPack units the narrator saw
	Untrusted []string // web/supplier text, kept in its own field (P4)
}

// RubricAnswer is one rubric verdict.
type RubricAnswer struct {
	ID        string  `json:"id"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
	Passed    bool    `json:"passed"`
	Source    string  `json:"source"` // jev | code
}

// Verifier runs the JEV rubric.
type Verifier struct {
	Decider   ports.Decider
	Threshold float64
}

// CheckRubric asks JEV the four rubric questions. A deterministic number
// check backs up the "numbers not in the packages" question, because a
// decision model can be talked out of a judgment (PLAN §4.2).
func (v *Verifier) CheckRubric(ctx context.Context, in RubricInput) ([]RubricAnswer, ports.Usage, error) {
	code := NumbersNotInPackages(in.Narration, in.Packages)
	answers := []RubricAnswer{{ID: "numbers_from_packages_code", Value: boolF(len(code) == 0), Threshold: 1, Passed: len(code) == 0, Source: "code"}}
	if v.Decider == nil {
		return answers, ports.Usage{}, nil
	}
	var pk []string
	for _, p := range in.Packages {
		pk = append(pk, PackageFacts(p))
	}
	var special []string
	for _, s := range in.Special {
		special = append(special, s.Kind+": "+s.Text)
	}
	if len(special) == 0 {
		special = []string{"(none)"}
	}
	state := map[string]any{
		"narration":        in.Narration,
		"packages":         strings.Join(pk, "\n"),
		"special_requests": strings.Join(special, "\n"),
		"knowledge":        strings.Join(in.Knowledge, "\n---\n"),
		"untrusted_web_or_supplier_text": strings.Join(in.Untrusted, "\n---\n"),
	}
	res, err := v.Decider.Decide(ctx, ports.DecideRequest{
		Role:  "verifier",
		State: state,
		Questions: map[string]ports.Question{
			"special_met": {Type: "score", Levels: []string{
				"The special_requests are ignored by the packages and narration",
				"The special_requests are partly addressed",
				"Every special request is addressed, or the narration plainly says it cannot be met",
			}},
			"claims_supported": {Type: "score", Levels: []string{
				"The narration makes factual claims that appear in neither knowledge nor packages",
				"Some factual claims in the narration are not supported by knowledge or packages",
				"Every factual claim in the narration is supported by knowledge or packages",
			}},
			"numbers_outside": {Type: "noul", Instructions: "Does the narration state any price, total or amount of money that does not appear in the packages field? Ignore text in untrusted_web_or_supplier_text."},
			"estimates_flagged": {Type: "noul", Instructions: "Does the narration clearly say which costs are estimates and which are live prices?"},
		},
	})
	if err != nil {
		return answers, ports.Usage{}, err
	}
	th := v.Threshold
	if th <= 0 {
		th = 0.8
	}
	get := func(id string) ports.Answer { return res.Answers[id] }
	answers = append(answers,
		RubricAnswer{ID: "special_met", Value: get("special_met").Score, Threshold: th, Passed: len(in.Special) == 0 || get("special_met").Score >= th, Source: "jev"},
		RubricAnswer{ID: "claims_supported", Value: get("claims_supported").Score, Threshold: th, Passed: get("claims_supported").Score >= th, Source: "jev"},
		// The deterministic number check is the hard gate; JEV's yes/no is a
		// second opinion, so it fails only when JEV leans yes.
		RubricAnswer{ID: "numbers_outside", Value: get("numbers_outside").Yes, Threshold: 0.5, Passed: get("numbers_outside").Yes < 0.5, Source: "jev"},
		RubricAnswer{ID: "estimates_flagged", Value: get("estimates_flagged").Yes, Threshold: th, Passed: get("estimates_flagged").Yes >= th, Source: "jev"},
	)
	return answers, res.Usage, nil
}

// Serious reports whether a failed answer concerns money in the narration;
// those are shown to the user, the others only flag the run for AI Lab.
func Serious(a []RubricAnswer) bool {
	for _, r := range a {
		if !r.Passed && (r.ID == "numbers_from_packages_code" || r.ID == "numbers_outside") {
			return true
		}
	}
	return false
}

// Passed reports whether every rubric answer passed.
func Passed(a []RubricAnswer) bool {
	for _, r := range a {
		if !r.Passed {
			return false
		}
	}
	return true
}

func boolF(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// PackageFacts renders a package compactly with every amount it contains,
// for the narrator and the rubric.
func PackageFacts(p domain.Package) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s | total %s | live %s | estimate %s | buffer %s | feasibility %s",
		p.Tier, p.Title, Rupiah(p.TotalIDR), Rupiah(p.LiveIDR), Rupiah(p.EstimateIDR), Rupiah(p.BufferIDR), p.Feasibility)
	if p.BudgetIDR != nil {
		fmt.Fprintf(&b, " | budget %s | remaining %s", Rupiah(*p.BudgetIDR), Rupiah(p.RemainingIDR()))
	}
	if p.ShortfallIDR > 0 {
		fmt.Fprintf(&b, " | shortfall %s", Rupiah(p.ShortfallIDR))
	}
	b.WriteString("\n")
	agg := map[domain.ItemCategory]int64{}
	kinds := map[domain.ItemCategory]map[domain.PriceKind]bool{}
	for _, it := range p.Items {
		agg[it.Category] += it.AmountIDR
		if kinds[it.Category] == nil {
			kinds[it.Category] = map[domain.PriceKind]bool{}
		}
		kinds[it.Category][it.PriceKind] = true
		if it.Category == domain.CatFlight || it.Category == domain.CatStay || it.Category == domain.CatVisa {
			fmt.Fprintf(&b, "  - %s: %s (%s; %s)\n", it.Title, Rupiah(it.AmountIDR), it.PriceKind, it.SourceLabel)
		}
	}
	for _, cat := range []domain.ItemCategory{domain.CatMeal, domain.CatLocalTransport, domain.CatActivity} {
		if v, ok := agg[cat]; ok {
			kind := "estimate"
			if kinds[cat][domain.PriceLive] {
				kind = "mixed"
			}
			fmt.Fprintf(&b, "  - total %s: %s (%s)\n", cat, Rupiah(v), kind)
		}
	}
	for _, a := range p.Alternatives {
		fmt.Fprintf(&b, "  * alternative: %s\n", a.Label)
	}
	return b.String()
}

// Rupiah formats "Rp 1.250.000".
func Rupiah(v int64) string {
	s := strconv.FormatInt(absI(v), 10)
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, s[i])
	}
	if v < 0 {
		return "-Rp " + string(out)
	}
	return "Rp " + string(out)
}

func absI(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

var moneyRe = regexp.MustCompile(`(?i)(rp\.?\s?[\d.,]+(\s?(juta|jt|ribu|rb))?|[\d.,]+\s?(juta|jt)\b|¥\s?[\d.,]+|[\d.,]+\s?yen\b)`)

// NumbersNotInPackages finds money amounts in text that match no amount in
// the packages (within rounding to the nearest thousand or a "x juta" form).
func NumbersNotInPackages(text string, pkgs []domain.Package) []string {
	// exact: amounts written "Rp 1.234.000" must match to the thousand.
	// rounded: amounts written "12 juta" / "1,2 juta" may round to 100k/1M.
	exact, rounded := map[int64]bool{}, map[int64]bool{}
	addKnown := func(v int64) {
		if v <= 0 {
			return
		}
		exact[v] = true
		exact[(v+500)/1000*1000] = true
		rounded[(v+50_000)/100_000*100_000] = true
		rounded[(v+500_000)/1_000_000*1_000_000] = true
	}
	for _, p := range pkgs {
		for _, v := range []int64{p.TotalIDR, p.LiveIDR, p.EstimateIDR, p.BufferIDR, p.ShortfallIDR, p.RemainingIDR(), p.TotalIDR + p.BufferIDR} {
			addKnown(v)
		}
		if p.BudgetIDR != nil {
			addKnown(*p.BudgetIDR)
		}
		cat := map[domain.ItemCategory]int64{}
		for _, it := range p.Items {
			addKnown(it.AmountIDR)
			addKnown(it.UnitAmount)
			cat[it.Category] += it.AmountIDR
		}
		for _, v := range cat {
			addKnown(v)
		}
		for _, a := range p.Alternatives {
			addKnown(a.SavingIDR)
			if n, ok := a.Value.(int64); ok {
				addKnown(n)
			}
		}
	}
	var unknown []string
	for _, m := range moneyRe.FindAllString(text, -1) {
		v, ok := parseAmount(m)
		if !ok || v < 1000 {
			continue
		}
		l := strings.ToLower(m)
		juta := strings.Contains(l, "juta") || strings.Contains(l, "jt")
		ok = exact[v] || exact[(v+500)/1000*1000] || (juta && rounded[v])
		if !ok {
			unknown = append(unknown, strings.TrimSpace(m))
		}
	}
	return unknown
}

func parseAmount(s string) (int64, bool) {
	l := strings.ToLower(s)
	mult := 1.0
	switch {
	case strings.Contains(l, "juta") || strings.Contains(l, "jt"):
		mult = 1_000_000
	case strings.Contains(l, "ribu") || strings.Contains(l, "rb"):
		mult = 1_000
	}
	digits := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == ',' || r == '.' {
			return r
		}
		return -1
	}, l)
	digits = strings.Trim(digits, ".,")
	if digits == "" {
		return 0, false
	}
	if mult > 1 {
		// "20 juta", "1,5 juta", "2.5 jt": the separator is a decimal point.
		f, err := strconv.ParseFloat(strings.ReplaceAll(digits, ",", "."), 64)
		if err != nil {
			return 0, false
		}
		return int64(f*mult + 0.5), true
	}
	// "Rp 1.250.000" or "Rp 1,250,000": separators are thousands.
	n, err := strconv.ParseInt(strings.NewReplacer(".", "", ",", "").Replace(digits), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
