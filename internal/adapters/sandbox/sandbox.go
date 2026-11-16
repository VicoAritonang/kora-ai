// Package sandbox is a deterministic stand-in for the flight and stay
// suppliers, used until Duffel and LiteAPI credentials exist (Fase 0 spike).
// Every offer carries supplier "sandbox" and the UI labels it "data simulasi,
// bukan harga nyata". Prices follow plausible route, season and hotel-tier
// shapes so the package builder, invariants and UI can be exercised end to
// end; they are not market prices.
package sandbox

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"time"


	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
)

var (
	wib = time.FixedZone("WIB", 7*3600)
	wita = time.FixedZone("WITA", 8*3600)
	jst = time.FixedZone("JST", 9*3600)
)

// Suppliers implements FlightSupplier and StaySupplier.
type Suppliers struct {
	Clock         platform.Clock
	BookingWindow int // days ahead the "airlines" sell
	FlightTTL     time.Duration
	StayTTL       time.Duration
	// Latency simulates supplier round trips (0 in tests).
	Latency time.Duration
}

// New returns sandbox suppliers.
func New(clock platform.Clock, bookingWindowDays int, flightTTL, stayTTL time.Duration) *Suppliers {
	return &Suppliers{Clock: clock, BookingWindow: bookingWindowDays, FlightTTL: flightTTL, StayTTL: stayTTL, Latency: 400 * time.Millisecond}
}

// Name identifies the supplier.
func (s *Suppliers) Name() string { return "sandbox" }

type airport struct {
	code string
	tz   *time.Location
}

var originAirports = map[string]airport{"JKT": {"CGK", wib}, "CGK": {"CGK", wib}, "SUB": {"SUB", wib}, "DPS": {"DPS", wita}}
var destAirports = map[string]airport{"OSA": {"KIX", jst}, "KIX": {"KIX", jst}, "UKY": {"KIX", jst}, "TYO": {"NRT", jst}, "NRT": {"NRT", jst}, "HND": {"HND", jst}}

type carrier struct {
	code, name string
	via        string // "" = direct
	base       int64  // IDR, round trip, one adult, JKT→KIX, off-peak
	currency   string // quote currency of the "supplier"
	checkedKg  int
	outDepHour int // local departure hour, outbound
	retDepHour int // local departure hour, inbound
	outMins    int // total outbound travel minutes
	retMins    int
}

var carriers = []carrier{
	{"GA", "Garuda Indonesia", "", 7_650_000, "IDR", 30, 22, 11, 445, 440},
	{"JL", "Japan Airlines", "", 9_350_000, "USD", 46, 21, 10, 430, 425},
	{"SQ", "Singapore Airlines", "SIN", 8_200_000, "USD", 30, 6, 16, 760, 810},
	{"CX", "Cathay Pacific", "HKG", 7_100_000, "USD", 23, 7, 14, 835, 870},
	{"TR", "Scoot", "SIN", 4_550_000, "IDR", 0, 1, 15, 790, 830},
	{"D7", "AirAsia X", "KUL", 4_850_000, "IDR", 0, 8, 13, 900, 880},
	{"NH", "ANA", "", 9_900_000, "USD", 46, 22, 11, 450, 440},
}

// approximate FX used only to quote "supplier currency" amounts; the app
// converts back with real rates.
var quoteRate = map[string]int64{"IDR": 1, "USD": 16_400}

func hash(parts ...string) uint64 {
	h := sha1.Sum([]byte(strings.Join(parts, "|")))
	return binary.BigEndian.Uint64(h[:8])
}

// jitter returns a factor in [1-pct, 1+pct] as per-mille.
func jitter(h uint64, pct int64) int64 { return 1000 - pct*10 + int64(h%uint64(pct*20+1)) }

func seasonFactor(m time.Month, dest string) int64 {
	switch {
	case m == time.March || m == time.April:
		return 1250 // sakura
	case m == time.December || m == time.January:
		return 1300
	case m == time.October || m == time.November:
		if dest == "UKY" {
			return 1200
		}
		return 1150
	case m == time.July || m == time.August:
		return 1150
	}
	return 1000
}

func (s *Suppliers) sleep(ctx context.Context) {
	if s.Latency <= 0 {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(s.Latency):
	}
}

