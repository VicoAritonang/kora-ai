package packagebuilder

import (
	"errors"
	"math/rand"
	"testing"
	"testing/quick"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/domain"
)

var (
	now  = time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)
	jst  = time.FixedZone("JST", 9*3600)
	wib  = time.FixedZone("WIB", 7*3600)
	jpyQ = domain.FXQuote{Base: "JPY", Quote: "IDR", RateE8: 10_650_000_000, Date: domain.DateOf(now), Source: "test"}
)

func fxTable() domain.FXTable { return domain.FXTable{"JPY": jpyQ} }

func f64(v float64) *float64 { return &v }

func norms() CostNorms {
	mk := func(cat, tier, label, unit string, amt int64) domain.Norm {
		return domain.Norm{
			ID: uuid.NewSHA1(uuid.Nil, []byte(cat+tier+label)), Destination: "OSA", Category: cat, Tier: tier,
			Label: label, Unit: unit, Amount: domain.Money{Amount: amt, Currency: "JPY"},
			SourceLabel: "test norm", VerifiedAt: domain.MustDate("2026-10-01"),
		}
	}
	transfer := mk("transport_link", "", "KIX → Namba (Nankai)", "per_ride", 970)
	visa := domain.Norm{
		ID: uuid.NewSHA1(uuid.Nil, []byte("visa")), Destination: "OSA", Category: "visa", Label: "Visa waiver e-paspor",
		Unit: "per_person", Amount: domain.IDR(0), SourceLabel: "test", VerifiedAt: domain.MustDate("2026-10-01"),
	}
	temple := mk("attraction", "", "Kuil Kiyomizu", "per_person", 500)
	temple.ChildFactor = f64(0.5)
	usj := mk("attraction", "", "Universal Studios Japan", "per_person", 10900)
	return CostNorms{
		Meals: map[string]domain.Norm{
			"hemat": mk("meal", "hemat", "Makan hemat", "per_meal", 900),
			"sedang": mk("meal", "sedang", "Makan sedang", "per_meal", 1500),
			"nyaman": mk("meal", "nyaman", "Makan nyaman", "per_meal", 3000),
		},
		LocalDaily: map[string]domain.Norm{
			"hemat": mk("local_transport_daily", "hemat", "Transport harian", "per_day", 800),
			"sedang": mk("local_transport_daily", "sedang", "Transport harian", "per_day", 1200),
			"nyaman": mk("local_transport_daily", "nyaman", "Transport harian", "per_day", 2500),
		},
		AirportTransfer: &transfer,
		Visa:            &visa,
		Attractions:     map[uuid.UUID]domain.Norm{temple.ID: temple, usj.ID: usj},
	}
}

// flight builds a round trip: overnight CGK→KIX arriving the next morning
// (JST) and a return departing at depHour on the return date.
func flight(name string, priceIDR int64, depart domain.Date, nights, stops int, durH float64, depHour int) domain.FlightOffer {
	out0 := time.Date(depart.Year, depart.Month, depart.Day, 23, 30, 0, 0, wib)
	arr := time.Date(depart.Year, depart.Month, depart.Day+1, 8, 15, 0, 0, jst)
	ret := depart.AddDays(1 + nights)
	retDep := time.Date(ret.Year, ret.Month, ret.Day, depHour, 0, 0, 0, jst)
	return domain.FlightOffer{
		ID: uuid.NewSHA1(uuid.Nil, []byte("f"+name)), Supplier: "sandbox", SupplierRef: name,
		Price: domain.IDR(priceIDR), PriceIDR: priceIDR, FX: domain.Identity("IDR", domain.DateOf(now)),
		Outbound: []domain.Segment{{Carrier: "GA", CarrierName: name, FlightNumber: "GA882", From: "CGK", To: "KIX", DepartAt: out0, ArriveAt: arr}},
		Inbound:  []domain.Segment{{Carrier: "GA", CarrierName: name, FlightNumber: "GA883", From: "KIX", To: "CGK", DepartAt: retDep, ArriveAt: retDep.Add(7 * time.Hour)}},
		Stops: stops, Duration: time.Duration(durH * float64(time.Hour)), Cabin: "economy",
		Pax: domain.Pax{Adults: 1}, FetchedAt: now, ExpiresAt: now.Add(30 * time.Minute),
	}
}

