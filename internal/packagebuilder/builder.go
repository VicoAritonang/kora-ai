package packagebuilder

import (
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/domain"
)

// Errors returned by Build.
var (
	ErrInvalidRequest = errors.New("packagebuilder: invalid request")
	ErrNoCandidates   = errors.New("packagebuilder: no usable flight and stay candidates")
)

// flightOpt is a live offer or an estimate, flattened for ranking.
type flightOpt struct {
	live      *domain.FlightOffer
	est       *domain.Estimate
	arrival   domain.Date
	returnDep domain.Date
	arrHour   int
	depHour   int
	priceIDR  int64
	durationH float64
	stops     int
}

func (f flightOpt) id() uuid.UUID {
	if f.live != nil {
		return f.live.ID
	}
	return uuid.Nil
}

// stayOpt is a live stay or a per-tier estimate sized to a flight's dates.
type stayOpt struct {
	live        *domain.StayOffer
	est         *domain.Estimate
	estTier     domain.Tier
	checkIn     domain.Date
	checkOut    domain.Date
	priceIDR    int64
	perNightIDR int64
	area        string
	rating      float64
	stars       int
	dist        *int
	rooms       int
}

func (s stayOpt) nights() int { return s.checkIn.DaysUntil(s.checkOut) }

func (s stayOpt) id() uuid.UUID {
	if s.live != nil {
		return s.live.ID
	}
	return uuid.Nil
}

type combo struct {
	f flightOpt
	s stayOpt
}

type builder struct {
	in       Input
	out      Output
	counter  int
	flights  []flightOpt
	stays    []domain.StayOffer
	acts     map[uuid.UUID]domain.ActivityOffer
	gapsSeen map[string]bool
}

// Build assembles up to three packages (Hemat, Seimbang, Nyaman). See
// MODULE_SPEC §9 for the algorithm.
func Build(in Input) (Output, error) {
	if in.Request.Pax.Validate() != nil {
		return Output{}, fmt.Errorf("%w: pax", ErrInvalidRequest)
	}
	if in.Policy.Version == "" {
		in.Policy = DefaultPolicy()
	}
	b := &builder{in: in, acts: map[uuid.UUID]domain.ActivityOffer{}, gapsSeen: map[string]bool{}}
	b.out.Rejected = []Rejection{}
	b.out.Gaps = []string{}
	b.filter()

	combos := b.combos()
	if len(combos) == 0 {
		return b.out, ErrNoCandidates
	}

	var pkgs []domain.Package
	seen := map[string]bool{}
	budget := in.Request.BudgetIDR
	for _, tier := range domain.Tiers {
		ranked := b.rank(tier, combos)
		// Prefer a combination no earlier tier used; when every fitting
		// combination is taken, the two tiers are merged (step 8).
		var fresh []combo
		for _, c := range ranked {
			if !seen[comboKey(c)] {
				fresh = append(fresh, c)
			}
		}
		chosen, ok := b.choose(tier, fresh, budget)
		if !ok {
			continue
		}
		seen[comboKey(chosen)] = true
		p, err := b.price(tier, chosen)
		if err != nil {
			return b.out, err
		}
		pkgs = append(pkgs, p)
	}

	if len(pkgs) == 0 {
		// Nothing fits: one cheapest package, honestly labeled, plus ordered
		// ways out (FR-10).
		cheapest := b.rank(domain.TierHemat, combos)[0]
		p, err := b.price(domain.TierHemat, cheapest)
		if err != nil {
			return b.out, err
		}
		p.Alternatives = b.alternatives(p, cheapest, combos)
		pkgs = append(pkgs, p)
	}
	b.out.Packages = pkgs
	return b.out, nil
}

func comboKey(c combo) string {
	return c.f.id().String() + "/" + c.s.id().String() + "/" + c.s.estTierKey()
}

func (s stayOpt) estTierKey() string {
	if s.est != nil {
		return string(s.estTier)
	}
	return ""
}

