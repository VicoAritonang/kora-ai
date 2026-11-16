// Package priceindex fills and reads the Destination Cost Index
// (price_cache). It answers "budget → destination" and out-of-window dates
// without live supplier calls (PLAN §6.4, MODULE_SPEC §13).
package priceindex

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/koraai/koraai/internal/costnorm"
	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/reference"
	"github.com/koraai/koraai/internal/store/postgres"
)

// ErrNoData means the index has nothing for the request.
var ErrNoData = errors.New("priceindex: no data")

// Index reads and refreshes price_cache.
type Index struct {
	DB      *postgres.DB
	Flights ports.FlightSupplier
	Stays   ports.StaySupplier
	FX      *costnorm.FX
	Norms   *costnorm.Service
	Clock   platform.Clock
	Log     *slog.Logger
	TTL     time.Duration // index row validity (7 days)
	Window  int           // booking window days
}

// FlightEstimate returns the per-adult round-trip estimate for a month,
// falling back to the same month last year, then the annual median.
func (x *Index) FlightEstimate(ctx context.Context, origin, dest string, month domain.Date) (domain.Estimate, error) {
	return x.estimate(ctx, "flight_route_month", origin, dest, "", month)
}

// StayEstimate returns the per-night, per-room estimate for a tier.
func (x *Index) StayEstimate(ctx context.Context, dest string, month domain.Date, tier domain.Tier) (domain.Estimate, error) {
	return x.estimate(ctx, "stay_city_month", "", dest, string(tier), month)
}

func (x *Index) estimate(ctx context.Context, kind, origin, dest, tier string, month domain.Date) (domain.Estimate, error) {
	now := x.Clock.Now()
	m := month.FirstOfMonth()
	rows, err := x.DB.Q().PriceCacheRows(ctx, kind, origin, dest, nil, now)
	if err != nil {
		return domain.Estimate{}, err
	}
	var same []domain.Estimate
	for _, r := range rows {
		if r.Tier != tier {
			continue
		}
		same = append(same, r)
		if r.Month == m {
			return r, nil
		}
	}
	prev := domain.Date{Year: m.Year - 1, Month: m.Month, Day: 1}
	for _, r := range same {
		if r.Month == prev {
			r.Note = "Bulan yang sama tahun lalu"
			r.Stale = true
			return r, nil
		}
	}
	if len(same) == 0 {
		return domain.Estimate{}, fmt.Errorf("%w: %s %s→%s %s", ErrNoData, kind, origin, dest, tier)
	}
	// Annual median of what we have, labeled.
	sort.Slice(same, func(i, j int) bool { return same[i].MedianIDR < same[j].MedianIDR })
	med := same[len(same)/2]
	med.Note = fmt.Sprintf("Median dari %d bulan yang tersedia; bulan %s belum ada di indeks", len(same), m)
	med.Month = m
	med.Stale = true
	return med, nil
}

// DestinationEstimate is one row of the budget → destination answer.
type DestinationEstimate struct {
	Code        string             `json:"code"`
	Label       string             `json:"label"`
	Month       domain.Date        `json:"month"`
	Nights      int                `json:"nights"`
	FlightIDR   int64              `json:"flight_idr"`
	StayIDR     map[string]int64   `json:"stay_idr"`
	DailyIDR    map[string]int64   `json:"daily_idr"`
	TotalIDR    map[string]int64   `json:"total_idr"` // per tier
	PriceCache  []string           `json:"price_cache_ids"`
	Notes       []string           `json:"notes,omitempty"`
	Fits        map[string]bool    `json:"fits,omitempty"`
}

