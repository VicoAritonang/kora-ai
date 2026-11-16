package pipeline

import (
	"testing"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/domain"
)

func pkg(t domain.Tier, f domain.Feasibility, total, buffer, budget int64) domain.Package {
	b := budget
	return domain.Package{ID: uuid.New(), Tier: t, Feasibility: f, TotalIDR: total, LiveIDR: total / 2, EstimateIDR: total - total/2, BufferIDR: buffer, BudgetIDR: &b, Days: make([]domain.PackageDay, 6)}
}

func TestRecommend(t *testing.T) {
	ps := []domain.Package{pkg(domain.TierHemat, domain.FeasibilityFits, 10, 1, 20), pkg(domain.TierSeimbang, domain.FeasibilityFits, 15, 1, 20)}
	if r := recommend(ps, ""); r != "seimbang" {
		t.Fatal(r)
	}
	if r := recommend(ps, "hemat"); r != "hemat" {
		t.Fatal("planner choice ignored")
	}
	ps[1].Feasibility = domain.FeasibilityOverBudget
	if r := recommend(ps, "seimbang"); r != "hemat" {
		t.Fatal("recommended an over-budget package")
	}
}

func TestPackageCards_BarSumsWithinScale(t *testing.T) {
	c := packageCards([]domain.Package{pkg(domain.TierHemat, domain.FeasibilityFits, 12_000_000, 2_000_000, 20_000_000)}, "hemat", false)[0]
	sum := c.Bar.LivePct + c.Bar.EstimatePct + c.Bar.BufferPct + c.Bar.RemainingPct
	if sum < 98 || sum > 100 || !c.Recommended || c.Nights != 5 {
		t.Fatalf("%+v", c)
	}
}

func TestSuggestions_FromAlternatives(t *testing.T) {
	p := pkg(domain.TierHemat, domain.FeasibilityOverBudget, 30, 3, 20)
	p.Alternatives = []domain.Alternative{{Kind: "fewer_nights", Label: "Kurangi jadi 4 malam", Value: 4}, {Kind: "cheaper_area", Label: "Area Tennoji", Value: "Tennoji"}}
	n := 6
	s := suggestions([]domain.Package{p}, "hemat", domain.TripState{Nights: &n})
	if len(s) < 3 || s[0].Action["type"] != "set_slot" || s[1].Action["type"] != "prefer_area" {
		t.Fatalf("%+v", s)
	}
}

func TestRecommend_PrefersFitsOverTight(t *testing.T) {
	ps := []domain.Package{pkg(domain.TierHemat, domain.FeasibilityFits, 14, 2, 20), pkg(domain.TierSeimbang, domain.FeasibilityTight, 19, 2, 20)}
	if r := recommend(ps, ""); r != "hemat" {
		t.Fatalf("recommended a tight package over one that fits: %s", r)
	}
}
