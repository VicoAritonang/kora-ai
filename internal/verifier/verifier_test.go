package verifier

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/domain"
)

var now = time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)

func valid() (domain.Package, domain.TripRequest) {
	budget := int64(20_000_000)
	exp := now.Add(30 * time.Minute)
	oid := uuid.New()
	sid := uuid.New()
	nid := uuid.New()
	d0 := 0
	req := domain.TripRequest{Pax: domain.Pax{Adults: 1}, BudgetIDR: &budget}
	p := domain.Package{
		Tier: domain.TierSeimbang, BudgetIDR: &budget, Feasibility: domain.FeasibilityFits,
		TotalIDR: 14_000_000, LiveIDR: 13_000_000, EstimateIDR: 1_000_000, BufferIDR: 2_000_000,
		Days: make([]domain.PackageDay, 7),
		Items: []domain.PackageItem{
			{Category: domain.CatFlight, AmountIDR: 7_000_000, PriceKind: domain.PriceLive, OfferID: &oid, ExpiresAt: &exp, SourceLabel: "Duffel", Currency: "IDR",
				Details: map[string]any{"arrival_date": "2027-03-11", "return_depart_date": "2027-03-17", "pax_adults": 1, "pax_children": 0}},
			{Category: domain.CatStay, AmountIDR: 6_000_000, PriceKind: domain.PriceLive, OfferID: &sid, ExpiresAt: &exp, SourceLabel: "LiteAPI", Currency: "JPY",
				FX: domain.FXQuote{Date: domain.DateOf(now)},
				Details: map[string]any{"check_in": "2027-03-11", "check_out": "2027-03-17", "nights": 6, "guests": 1}},
			{Category: domain.CatMeal, DayIndex: &d0, AmountIDR: 1_000_000, PriceKind: domain.PriceEstimate, NormID: &nid, SourceLabel: "norm", Currency: "JPY",
				FX: domain.FXQuote{Date: domain.DateOf(now)}},
		},
	}
	return p, req
}

func codes(v []Violation) map[string]bool {
	m := map[string]bool{}
	for _, x := range v {
		m[x.Code] = true
	}
	return m
}

func TestCheckInvariants_ValidPackage_NoViolations(t *testing.T) {
	p, req := valid()
	if v := CheckInvariants(p, req, now, Options{FXMaxAgeDays: 4}); len(v) != 0 {
		t.Fatalf("%+v", v)
	}
}

func TestCheckInvariants_EachViolation(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*domain.Package, *domain.TripRequest)
		code string
	}{
		{"1 total", func(p *domain.Package, _ *domain.TripRequest) { p.TotalIDR++ }, "total_mismatch"},
		{"2 feasibility", func(p *domain.Package, _ *domain.TripRequest) { p.Feasibility = domain.FeasibilityTight }, "feasibility_inconsistent"},
		{"3 check-in", func(p *domain.Package, _ *domain.TripRequest) { p.Items[1].Details["check_in"] = "2027-03-10" }, "check_in_not_arrival"},
		{"3 check-out", func(p *domain.Package, _ *domain.TripRequest) { p.Items[1].Details["check_out"] = "2027-03-18" }, "check_out_not_return"},
		{"4 days", func(p *domain.Package, _ *domain.TripRequest) { p.Days = p.Days[:5] }, "days_mismatch"},
		{"5 expired", func(p *domain.Package, _ *domain.TripRequest) { e := now.Add(-time.Minute); p.Items[0].ExpiresAt = &e }, "live_item_expired"},
		{"6 no source", func(p *domain.Package, _ *domain.TripRequest) { p.Items[2].SourceLabel = "" }, "missing_source"},
		{"6 live w/o offer", func(p *domain.Package, _ *domain.TripRequest) { p.Items[0].OfferID = nil }, "live_without_offer"},
		{"6 estimate w/o ref", func(p *domain.Package, _ *domain.TripRequest) { p.Items[2].NormID = nil }, "estimate_without_reference"},
		{"7 pax", func(_ *domain.Package, r *domain.TripRequest) { r.Pax = domain.Pax{Adults: 2} }, "flight_pax_mismatch"},
		{"8 fx", func(p *domain.Package, _ *domain.TripRequest) { p.Items[1].FX.Date = domain.DateOf(now).AddDays(-10) }, "fx_stale"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, req := valid()
			c.mut(&p, &req)
			if v := CheckInvariants(p, req, now, Options{FXMaxAgeDays: 4}); !codes(v)[c.code] {
				t.Fatalf("want %s, got %+v", c.code, v)
			}
		})
	}
}

func TestNumbersNotInPackages(t *testing.T) {
	p, _ := valid()
	pk := []domain.Package{p}
	if u := NumbersNotInPackages("Total paket Rp 14.000.000 dan budget 20 juta.", pk); len(u) != 0 {
		t.Fatalf("false positive %v", u)
	}
	if u := NumbersNotInPackages("Tiket kereta hanya Rp 350.000 saja.", pk); len(u) != 1 {
		t.Fatalf("missed invented number: %v", u)
	}
	if u := NumbersNotInPackages("Penerbangan sekitar 7 juta.", pk); len(u) != 0 {
		t.Fatalf("rounded juta flagged: %v", u)
	}
}

func TestRupiah(t *testing.T) {
	if Rupiah(17850000) != "Rp 17.850.000" {
		t.Fatal(Rupiah(17850000))
	}
}

func TestNumbersNotInPackages_ExactRupiahMustMatch(t *testing.T) {
	p, _ := valid()
	// 3.000.000 rounds to no amount at the thousand, so the exact form is flagged
	// even though "3 juta" would be accepted as a rounding of nothing either.
	if u := NumbersNotInPackages("Total hanya Rp 3.000.000 saja.", []domain.Package{p}); len(u) != 1 {
		t.Fatalf("%v", u)
	}
}
