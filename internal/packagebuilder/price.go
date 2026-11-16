package packagebuilder

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/domain"
)

// price builds one complete package for a combination (steps 2–4, 7).
// Calling it never mutates builder state other than gaps and the id counter,
// so choose() can price speculatively.
func (b *builder) price(tier domain.Tier, c combo) (domain.Package, error) {
	req := b.in.Request
	nights := c.s.nights()
	if nights <= 0 {
		return domain.Package{}, fmt.Errorf("%w: zero nights", ErrInvalidRequest)
	}
	p := domain.Package{
		ID:             b.newID(),
		Tier:           tier,
		Status:         domain.PackageProposed,
		Title:          fmt.Sprintf("%s %d malam — %s", b.destLabel(), nights, tier.Label()),
		BudgetIDR:      req.BudgetIDR,
		BuilderVersion: b.in.Policy.Version,
		Assumptions:    append([]domain.Assumption{}, req.Assumptions...),
		Alternatives:   []domain.Alternative{},
		CreatedAt:      b.in.Now,
	}
	if fid := c.f.id(); fid != uuid.Nil {
		p.FlightOfferID = &fid
	}
	if sid := c.s.id(); sid != uuid.Nil {
		p.StayOfferID = &sid
	}
	var items []domain.PackageItem
	add := func(it domain.PackageItem) {
		it.SortOrder = len(items)
		if it.ID == uuid.Nil {
			it.ID = b.newID()
		}
		if it.Details == nil {
			it.Details = map[string]any{}
		}
		items = append(items, it)
	}

	// --- Fixed costs: flight, stay, visa, airport transfers (step 2).
	add(b.flightItem(c.f))
	add(b.stayItem(c.s))
	if c.f.est != nil {
		p.Assumptions = append(p.Assumptions, domain.Assumption{
			Slot: "flight_price", Value: "estimate",
			Reason: "Harga live penerbangan belum tersedia untuk tanggal ini; dipakai estimasi dari Indeks Biaya Destinasi.",
			Source: "price_index",
		})
	}
	if c.s.est != nil {
		p.Assumptions = append(p.Assumptions, domain.Assumption{
			Slot: "stay_price", Value: "estimate",
			Reason: "Harga live penginapan belum tersedia; dipakai median per malam dari Indeks Biaya Destinasi.",
			Source: "price_index",
		})
	}
	if req.Pax.Children > 0 && c.f.est != nil {
		p.Assumptions = append(p.Assumptions, domain.Assumption{
			Slot: "child_fare", Value: "adult", Reason: "Tarif anak diestimasi sama dengan dewasa.", Source: "default",
		})
	}
	if n := b.in.Norms.Visa; n != nil {
		if it, ok := b.normItem(*n, 1, nil, domain.CatVisa, n.Label); ok {
			add(it)
		}
	} else {
		b.gap("visa")
	}
	lastDay := nights
	if n := b.in.Norms.AirportTransfer; n != nil {
		d0, dN := 0, lastDay
		if it, ok := b.normItem(*n, 1, &d0, domain.CatLocalTransport, n.Label+" (kedatangan)"); ok {
			add(it)
		}
		if it, ok := b.normItem(*n, 1, &dN, domain.CatLocalTransport, n.Label+" (kepulangan)"); ok {
			add(it)
		}
	} else {
		b.gap("airport_transfer")
	}

	// --- Daily costs (step 3).
	days := b.days(c, nights)
	normTier := tier.NormTier()
	meal, hasMeal := b.in.Norms.Meals[normTier]
	if !hasMeal {
		b.gap("meal:" + normTier)
	}
	local, hasLocal := b.in.Norms.LocalDaily[normTier]
	if !hasLocal {
		b.gap("local_transport:" + normTier)
	}
	excluded := 0
	for i := range days {
		di := i
		if hasMeal {
			meals := 3
			switch i {
			case 0:
				meals = arrivalMeals(c.f.arrHour)
			case lastDay:
				meals = departureMeals(c.f.depHour)
			}
			if meals > 0 {
				if it, ok := b.normItem(meal, int64(meals), &di, domain.CatMeal, fmt.Sprintf("Makan (%d kali)", meals)); ok {
					it.Quantity = meals
					add(it)
				}
			}
		}
		if hasLocal && i != 0 && i != lastDay {
			if it, ok := b.normItem(local, 1, &di, domain.CatLocalTransport, local.Label); ok {
				add(it)
			}
		}
		for _, act := range b.dayActivities(i) {
			it, ok := act(tier, di)
			if !ok {
				excluded++
				continue
			}
			add(it)
		}
	}
	if excluded > 0 {
		p.Assumptions = append(p.Assumptions, domain.Assumption{
			Slot: "activities", Value: excluded,
			Reason: fmt.Sprintf("%d atraksi berbayar di atas batas tingkat %s tidak dimasukkan.", excluded, tier.Label()),
			Source: "policy",
		})
	}

	p.Items = items
	p.Days = days
	b.totals(&p)
	return p, nil
}