func stay(name, area string, totalJPY int64, checkIn domain.Date, nights int, rating float64, stars int, dist int) domain.StayOffer {
	m := domain.Money{Amount: totalJPY, Currency: "JPY"}
	idr, _ := jpyQ.Convert(m)
	d := dist
	return domain.StayOffer{
		ID: uuid.NewSHA1(uuid.Nil, []byte("s"+name)), Supplier: "sandbox", SupplierRef: name, Name: name,
		City: "OSA", Area: area, Rating: rating, Stars: stars,
		PricePerNight: domain.Money{Amount: totalJPY / int64(nights), Currency: "JPY"}, PriceTotal: m,
		PriceIDR: idr.Amount, FX: jpyQ, CheckIn: checkIn, CheckOut: checkIn.AddDays(nights), Guests: 1,
		DistanceToStationM: &d, FetchedAt: now, ExpiresAt: now.Add(time.Hour),
	}
}

func request(budget int64, nights int) domain.TripRequest {
	b := budget
	dep := domain.MustDate("2027-03-10")
	return domain.TripRequest{
		ID: uuid.NewSHA1(uuid.Nil, []byte("req")), Mode: domain.IntentPlanTrip,
		Origin: domain.Place{Code: "JKT", Label: "Jakarta"}, Destination: &domain.Place{Code: "OSA", Label: "Osaka"},
		Budget: &domain.Money{Amount: budget, Currency: "IDR"}, BudgetIDR: &b,
		DepartDate: dep, ReturnDate: dep.AddDays(nights + 1), Nights: nights, Pax: domain.Pax{Adults: 1},
	}
}

func osakaInput(budget int64) Input {
	req := request(budget, 6)
	arrive := req.DepartDate.AddDays(1)
	return Input{
		Request: req,
		Flights: []domain.FlightOffer{
			flight("LCC", 5_200_000, req.DepartDate, 6, 1, 14, 13),
			flight("Garuda", 7_400_000, req.DepartDate, 6, 0, 7.5, 13),
			flight("JAL", 9_100_000, req.DepartDate, 6, 0, 7.2, 13),
		},
		Stays: []domain.StayOffer{
			stay("Hostel Namba", "Namba", 36_000, arrive, 6, 8.1, 2, 300),
			stay("Hotel Umeda", "Umeda", 72_000, arrive, 6, 8.4, 3, 400),
			stay("Hotel Shin-Osaka", "Shin-Osaka", 60_000, arrive, 6, 7.9, 3, 1500),
			stay("Grand Osaka", "Umeda", 180_000, arrive, 6, 9.1, 5, 200),
		},
		Norms: norms(), FX: fxTable(), Policy: DefaultPolicy(), Now: now,
	}
}

func sumItems(p domain.Package) int64 {
	var s int64
	for _, it := range p.Items {
		s += it.AmountIDR
	}
	return s
}

func TestBuild_Osaka20Juta_AllPackagesFit(t *testing.T) {
	out, err := Build(osakaInput(20_000_000))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Packages) < 2 {
		t.Fatalf("want at least 2 packages, got %d", len(out.Packages))
	}
	seen := map[string]bool{}
	for _, p := range out.Packages {
		if p.Feasibility == domain.FeasibilityOverBudget {
			t.Errorf("%s: over budget", p.Tier)
		}
		k := p.FlightOfferID.String() + p.StayOfferID.String()
		if seen[k] {
			t.Errorf("%s duplicates an earlier combination", p.Tier)
		}
		seen[k] = true
	}
}