// Search returns 5–7 round-trip offers. Dates beyond the sales window return
// ports.ErrOutOfBookingWindow, as a real supplier would return nothing.
func (s *Suppliers) Search(ctx context.Context, q ports.FlightQuery) ([]domain.FlightOffer, error) {
	s.sleep(ctx)
	now := s.Clock.Now()
	today := domain.DateOf(now)
	if q.ReturnDate.After(today.AddDays(s.BookingWindow)) {
		return nil, fmt.Errorf("%w: return %s after %s", ports.ErrOutOfBookingWindow, q.ReturnDate, today.AddDays(s.BookingWindow))
	}
	if q.DepartDate.Before(today.AddDays(1)) || !q.ReturnDate.After(q.DepartDate) {
		return nil, fmt.Errorf("%w: dates", ports.ErrInvalidQuery)
	}
	org, ok1 := originAirports[q.Origin]
	dst, ok2 := destAirports[q.Destination]
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("%w: route %s-%s not served", ports.ErrInvalidQuery, q.Origin, q.Destination)
	}
	var out []domain.FlightOffer
	for _, c := range carriers {
		if q.MaxStops == 0 && c.via != "" {
			continue
		}
		if dst.code == "NRT" && c.code == "D7" {
			continue
		}
		h := hash(c.code, q.Origin, q.Destination, q.DepartDate.String(), q.ReturnDate.String())
		if h%7 == 0 {
			continue // sold out on this date
		}
		price := c.base * seasonFactor(q.DepartDate.Month, q.Destination) / 1000 * jitter(h>>8, 9) / 1000
		if org.code != "CGK" {
			price = price * 110 / 100
		}
		if dst.code == "NRT" {
			price = price * 105 / 100
		}
		// Prices fall slightly for bookings further ahead, rise close in.
		lead := today.DaysUntil(q.DepartDate)
		if lead < 21 {
			price = price * 125 / 100
		}
		cabinMult := int64(1)
		if q.Cabin == "business" {
			cabinMult = 3
		}
		perPaxIDR := price * cabinMult
		total := perPaxIDR*int64(q.Pax.Adults) + perPaxIDR*75/100*int64(q.Pax.Children)
		amount := domain.Money{Amount: total, Currency: "IDR"}
		if c.currency == "USD" {
			cents := total * 100 / quoteRate["USD"]
			amount = domain.Money{Amount: cents, Currency: "USD"}
		}
		outDep := time.Date(q.DepartDate.Year, q.DepartDate.Month, q.DepartDate.Day, c.outDepHour, int(h%4)*15, 0, 0, org.tz)
		outArr := outDep.Add(time.Duration(c.outMins) * time.Minute).In(dst.tz)
		retDep := time.Date(q.ReturnDate.Year, q.ReturnDate.Month, q.ReturnDate.Day, c.retDepHour, int((h>>4)%4)*15, 0, 0, dst.tz)
		retArr := retDep.Add(time.Duration(c.retMins) * time.Minute).In(org.tz)
		seg := func(from, to string, dep, arr time.Time, n int) domain.Segment {
			return domain.Segment{Carrier: c.code, CarrierName: c.name, FlightNumber: fmt.Sprintf("%s%d", c.code, 100+int(h%700)+n), From: from, To: to, DepartAt: dep, ArriveAt: arr}
		}
		var outSegs, inSegs []domain.Segment
		stops := 0
		if c.via == "" {
			outSegs = []domain.Segment{seg(org.code, dst.code, outDep, outArr, 0)}
			inSegs = []domain.Segment{seg(dst.code, org.code, retDep, retArr, 1)}
		} else {
			stops = 1
			mid1 := outDep.Add(time.Duration(c.outMins/3) * time.Minute)
			mid2 := mid1.Add(time.Duration(c.outMins/4) * time.Minute)
			outSegs = []domain.Segment{seg(org.code, c.via, outDep, mid1, 0), seg(c.via, dst.code, mid2, outArr, 2)}
			rmid1 := retDep.Add(time.Duration(c.retMins/2) * time.Minute)
			rmid2 := rmid1.Add(time.Duration(c.retMins/5) * time.Minute)
			inSegs = []domain.Segment{seg(dst.code, c.via, retDep, rmid1, 1), seg(c.via, org.code, rmid2, retArr, 3)}
		}
		ref := fmt.Sprintf("sbx_off_%x", h)
		bag := domain.BaggageInfo{CheckedKg: c.checkedKg, Cabin: true}
		if c.checkedKg == 0 {
			bag.Note = "Tarif tanpa bagasi tercatat"
		}
		out = append(out, domain.FlightOffer{
			ID: platform.NewID(), Supplier: "sandbox", SupplierRef: ref,
			Price: amount, Outbound: outSegs, Inbound: inSegs, Stops: stops,
			Duration: time.Duration(c.outMins+c.retMins) * time.Minute, Baggage: bag, Cabin: cabinOr(q.Cabin),
			Pax: q.Pax, FetchedAt: now, ExpiresAt: now.Add(s.FlightTTL),
			Raw: []byte(fmt.Sprintf(`{"sandbox":true,"ref":%q,"carrier":%q}`, ref, c.code)),
		})
	}
	if q.MaxStops > 0 {
		filtered := out[:0]
		for _, o := range out {
			if o.Stops <= q.MaxStops {
				filtered = append(filtered, o)
			}
		}
		out = filtered
	}
	return out, nil
}