func (b *builder) destLabel() string {
	if b.in.DestinationLabel != "" {
		return b.in.DestinationLabel
	}
	if d := b.in.Request.Destination; d != nil {
		return d.Label
	}
	return "Perjalanan"
}

// totals computes totals, buffer and feasibility (steps 4 and 7).
func (b *builder) totals(p *domain.Package) {
	var total, live, est, foreignEst int64
	for _, it := range p.Items {
		total += it.AmountIDR
		if it.PriceKind == domain.PriceLive {
			live += it.AmountIDR
		} else {
			est += it.AmountIDR
			if it.Currency != "IDR" {
				foreignEst += it.AmountIDR
			}
		}
	}
	p.TotalIDR, p.LiveIDR, p.EstimateIDR = total, live, est
	fxPart := ceilPct(foreignEst, b.in.Policy.FXBufferPct)
	if p.BudgetIDR == nil {
		p.BufferIDR = ceilPct(total, b.in.Policy.ContingencyPct) + fxPart
		p.Feasibility = domain.FeasibilityUnbounded
		return
	}
	budget := *p.BudgetIDR
	p.BufferIDR = ceilPct(budget, b.in.Policy.ContingencyPct) + fxPart
	switch {
	case total+p.BufferIDR <= budget:
		p.Feasibility = domain.FeasibilityFits
	case total <= budget:
		p.Feasibility = domain.FeasibilityTight
	default:
		p.Feasibility = domain.FeasibilityOverBudget
		p.ShortfallIDR = total + p.BufferIDR - budget
	}
}

func ceilPct(v int64, pct int) int64 {
	if v <= 0 || pct <= 0 {
		return 0
	}
	n := v * int64(pct)
	return (n + 99) / 100
}

func (b *builder) flightItem(f flightOpt) domain.PackageItem {
	req := b.in.Request
	if f.live != nil {
		o := f.live
		exp := o.ExpiresAt
		id := o.ID
		return domain.PackageItem{
			Category: domain.CatFlight, Quantity: 1,
			Title:      fmt.Sprintf("%s → %s, pulang-pergi (%s)", firstFrom(o.Outbound), lastTo(o.Outbound), strings.Join(o.Carriers(), ", ")),
			UnitAmount: o.Price.Amount, Currency: o.Price.Currency, AmountIDR: o.PriceIDR, FX: o.FX,
			PriceKind: domain.PriceLive, OfferID: &id, SourceLabel: supplierLabel(o.Supplier),
			PricedAt: o.FetchedAt, ExpiresAt: &exp,
			Details: map[string]any{
				"depart_date": o.DepartureDate().String(), "arrival_date": f.arrival.String(),
				"return_depart_date": f.returnDep.String(), "stops": o.Stops,
				"duration_minutes": int(o.Duration.Minutes()), "carriers": o.Carriers(),
				"baggage": o.Baggage, "cabin": o.Cabin, "outbound": o.Outbound, "inbound": o.Inbound,
				"pax_adults": o.Pax.Adults, "pax_children": o.Pax.Children, "supplier": o.Supplier,
			},
		}
	}
	e := f.est
	pcID := e.PriceCacheID
	return domain.PackageItem{
		Category: domain.CatFlight, Quantity: req.Pax.Total(),
		Title:      fmt.Sprintf("Penerbangan %s → %s pulang-pergi (estimasi)", req.Origin.Label, b.destLabel()),
		UnitAmount: e.MedianIDR, Currency: "IDR", AmountIDR: f.priceIDR, FX: domain.Identity("IDR", domain.DateOf(e.RefreshedAt)),
		PriceKind: domain.PriceEstimate, PriceCacheID: &pcID,
		SourceLabel: fmt.Sprintf("Indeks Biaya Destinasi — median %s (%d sampel)", monthLabel(e.Month), e.SampleCount),
		PricedAt:    e.RefreshedAt,
		Details: map[string]any{
			"depart_date": req.DepartDate.String(), "arrival_date": f.arrival.String(),
			"return_depart_date": f.returnDep.String(), "estimated": true,
			"pax_adults": req.Pax.Adults, "pax_children": req.Pax.Children, "stale": e.Stale, "note": e.Note,
		},
	}
}