func TestBuild_Osaka_ThreeTiersFit(t *testing.T) {
	out, err := Build(osakaInput(45_000_000))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Packages) != 3 {
		t.Fatalf("want 3 packages, got %d (%+v)", len(out.Packages), out.Rejected)
	}
	tiers := []domain.Tier{domain.TierHemat, domain.TierSeimbang, domain.TierNyaman}
	for i, p := range out.Packages {
		if p.Tier != tiers[i] {
			t.Errorf("package %d tier %s", i, p.Tier)
		}
		if p.TotalIDR != sumItems(p) || p.TotalIDR != p.LiveIDR+p.EstimateIDR {
			t.Errorf("%s: totals inconsistent", p.Tier)
		}
		if p.Feasibility != domain.FeasibilityFits {
			t.Errorf("%s: feasibility %s total %d buffer %d", p.Tier, p.Feasibility, p.TotalIDR, p.BufferIDR)
		}
		if len(p.Days) != 7 {
			t.Errorf("%s: %d days", p.Tier, len(p.Days))
		}
	}
	// Hemat takes the cheapest flight within reasonable stops; Nyaman the
	// direct one with the best hotel that still fits.
	if out.Packages[0].FlightOfferID == nil || *out.Packages[0].FlightOfferID != osakaInput(0).Flights[0].ID {
		t.Errorf("hemat should use the LCC")
	}
	if *out.Packages[2].StayOfferID != osakaInput(0).Stays[3].ID {
		t.Errorf("nyaman should use the 5-star hotel")
	}
}

func TestBuild_CheckInIsArrivalDate_ForOvernightFlight(t *testing.T) {
	out, err := Build(osakaInput(20_000_000))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range out.Packages {
		var fl, st domain.PackageItem
		for _, it := range p.Items {
			switch it.Category {
			case domain.CatFlight:
				fl = it
			case domain.CatStay:
				st = it
			}
		}
		if fl.Details["arrival_date"] != st.Details["check_in"] {
			t.Errorf("%s: arrival %v check-in %v", p.Tier, fl.Details["arrival_date"], st.Details["check_in"])
		}
		if fl.Details["depart_date"] == st.Details["check_in"] {
			t.Errorf("%s: check-in on the departure date of an overnight flight", p.Tier)
		}
		if p.Days[0].Date.String() != st.Details["check_in"] {
			t.Errorf("%s: day 0 is not the arrival date", p.Tier)
		}
	}
}

func TestBuild_StayWithWrongDates_IsNotCombined(t *testing.T) {
	in := osakaInput(20_000_000)
	// Check-in on the (overnight) departure date: must never pair.
	in.Stays = []domain.StayOffer{stay("Wrong", "Namba", 36_000, in.Request.DepartDate, 7, 8.0, 2, 100)}
	_, err := Build(in)
	if !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("want ErrNoCandidates, got %v", err)
	}
}

func TestBuild_OverBudget_ReturnsCheapestWithAlternatives(t *testing.T) {
	out, err := Build(osakaInput(10_000_000))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Packages) != 1 {
		t.Fatalf("want 1 package, got %d", len(out.Packages))
	}
	p := out.Packages[0]
	if p.Feasibility != domain.FeasibilityOverBudget || p.ShortfallIDR <= 0 {
		t.Fatalf("feasibility %s shortfall %d", p.Feasibility, p.ShortfallIDR)
	}
	if p.ShortfallIDR != p.TotalIDR+p.BufferIDR-10_000_000 {
		t.Errorf("shortfall %d", p.ShortfallIDR)
	}
	kinds := map[string]bool{}
	for _, a := range p.Alternatives {
		kinds[a.Kind] = true
	}
	if !kinds["add_funds"] || !kinds["fewer_nights"] {
		t.Errorf("alternatives %+v", p.Alternatives)
	}
	if p.Alternatives[len(p.Alternatives)-1].Kind != "add_funds" {
		t.Errorf("add_funds must be last")
	}
}