func (b *builder) newID() uuid.UUID {
	if b.in.NewID != nil {
		return b.in.NewID()
	}
	b.counter++
	return uuid.NewSHA1(b.in.Request.ID, []byte("pb:"+strconv.Itoa(b.counter)))
}

func (b *builder) reject(kind string, id uuid.UUID, reason string) {
	b.out.Rejected = append(b.out.Rejected, Rejection{Kind: kind, OfferID: id, Reason: reason})
}

func (b *builder) gap(g string) {
	if !b.gapsSeen[g] {
		b.gapsSeen[g] = true
		b.out.Gaps = append(b.out.Gaps, g)
	}
}

// filter drops expired, mismatched or unpriceable candidates (step 1).
func (b *builder) filter() {
	req := b.in.Request
	now := b.in.Now
	for i := range b.in.Flights {
		f := &b.in.Flights[i]
		switch {
		case !f.ExpiresAt.After(now):
			b.reject("flight", f.ID, "expired")
		case f.Pax != req.Pax:
			b.reject("flight", f.ID, "pax mismatch")
		case len(f.Outbound) == 0 || len(f.Inbound) == 0:
			b.reject("flight", f.ID, "not a round trip")
		case f.PriceIDR <= 0:
			b.reject("flight", f.ID, "no IDR price")
		default:
			b.flights = append(b.flights, flightOpt{
				live: f, arrival: f.ArrivalDate(), returnDep: f.ReturnDepartureDate(),
				arrHour: f.ArrivalHour(), depHour: f.ReturnDepartureHour(),
				priceIDR: f.PriceIDR, durationH: f.Duration.Hours(), stops: f.Stops,
			})
		}
	}
	if len(b.flights) == 0 && b.in.FlightEstimate != nil && req.Nights > 0 {
		e := b.in.FlightEstimate
		price := e.MedianIDR * int64(req.Pax.Total())
		// Arrival = return − nights: the request's return date already
		// accounts for an overnight outbound.
		ret := req.ReturnDate
		if ret.IsZero() {
			ret = req.DepartDate.AddDays(req.Nights)
		}
		b.flights = append(b.flights, flightOpt{
			est: e, arrival: ret.AddDays(-req.Nights), returnDep: ret,
			arrHour: 12, depHour: 12, priceIDR: price, durationH: 0, stops: 0,
		})
	}
	for i := range b.in.Stays {
		s := b.in.Stays[i]
		switch {
		case !s.ExpiresAt.After(now):
			b.reject("stay", s.ID, "expired")
		case s.Guests != req.Pax.Total():
			b.reject("stay", s.ID, "guest count mismatch")
		case s.Rating > 0 && s.Rating < b.in.Policy.MinStayRating:
			b.reject("stay", s.ID, "rating below minimum")
		case s.PriceIDR <= 0 || s.Nights() <= 0:
			b.reject("stay", s.ID, "no price or no nights")
		default:
			b.stays = append(b.stays, s)
		}
	}
	for _, a := range b.in.Activities {
		if !a.ExpiresAt.After(now) {
			b.reject("activity", a.ID, "expired")
			continue
		}
		b.acts[a.ID] = a
	}
}

// combos pairs flights with stays whose dates line up: check-in on the
// arrival date and check-out on the return departure date (invariant 3).
func (b *builder) combos() []combo {
	var out []combo
	for _, f := range b.flights {
		matched := false
		for i := range b.stays {
			s := &b.stays[i]
			if s.CheckIn != f.arrival || s.CheckOut != f.returnDep {
				continue
			}
			matched = true
			per := s.PriceIDR / int64(max(1, s.Nights()))
			out = append(out, combo{f, stayOpt{
				live: s, checkIn: s.CheckIn, checkOut: s.CheckOut, priceIDR: s.PriceIDR,
				perNightIDR: per, area: s.Area, rating: s.Rating, stars: s.Stars,
				dist: s.DistanceToStationM, rooms: 1,
			}})
		}
		if !matched && len(b.stays) == 0 && len(b.in.StayEstimates) > 0 {
			nights := f.arrival.DaysUntil(f.returnDep)
			if nights <= 0 {
				continue
			}
			rooms := (b.in.Request.Pax.Total() + 1) / 2
			for _, tier := range domain.Tiers {
				e, ok := b.in.StayEstimates[tier]
				if !ok {
					continue
				}
				est := e
				out = append(out, combo{f, stayOpt{
					est: &est, estTier: tier, checkIn: f.arrival, checkOut: f.returnDep,
					perNightIDR: e.MedianIDR, priceIDR: e.MedianIDR * int64(nights) * int64(rooms),
					area: "", rooms: rooms,
				}})
			}
		} else if !matched && f.live != nil {
			b.reject("flight", f.live.ID, "no stay matches arrival "+f.arrival.String()+" / return "+f.returnDep.String())
		}
	}
	return out
}