func (b *builder) stayItem(s stayOpt) domain.PackageItem {
	req := b.in.Request
	if s.live != nil {
		o := s.live
		exp := o.ExpiresAt
		id := o.ID
		return domain.PackageItem{
			Category: domain.CatStay, Quantity: 1,
			Title:      fmt.Sprintf("%s, %s (%d malam)", o.Name, o.Area, o.Nights()),
			UnitAmount: o.PriceTotal.Amount, Currency: o.PriceTotal.Currency, AmountIDR: o.PriceIDR, FX: o.FX,
			PriceKind: domain.PriceLive, OfferID: &id, SourceLabel: supplierLabel(o.Supplier),
			PricedAt: o.FetchedAt, ExpiresAt: &exp,
			Details: map[string]any{
				"check_in": o.CheckIn.String(), "check_out": o.CheckOut.String(), "nights": o.Nights(),
				"guests": o.Guests, "area": o.Area, "rating": o.Rating, "stars": o.Stars,
				"room_name": o.RoomName, "distance_to_station_m": o.DistanceToStationM,
				"price_per_night_idr": s.perNightIDR, "untrusted_fields": []string{"title", "room_name"},
			},
		}
	}
	e := s.est
	pcID := e.PriceCacheID
	return domain.PackageItem{
		Category: domain.CatStay, Quantity: s.nights() * s.rooms,
		Title:      fmt.Sprintf("Penginapan tingkat %s (%d malam, %d kamar, estimasi)", s.estTier.Label(), s.nights(), s.rooms),
		UnitAmount: e.MedianIDR, Currency: "IDR", AmountIDR: s.priceIDR, FX: domain.Identity("IDR", domain.DateOf(e.RefreshedAt)),
		PriceKind: domain.PriceEstimate, PriceCacheID: &pcID,
		SourceLabel: fmt.Sprintf("Indeks Biaya Destinasi — median per malam %s", monthLabel(e.Month)),
		PricedAt:    e.RefreshedAt,
		Details: map[string]any{
			"check_in": s.checkIn.String(), "check_out": s.checkOut.String(), "nights": s.nights(),
			"guests": req.Pax.Total(), "rooms": s.rooms, "estimated": true,
		},
	}
}

// normItem prices a cost norm for the party: unit × qty × adults, plus
// children at the norm's child factor (rounded up). Returns false and records
// a gap when the norm cannot be converted to rupiah.
func (b *builder) normItem(n domain.Norm, qty int64, day *int, cat domain.ItemCategory, title string) (domain.PackageItem, bool) {
	pax := b.in.Request.Pax
	persons := int64(1)
	childPct := int64(100)
	if n.ChildFactor != nil {
		childPct = int64(*n.ChildFactor*100 + 0.5)
	}
	var amount int64
	if n.Unit == "per_trip" || n.Unit == "per_day_group" {
		amount = n.Amount.Amount * qty
	} else {
		persons = int64(pax.Adults)
		amount = n.Amount.Amount * qty * persons
		if pax.Children > 0 {
			ch := n.Amount.Amount * qty * int64(pax.Children) * childPct
			amount += (ch + 99) / 100
		}
	}
	src := domain.Money{Amount: amount, Currency: n.Amount.Currency}
	idr, q, err := b.in.FX.ToIDR(src)
	if err != nil {
		b.gap("fx:" + n.Amount.Currency)
		return domain.PackageItem{}, false
	}
	nid := n.ID
	details := map[string]any{"unit": n.Unit, "adults": pax.Adults, "children": pax.Children, "verified_at": n.VerifiedAt.String()}
	if n.ChildFactor != nil && pax.Children > 0 {
		details["child_factor"] = *n.ChildFactor
	}
	if n.ChildFactor == nil && pax.Children > 0 && n.Unit != "per_trip" {
		details["child_priced_as_adult"] = true
	}
	return domain.PackageItem{
		DayIndex: day, Category: cat, Title: title, Quantity: int(qty),
		UnitAmount: n.Amount.Amount, Currency: n.Amount.Currency, AmountIDR: idr, FX: q,
		PriceKind: domain.PriceEstimate, NormID: &nid, SourceLabel: n.SourceLabel, SourceURL: n.SourceURL,
		PricedAt: n.VerifiedAt.Time(), Details: details,
	}, true
}