func TestBuild_BudgetBelowCheapestFlight_OnlyDestinationAndFunds(t *testing.T) {
	in := osakaInput(3_000_000)
	in.Destinations = []DestinationOption{{Code: "BKK", Label: "Bangkok", TotalIDR: 2_900_000}}
	out, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range out.Packages[0].Alternatives {
		if a.Kind != "other_destination" && a.Kind != "add_funds" {
			t.Errorf("unexpected alternative %s", a.Kind)
		}
	}
}

func TestBuild_Tight_WhenBufferDoesNotFit(t *testing.T) {
	in := osakaInput(0)
	in.Flights = in.Flights[:1]
	in.Stays = in.Stays[:1]
	probe, _ := Build(in)
	total := probe.Packages[0].TotalIDR
	// Budget just above the total: the 10% buffer cannot fit.
	in.Request = request(total+1000, 6)
	out, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Packages[0].Feasibility != domain.FeasibilityTight {
		t.Fatalf("got %s", out.Packages[0].Feasibility)
	}
}

func TestBuild_ExpiredOffersRejected(t *testing.T) {
	in := osakaInput(20_000_000)
	in.Now = now.Add(2 * time.Hour) // all offers expired
	_, err := Build(in)
	if !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("got %v", err)
	}
}

func TestBuild_NoLiveFlights_UsesEstimate(t *testing.T) {
	in := osakaInput(20_000_000)
	in.Flights = nil
	in.FlightEstimate = &domain.Estimate{
		PriceCacheID: uuid.NewSHA1(uuid.Nil, []byte("pc")), Kind: "flight_route_month", Origin: "JKT", Destination: "OSA",
		Month: domain.MustDate("2027-03-01"), MedianIDR: 6_500_000, SampleCount: 9, RefreshedAt: now,
	}
	in.Request.DepartDate = in.Request.DepartDate.AddDays(1) // estimate assumes same-day arrival
	out, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range out.Packages {
		fl := p.Items[0]
		if fl.PriceKind != domain.PriceEstimate || fl.PriceCacheID == nil || fl.OfferID != nil {
			t.Fatalf("flight item %+v", fl)
		}
	}
}

func TestBuild_EarlyMorningReturn_NoMealsOnLastDay(t *testing.T) {
	in := osakaInput(20_000_000)
	in.Flights = []domain.FlightOffer{flight("Red-eye", 5_000_000, in.Request.DepartDate, 6, 0, 7, 1)}
	out, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	last := len(out.Packages[0].Days) - 1
	for _, it := range out.Packages[0].Items {
		if it.Category == domain.CatMeal && it.DayIndex != nil && *it.DayIndex == last {
			t.Fatalf("meal on departure day: %+v", it)
		}
	}
}

func TestBuild_Children_UseChildFactor(t *testing.T) {
	in := osakaInput(40_000_000)
	in.Request.Pax = domain.Pax{Adults: 1, Children: 1}
	for i := range in.Flights {
		in.Flights[i].Pax = in.Request.Pax
	}
	for i := range in.Stays {
		in.Stays[i].Guests = 2
	}
	n := in.Norms
	var temple domain.Norm
	for _, a := range n.Attractions {
		if a.ChildFactor != nil {
			temple = a
		}
	}
	in.DayPlan = &DayPlan{Days: []PlanDay{{DayIndex: 2, Title: "Kyoto", NormIDs: []uuid.UUID{temple.ID}}}}
	out, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range out.Packages[0].Items {
		if it.Category == domain.CatActivity {
			// ¥500 adult + ¥250 child = ¥750 → 79 875 IDR.
			if it.AmountIDR != 79875 {
				t.Fatalf("activity amount %d", it.AmountIDR)
			}
			return
		}
	}
	t.Fatal("activity missing")
}