func cabinOr(c string) string {
	if c == "" {
		return "economy"
	}
	return c
}

// Refresh re-prices an offer: prices drift by up to ±3% per hour bucket.
func (s *Suppliers) Refresh(ctx context.Context, o domain.FlightOffer) (domain.FlightOffer, error) {
	s.sleep(ctx)
	now := s.Clock.Now()
	h := hash(o.SupplierRef, now.Format("2006010215"))
	if h%23 == 0 {
		return o, fmt.Errorf("%w: offer no longer available", ports.ErrNotFound)
	}
	o.Price.Amount = o.Price.Amount * jitter(h, 3) / 1000
	o.ID = platform.NewID()
	o.FetchedAt, o.ExpiresAt = now, now.Add(s.FlightTTL)
	return o, nil
}

// ------------------------------------------------------------------ stays --

type hotel struct {
	name, area string
	stars      int
	rating     float64
	distM      int
	nightlyJPY int64
}

var hotels = map[string][]hotel{
	"OSA": {
		{"Kora Hostel Namba", "Namba", 2, 8.3, 250, 4200},
		{"Dotonbori Riverside Inn", "Namba", 3, 8.0, 400, 8500},
		{"Umeda Business Hotel", "Umeda", 3, 8.4, 300, 10500},
		{"Shin-Osaka Station Hotel", "Shin-Osaka", 3, 7.8, 150, 9000},
		{"Tennoji Garden Hotel", "Tennoji", 3, 8.1, 500, 8000},
		{"Shinsaibashi Boutique Hotel", "Shinsaibashi", 4, 8.7, 350, 16000},
		{"Nakanoshima Riverside Hotel", "Kitahama", 4, 8.9, 600, 19000},
		{"Grand Umeda Tower Hotel", "Umeda", 5, 9.2, 200, 38000},
		{"Bay Area Family Inn", "Bay Area", 3, 7.6, 900, 7500},
		{"Shinsekai Capsule Stay", "Shinsekai", 1, 6.8, 300, 3000},
	},
	"UKY": {
		{"Kyoto Station Hostel", "Kyoto Station", 2, 8.2, 300, 4800},
		{"Gion Machiya Guesthouse", "Gion", 3, 8.8, 700, 12500},
		{"Kawaramachi City Hotel", "Kawaramachi", 3, 8.3, 250, 11000},
		{"Karasuma Business Inn", "Karasuma", 3, 8.0, 200, 9500},
		{"Higashiyama Ryokan", "Higashiyama", 4, 9.0, 900, 26000},
		{"Arashiyama Riverside Ryokan", "Arashiyama", 4, 9.1, 500, 32000},
		{"Grand Kyoto Palace Hotel", "Karasuma", 5, 9.3, 400, 45000},
		{"Fushimi Budget Stay", "Fushimi", 2, 7.4, 350, 5200},
	},
	"TYO": {
		{"Asakusa Backpackers", "Asakusa", 2, 8.4, 200, 4500},
		{"Ueno Station Hotel", "Ueno", 3, 8.1, 150, 10000},
		{"Shinjuku Business Hotel", "Shinjuku", 3, 8.2, 300, 13000},
		{"Shibuya Design Hotel", "Shibuya", 4, 8.8, 400, 21000},
		{"Ginza Boutique Hotel", "Ginza", 4, 8.9, 350, 24000},
		{"Grand Marunouchi Hotel", "Marunouchi", 5, 9.3, 200, 52000},
		{"Ikebukuro Capsule", "Ikebukuro", 1, 6.9, 250, 3500},
		{"Ryogoku Family Inn", "Ryogoku", 3, 7.9, 450, 9000},
	},
}

func hotelSeason(m time.Month, city string) int64 {
	switch {
	case m == time.March || m == time.April:
		return 1350
	case m == time.November && city == "UKY":
		return 1450
	case m == time.October || m == time.November:
		return 1200
	case m == time.December || m == time.July || m == time.August:
		return 1150
	}
	return 1000
}