// EstimateTrip prices every supported destination for a month from the
// index and the cost norms. No live supplier call is made.
func (x *Index) EstimateTrip(ctx context.Context, origin string, month domain.Date, nights int, pax domain.Pax, budgetIDR *int64, contingencyPct int) ([]DestinationEstimate, error) {
	var out []DestinationEstimate
	for _, c := range reference.Destinations {
		fl, err := x.FlightEstimate(ctx, origin, c.Code, month)
		if err != nil {
			continue
		}
		d := DestinationEstimate{Code: c.Code, Label: c.Label, Month: month.FirstOfMonth(), Nights: nights,
			// FlightIDR is the cheapest sampled fare, shown as "mulai dari".
			FlightIDR: fl.MinIDR * int64(pax.Total()),
			StayIDR:   map[string]int64{}, DailyIDR: map[string]int64{}, TotalIDR: map[string]int64{}, Fits: map[string]bool{}}
		d.PriceCache = append(d.PriceCache, fl.PriceCacheID.String())
		if fl.Note != "" {
			d.Notes = append(d.Notes, "Penerbangan: "+fl.Note)
		}
		norms, valid, err := x.Norms.ForTrip(ctx, c.Code, month)
		if err != nil {
			continue
		}
		fxT, err := x.FX.Table(ctx, costnorm.Currencies(valid)...)
		if err != nil {
			return nil, err
		}
		rooms := int64((pax.Total() + 1) / 2)
		persons := int64(pax.Total())
		for _, tier := range domain.Tiers {
			st, err := x.StayEstimate(ctx, c.Code, month, tier)
			if err != nil {
				continue
			}
			// Hemat books the cheapest sampled fare and room; the other tiers
			// the median. Using the median for Hemat overstated it against
			// live packages (IMPLEMENTATION_NOTES §2).
			flight, perNight := fl.MedianIDR, st.MedianIDR
			if tier == domain.TierHemat {
				flight, perNight = fl.MinIDR, st.MinIDR
			}
			flightTotal := flight * int64(pax.Total())
			stay := perNight * int64(nights) * rooms
			var daily int64
			if m, ok := norms.Meals[tier.NormTier()]; ok {
				v, _, err := fxT.ToIDR(m.Amount.Mul(3 * int64(nights) * persons))
				if err == nil {
					daily += v
				}
			}
			if l, ok := norms.LocalDaily[tier.NormTier()]; ok {
				v, _, err := fxT.ToIDR(l.Amount.Mul(int64(max(0, nights-1)) * persons))
				if err == nil {
					daily += v
				}
			}
			if n := norms.AirportTransfer; n != nil {
				v, _, err := fxT.ToIDR(n.Amount.Mul(2 * persons))
				if err == nil {
					daily += v
				}
			}
			d.StayIDR[string(tier)] = stay
			d.DailyIDR[string(tier)] = daily
			total := flightTotal + stay + daily
			d.TotalIDR[string(tier)] = total
			if budgetIDR != nil {
				d.Fits[string(tier)] = total+(*budgetIDR*int64(contingencyPct)+99)/100 <= *budgetIDR
			}
			d.PriceCache = append(d.PriceCache, st.PriceCacheID.String())
		}
		if len(d.TotalIDR) > 0 {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TotalIDR["hemat"] < out[j].TotalIDR["hemat"] })
	if len(out) == 0 {
		return nil, ErrNoData
	}
	return out, nil
}

// Refresh samples suppliers for every origin × destination × month inside
// the sales window and upserts min/median rows. Sampling: three departure
// dates per month, five nights, one adult. Cost control lives in the caller's
// schedule (worker job, weekly by default).
func (x *Index) Refresh(ctx context.Context, origins []string, months int) (int, error) {
	today := domain.DateOf(x.Clock.Now())
	written := 0
	for i := 0; i < months; i++ {
		month := domain.Date{Year: today.Year, Month: today.Month, Day: 1}.AddDays(0)
		month = domain.DateOf(month.Time().AddDate(0, i, 0))
		samples := []domain.Date{{Year: month.Year, Month: month.Month, Day: 5}, {Year: month.Year, Month: month.Month, Day: 15}, {Year: month.Year, Month: month.Month, Day: 25}}
		for _, dest := range reference.Destinations {
			// Stays: one search per sample date, bucketed by tier.
			perTier := map[domain.Tier][]int64{}
			for _, d := range samples {
				if d.Before(today.AddDays(2)) {
					continue
				}
				stays, err := x.Stays.Search(ctx, ports.StayQuery{City: dest.Code, CheckIn: d, CheckOut: d.AddDays(5), Guests: domain.Pax{Adults: 2}})
				if err != nil {
					continue
				}
				for _, s := range stays {
					idr, _, err := x.FX.Convert(ctx, s.PricePerNight)
					if err != nil {
						continue
					}
					perTier[tierOf(s)] = append(perTier[tierOf(s)], idr)
				}
			}
			for tier, vals := range perTier {
				if err := x.upsert(ctx, "stay_city_month", "", dest.Code, string(tier), month, vals, x.Stays.Name()); err == nil {
					written++
				}
			}
			for _, org := range origins {
				var vals []int64
				for _, d := range samples {
					if d.Before(today.AddDays(2)) || d.AddDays(5).After(today.AddDays(x.Window)) {
						continue
					}
					offers, err := x.Flights.Search(ctx, ports.FlightQuery{Origin: org, Destination: dest.Code, DepartDate: d, ReturnDate: d.AddDays(5), Pax: domain.Pax{Adults: 1}, MaxStops: 1})
					if err != nil {
						continue
					}
					for _, o := range offers {
						idr, _, err := x.FX.Convert(ctx, o.Price)
						if err == nil {
							vals = append(vals, idr)
						}
					}
				}
				if err := x.upsert(ctx, "flight_route_month", org, dest.Code, "", month, vals, x.Flights.Name()); err == nil {
					written++
				}
			}
		}
	}
	return written, nil
}

// tierOf buckets a hotel by stars (≤2 hemat, 3 seimbang, ≥4 nyaman).
func tierOf(s domain.StayOffer) domain.Tier {
	switch {
	case s.Stars <= 2:
		return domain.TierHemat
	case s.Stars == 3:
		return domain.TierSeimbang
	}
	return domain.TierNyaman
}

func (x *Index) upsert(ctx context.Context, kind, origin, dest, tier string, month domain.Date, vals []int64, source string) error {
	if len(vals) == 0 {
		return ErrNoData
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	now := x.Clock.Now()
	e := domain.Estimate{
		PriceCacheID: platform.NewID(), Kind: kind, Origin: origin, Destination: dest, Month: month, Tier: tier,
		MinIDR: vals[0], MedianIDR: vals[len(vals)/2], SampleCount: len(vals), Source: source, RefreshedAt: now,
	}
	return x.DB.Q().UpsertPriceCache(ctx, e, now.Add(x.TTL))
}