func TestBuild_HematExcludesExpensiveAttraction(t *testing.T) {
	in := osakaInput(30_000_000)
	var usj domain.Norm
	for _, a := range in.Norms.Attractions {
		if a.ChildFactor == nil {
			usj = a
		}
	}
	in.DayPlan = &DayPlan{Days: []PlanDay{{DayIndex: 3, Title: "USJ", NormIDs: []uuid.UUID{usj.ID}}}}
	out, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	has := func(p domain.Package) bool {
		for _, it := range p.Items {
			if it.Category == domain.CatActivity {
				return true
			}
		}
		return false
	}
	if has(out.Packages[0]) {
		t.Error("hemat includes USJ")
	}
	if !has(out.Packages[len(out.Packages)-1]) {
		t.Error("nyaman excludes USJ")
	}
}

func TestBuild_NoBudget_Unbounded(t *testing.T) {
	in := osakaInput(0)
	in.Request.Budget, in.Request.BudgetIDR = nil, nil
	out, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range out.Packages {
		if p.Feasibility != domain.FeasibilityUnbounded || p.ShortfallIDR != 0 {
			t.Fatalf("%+v", p.Feasibility)
		}
	}
}

func TestBuild_MissingMealNorm_RecordsGap(t *testing.T) {
	in := osakaInput(20_000_000)
	delete(in.Norms.Meals, "sedang")
	out, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range out.Gaps {
		if g == "meal:sedang" {
			found = true
		}
	}
	if !found {
		t.Fatalf("gaps %v", out.Gaps)
	}
}

func TestBuild_Deterministic(t *testing.T) {
	a, _ := Build(osakaInput(20_000_000))
	b, _ := Build(osakaInput(20_000_000))
	for i := range a.Packages {
		if a.Packages[i].ID != b.Packages[i].ID || a.Packages[i].TotalIDR != b.Packages[i].TotalIDR {
			t.Fatal("non-deterministic output")
		}
	}
}

// Property: for random candidate sets and budgets, totals always equal the
// sum of items, and "fits" always means total + buffer ≤ budget.
func TestBuild_InvariantsHold_Property(t *testing.T) {
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))
		budget := int64(3_000_000 + r.Intn(40_000_000))
		nights := 2 + r.Intn(8)
		req := request(budget, nights)
		arrive := req.DepartDate.AddDays(1)
		in := Input{Request: req, Norms: norms(), FX: fxTable(), Policy: DefaultPolicy(), Now: now}
		for i := 0; i < 1+r.Intn(5); i++ {
			in.Flights = append(in.Flights, flight(string(rune('A'+i)), int64(3_000_000+r.Intn(9_000_000)), req.DepartDate, nights, r.Intn(3), 6+r.Float64()*15, r.Intn(24)))
		}
		for i := 0; i < 1+r.Intn(6); i++ {
			in.Stays = append(in.Stays, stay(string(rune('a'+i)), []string{"Namba", "Umeda", "Tennoji"}[r.Intn(3)],
				int64(4000+r.Intn(40000))*int64(nights), arrive, nights, 7+r.Float64()*3, 1+r.Intn(5), r.Intn(2000)))
		}
		out, err := Build(in)
		if err != nil {
			return false
		}
		for _, p := range out.Packages {
			if p.TotalIDR != sumItems(p) || p.TotalIDR != p.LiveIDR+p.EstimateIDR {
				return false
			}
			switch p.Feasibility {
			case domain.FeasibilityFits:
				if p.TotalIDR+p.BufferIDR > budget {
					return false
				}
			case domain.FeasibilityTight:
				if p.TotalIDR > budget || p.TotalIDR+p.BufferIDR <= budget {
					return false
				}
			case domain.FeasibilityOverBudget:
				if p.TotalIDR <= budget || len(out.Packages) != 1 {
					return false
				}
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func TestRupiah(t *testing.T) {
	for in, want := range map[int64]string{0: "Rp 0", 999: "Rp 999", 1000: "Rp 1.000", 20000000: "Rp 20.000.000"} {
		if got := rupiah(in); got != want {
			t.Errorf("%d: %s", in, got)
		}
	}
}