// days lays out nights+1 itinerary days from the day plan, filling gaps with
// neutral titles. Dates start on the arrival date.
func (b *builder) days(c combo, nights int) []domain.PackageDay {
	plan := map[int]PlanDay{}
	if b.in.DayPlan != nil {
		for _, d := range b.in.DayPlan.Days {
			plan[d.DayIndex] = d
		}
	}
	out := make([]domain.PackageDay, nights+1)
	for i := 0; i <= nights; i++ {
		d := domain.PackageDay{DayIndex: i, Date: c.s.checkIn.AddDays(i)}
		if pd, ok := plan[i]; ok {
			d.Title, d.Summary, d.Area = pd.Title, pd.Summary, pd.Area
		}
		if d.Title == "" {
			switch i {
			case 0:
				d.Title = "Tiba di " + b.destLabel()
			case nights:
				d.Title = "Hari kepulangan"
			default:
				d.Title = fmt.Sprintf("Hari %d — eksplorasi bebas", i+1)
			}
		}
		out[i] = d
	}
	return out
}

// dayActivities returns pricing closures for the activities planned on day i.
// A closure returns false when the tier excludes the activity.
func (b *builder) dayActivities(i int) []func(domain.Tier, int) (domain.PackageItem, bool) {
	if b.in.DayPlan == nil {
		return nil
	}
	var out []func(domain.Tier, int) (domain.PackageItem, bool)
	for _, pd := range b.in.DayPlan.Days {
		if pd.DayIndex != i {
			continue
		}
		for _, nid := range pd.NormIDs {
			n, ok := b.in.Norms.Attractions[nid]
			if !ok {
				continue // validated earlier by propose_day_plan
			}
			out = append(out, func(t domain.Tier, di int) (domain.PackageItem, bool) {
				it, ok := b.normItem(n, 1, &di, domain.CatActivity, n.Label)
				if !ok {
					return it, false
				}
				perPerson := it.AmountIDR / int64(max(1, b.in.Request.Pax.Total()))
				if !b.tierAllows(t, perPerson) {
					return it, false
				}
				return it, true
			})
		}
		for _, aid := range pd.ActivityOfferIDs {
			a, ok := b.acts[aid]
			if !ok {
				continue
			}
			out = append(out, func(t domain.Tier, di int) (domain.PackageItem, bool) {
				if !b.tierAllows(t, a.PriceIDR) {
					return domain.PackageItem{}, false
				}
				id, exp := a.ID, a.ExpiresAt
				n := int64(b.in.Request.Pax.Total())
				return domain.PackageItem{
					DayIndex: &di, Category: domain.CatActivity, Title: a.Title, Quantity: int(n),
					UnitAmount: a.Price.Amount, Currency: a.Price.Currency, AmountIDR: a.PriceIDR * n, FX: a.FX,
					PriceKind: domain.PriceLive, OfferID: &id, SourceLabel: supplierLabel(a.Supplier),
					PricedAt: a.FetchedAt, ExpiresAt: &exp, Details: map[string]any{"untrusted_fields": []string{"title"}},
				}, true
			})
		}
	}
	return out
}

func (b *builder) tierAllows(t domain.Tier, perPersonIDR int64) bool {
	switch t {
	case domain.TierHemat:
		return perPersonIDR <= b.in.Policy.HematMaxActivityIDR
	case domain.TierSeimbang:
		return perPersonIDR <= b.in.Policy.SeimbangMaxActivityIDR
	}
	return true
}

// arrivalMeals counts meals on the arrival day from the landing hour.
func arrivalMeals(hour int) int {
	switch {
	case hour < 10:
		return 3
	case hour < 14:
		return 2
	case hour < 21:
		return 1
	}
	return 0
}

// departureMeals counts meals on the departure day; an early-morning flight
// means none (MODULE_SPEC §9 edge cases).
func departureMeals(hour int) int {
	switch {
	case hour < 8:
		return 0
	case hour < 12:
		return 1
	case hour < 17:
		return 2
	}
	return 3
}

func firstFrom(s []domain.Segment) string {
	if len(s) == 0 {
		return ""
	}
	return s[0].From
}

func lastTo(s []domain.Segment) string {
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1].To
}

func supplierLabel(s string) string {
	switch s {
	case "duffel":
		return "Duffel"
	case "liteapi":
		return "LiteAPI"
	case "viator":
		return "Viator"
	case "sandbox":
		return "Sandbox (data simulasi, bukan harga nyata)"
	}
	return s
}

var bulan = []string{"", "Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}

func monthLabel(d domain.Date) string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%s %d", bulan[d.Month], d.Year)
}