// SearchStays returns hotels in the city (optionally one area) for the dates.
func (s *Suppliers) SearchStays(ctx context.Context, q ports.StayQuery) ([]domain.StayOffer, error) {
	s.sleep(ctx)
	now := s.Clock.Now()
	nights := q.CheckIn.DaysUntil(q.CheckOut)
	if nights <= 0 || nights > 30 {
		return nil, fmt.Errorf("%w: %d nights", ports.ErrInvalidQuery, nights)
	}
	if q.CheckIn.Before(domain.DateOf(now)) {
		return nil, fmt.Errorf("%w: check-in in the past", ports.ErrInvalidQuery)
	}
	list, ok := hotels[strings.ToUpper(q.City)]
	if !ok {
		return nil, fmt.Errorf("%w: city %s", ports.ErrInvalidQuery, q.City)
	}
	guests := q.Guests.Total()
	rooms := (guests + 1) / 2
	var out []domain.StayOffer
	for _, h := range list {
		if q.Area != "" && !strings.EqualFold(h.area, q.Area) {
			continue
		}
		if q.MinRating > 0 && h.rating < q.MinRating {
			continue
		}
		hh := hash(h.name, q.CheckIn.String(), q.CheckOut.String())
		if hh%9 == 0 {
			continue // fully booked
		}
		nightly := h.nightlyJPY * hotelSeason(q.CheckIn.Month, strings.ToUpper(q.City)) / 1000 * jitter(hh>>8, 8) / 1000
		nightly = nightly / 10 * 10
		total := nightly * int64(nights) * int64(rooms)
		d := h.distM
		ref := fmt.Sprintf("sbx_htl_%x", hh)
		out = append(out, domain.StayOffer{
			ID: platform.NewID(), Supplier: "sandbox", SupplierRef: ref,
			Name: h.name, City: strings.ToUpper(q.City), Area: h.area, Rating: h.rating, Stars: h.stars,
			PricePerNight: domain.Money{Amount: nightly * int64(rooms), Currency: "JPY"},
			PriceTotal:    domain.Money{Amount: total, Currency: "JPY"},
			CheckIn:       q.CheckIn, CheckOut: q.CheckOut, Guests: guests,
			RoomName:           roomName(guests, rooms),
			DistanceToStationM: &d,
			RefundableNote:     "Kebijakan pembatalan mengikuti penjual",
			FetchedAt:          now, ExpiresAt: now.Add(s.StayTTL),
			Raw:                []byte(fmt.Sprintf(`{"sandbox":true,"ref":%q}`, ref)),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PriceTotal.Amount < out[j].PriceTotal.Amount })
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func roomName(guests, rooms int) string {
	if rooms > 1 {
		return fmt.Sprintf("%d kamar Twin", rooms)
	}
	if guests == 1 {
		return "Kamar Single"
	}
	return "Kamar Double"
}

// RefreshStay re-prices a stay offer.
func (s *Suppliers) RefreshStay(ctx context.Context, o domain.StayOffer) (domain.StayOffer, error) {
	s.sleep(ctx)
	now := s.Clock.Now()
	h := hash(o.SupplierRef, now.Format("2006010215"))
	if h%29 == 0 {
		return o, fmt.Errorf("%w: room no longer available", ports.ErrNotFound)
	}
	f := jitter(h, 3)
	o.PriceTotal.Amount = o.PriceTotal.Amount * f / 1000
	o.PricePerNight.Amount = o.PricePerNight.Amount * f / 1000
	o.ID = platform.NewID()
	o.FetchedAt, o.ExpiresAt = now, now.Add(s.StayTTL)
	return o, nil
}

// Stays adapts the sandbox to ports.StaySupplier (method names differ from
// the flight side on the shared struct).
type Stays struct{ S *Suppliers }

// Name identifies the supplier.
func (s Stays) Name() string { return "sandbox" }

// Search searches stays.
func (s Stays) Search(ctx context.Context, q ports.StayQuery) ([]domain.StayOffer, error) {
	return s.S.SearchStays(ctx, q)
}

// Refresh refreshes a stay.
func (s Stays) Refresh(ctx context.Context, o domain.StayOffer) (domain.StayOffer, error) {
	return s.S.RefreshStay(ctx, o)
}

var _ ports.FlightSupplier = (*Suppliers)(nil)
var _ ports.StaySupplier = Stays{}