// rank orders combinations by the tier's preference (MODULE_SPEC §9 step 5).
func (b *builder) rank(tier domain.Tier, cs []combo) []combo {
	out := append([]combo(nil), cs...)
	maxStops := b.in.Policy.MaxReasonableStops
	near := b.in.Policy.NearStationM
	medianNight := medianPerNight(cs)
	sort.SliceStable(out, func(i, j int) bool {
		a, c := out[i], out[j]
		// Estimated stays only serve the tier they were estimated for.
		if (a.s.est != nil) != (c.s.est != nil) {
			return a.s.est == nil
		}
		if a.s.est != nil && a.s.estTier != c.s.estTier {
			return a.s.estTier == tier
		}
		switch tier {
		case domain.TierHemat:
			ar, cr := a.f.stops <= maxStops, c.f.stops <= maxStops
			if ar != cr {
				return ar
			}
			return a.f.priceIDR+a.s.priceIDR < c.f.priceIDR+c.s.priceIDR
		case domain.TierSeimbang:
			an, cn := isNear(a.s.dist, near), isNear(c.s.dist, near)
			if an != cn {
				return an
			}
			// Mid-priced stays first, then flight value (price × hours).
			ad, cd := absInt(a.s.perNightIDR-medianNight), absInt(c.s.perNightIDR-medianNight)
			if ad != cd {
				return ad < cd
			}
			return flightValue(a.f) < flightValue(c.f)
		default: // nyaman
			if a.f.stops != c.f.stops {
				return a.f.stops < c.f.stops
			}
			if a.s.stars != c.s.stars {
				return a.s.stars > c.s.stars
			}
			if a.s.rating != c.s.rating {
				return a.s.rating > c.s.rating
			}
			if a.f.durationH != c.f.durationH {
				return a.f.durationH < c.f.durationH
			}
			return a.f.priceIDR+a.s.priceIDR < c.f.priceIDR+c.s.priceIDR
		}
	})
	return out
}

func flightValue(f flightOpt) float64 {
	h := f.durationH
	if h <= 0 {
		h = 1
	}
	return float64(f.priceIDR) * h
}

func isNear(d *int, limit int) bool { return d != nil && *d <= limit }

func absInt(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func medianPerNight(cs []combo) int64 {
	var v []int64
	for _, c := range cs {
		v = append(v, c.s.perNightIDR)
	}
	if len(v) == 0 {
		return 0
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	return v[len(v)/2]
}

// choose picks the tier's best combination that fits the budget (step 6). A
// "tight" combination is used only when nothing fits with the buffer.
func (b *builder) choose(tier domain.Tier, ranked []combo, budget *int64) (combo, bool) {
	if len(ranked) == 0 {
		return combo{}, false
	}
	if budget == nil {
		return ranked[0], true
	}
	var tight *combo
	for i := range ranked {
		p, err := b.price(tier, ranked[i])
		if err != nil {
			continue
		}
		switch p.Feasibility {
		case domain.FeasibilityFits:
			return ranked[i], true
		case domain.FeasibilityTight:
			if tight == nil {
				tight = &ranked[i]
			}
		}
	}
	if tight != nil {
		return *tight, true
	}
	return combo{}, false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
