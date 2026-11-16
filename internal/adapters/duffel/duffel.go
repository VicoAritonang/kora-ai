// Package duffel adapts the Duffel Flights API (v2) to ports.FlightSupplier.
//
// Status: written against Duffel's public API reference; the Fase 0 spike
// (F0-1) must confirm LCC coverage, IDR pricing and the out-of-window
// behaviour on real routes before this is switched on (SUPPLIER_FLIGHTS).
package duffel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
)

// Client is the Duffel adapter.
type Client struct {
	BaseURL       string
	Token         string
	HTTP          *platform.HTTPClient
	Clock         platform.Clock
	BookingWindow int
	TTL           time.Duration
}

// New returns a Duffel client.
func New(baseURL, token string, clock platform.Clock, bookingWindowDays int, ttl time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: platform.NewHTTPClient("duffel", 25*time.Second), Clock: clock, BookingWindow: bookingWindowDays, TTL: ttl}
}

// Name identifies the supplier.
func (c *Client) Name() string { return "duffel" }

// cityCode maps Kora destination codes to IATA city codes Duffel accepts.
func cityCode(code string) string {
	switch code {
	case "UKY": // Kyoto has no airport; Kansai airports serve it.
		return "OSA"
	}
	return code
}

func (c *Client) headers() map[string]string {
	return map[string]string{"Authorization": "Bearer " + c.Token, "Duffel-Version": "v2", "Accept": "application/json", "Content-Type": "application/json"}
}

type place struct {
	IATACode string `json:"iata_code"`
	TimeZone string `json:"time_zone"`
}

type wireOffer struct {
	ID            string `json:"id"`
	TotalAmount   string `json:"total_amount"`
	TotalCurrency string `json:"total_currency"`
	ExpiresAt     string `json:"expires_at"`
	Slices        []struct {
		Duration string `json:"duration"`
		Segments []struct {
			DepartingAt      string `json:"departing_at"`
			ArrivingAt       string `json:"arriving_at"`
			Origin           place  `json:"origin"`
			Destination      place  `json:"destination"`
			MarketingCarrier struct {
				IATACode string `json:"iata_code"`
				Name     string `json:"name"`
			} `json:"marketing_carrier"`
			FlightNumber string `json:"marketing_carrier_flight_number"`
			Passengers   []struct {
				Baggages []struct {
					Type     string `json:"type"`
					Quantity int    `json:"quantity"`
				} `json:"baggages"`
				CabinClass string `json:"cabin_class"`
			} `json:"passengers"`
		} `json:"segments"`
	} `json:"slices"`
}

// Search creates an offer request and maps the returned offers.
func (c *Client) Search(ctx context.Context, q ports.FlightQuery) ([]domain.FlightOffer, error) {
	type slice struct {
		Origin        string `json:"origin"`
		Destination   string `json:"destination"`
		DepartureDate string `json:"departure_date"`
	}
	var pax []map[string]any
	for i := 0; i < q.Pax.Adults; i++ {
		pax = append(pax, map[string]any{"type": "adult"})
	}
	for i := 0; i < q.Pax.Children; i++ {
		pax = append(pax, map[string]any{"age": 8}) // age assumption; the trip state does not carry ages yet
	}
	data := map[string]any{
		"slices": []slice{
			{cityCode(q.Origin), cityCode(q.Destination), q.DepartDate.String()},
			{cityCode(q.Destination), cityCode(q.Origin), q.ReturnDate.String()},
		},
		"passengers":  pax,
		"cabin_class": cabinOr(q.Cabin),
	}
	if q.MaxStops >= 0 {
		data["max_connections"] = q.MaxStops
	}
	body, _ := json.Marshal(map[string]any{"data": data})
	out, _, err := c.HTTP.Do(ctx, "POST", c.BaseURL+"/air/offer_requests?return_offers=true&supplier_timeout=20000", body, c.headers())
	if err != nil {
		return nil, mapErr(err)
	}
	var r struct {
		Data struct {
			Offers []wireOffer `json:"offers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, fmt.Errorf("duffel: decode: %w", err)
	}
	now := c.Clock.Now()
	if len(r.Data.Offers) == 0 && q.ReturnDate.After(domain.DateOf(now).AddDays(c.BookingWindow)) {
		return nil, fmt.Errorf("%w: no offers for %s", ports.ErrOutOfBookingWindow, q.ReturnDate)
	}
	var offers []domain.FlightOffer
	for _, w := range r.Data.Offers {
		o, err := c.mapOffer(w, q.Pax, now)
		if err != nil {
			continue // one malformed offer must not fail the search
		}
		offers = append(offers, o)
	}
	return offers, nil
}

// Refresh re-reads an offer by id.
func (c *Client) Refresh(ctx context.Context, o domain.FlightOffer) (domain.FlightOffer, error) {
	out, _, err := c.HTTP.Do(ctx, "GET", c.BaseURL+"/air/offers/"+o.SupplierRef+"?return_available_services=false", nil, c.headers())
	if err != nil {
		return o, mapErr(err)
	}
	var r struct {
		Data wireOffer `json:"data"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return o, fmt.Errorf("duffel: decode: %w", err)
	}
	n, err := c.mapOffer(r.Data, o.Pax, c.Clock.Now())
	if err != nil {
		return o, err
	}
	n.SearchKey = o.SearchKey
	return n, nil
}

func (c *Client) mapOffer(w wireOffer, pax domain.Pax, now time.Time) (domain.FlightOffer, error) {
	if len(w.Slices) != 2 {
		return domain.FlightOffer{}, errors.New("not a round trip")
	}
	amount, err := ParseAmount(w.TotalAmount, w.TotalCurrency)
	if err != nil {
		return domain.FlightOffer{}, err
	}
	o := domain.FlightOffer{
		ID: platform.NewID(), Supplier: "duffel", SupplierRef: w.ID, Price: amount, Pax: pax,
		FetchedAt: now, ExpiresAt: now.Add(c.TTL), Cabin: "economy",
	}
	if t, err := time.Parse(time.RFC3339, w.ExpiresAt); err == nil && t.Before(o.ExpiresAt) {
		o.ExpiresAt = t
	}
	for i, s := range w.Slices {
		var segs []domain.Segment
		for _, sg := range s.Segments {
			dep, err1 := localTime(sg.DepartingAt, sg.Origin.TimeZone)
			arr, err2 := localTime(sg.ArrivingAt, sg.Destination.TimeZone)
			if err1 != nil || err2 != nil {
				return domain.FlightOffer{}, errors.New("bad segment time")
			}
			segs = append(segs, domain.Segment{Carrier: sg.MarketingCarrier.IATACode, CarrierName: sg.MarketingCarrier.Name,
				FlightNumber: sg.MarketingCarrier.IATACode + sg.FlightNumber, From: sg.Origin.IATACode, To: sg.Destination.IATACode, DepartAt: dep, ArriveAt: arr})
			if len(sg.Passengers) > 0 {
				if sg.Passengers[0].CabinClass != "" {
					o.Cabin = sg.Passengers[0].CabinClass
				}
				for _, b := range sg.Passengers[0].Baggages {
					if b.Type == "checked" && b.Quantity > 0 {
						o.Baggage.CheckedKg = 23 * b.Quantity // Duffel reports pieces; weight per piece varies by airline
						o.Baggage.Note = fmt.Sprintf("%d koper tercatat", b.Quantity)
					}
				}
			}
		}
		if len(segs) == 0 {
			return domain.FlightOffer{}, errors.New("empty slice")
		}
		if len(segs)-1 > o.Stops {
			o.Stops = len(segs) - 1
		}
		o.Duration += segs[len(segs)-1].ArriveAt.Sub(segs[0].DepartAt)
		if i == 0 {
			o.Outbound = segs
		} else {
			o.Inbound = segs
		}
	}
	o.Baggage.Cabin = true
	o.Raw, _ = json.Marshal(w)
	return o, nil
}

func localTime(s, tz string) (time.Time, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil || tz == "" {
		loc = time.UTC
	}
	return time.ParseInLocation("2006-01-02T15:04:05", s, loc)
}

// ParseAmount converts a decimal string ("1234.50") into minor units.
func ParseAmount(s, currency string) (domain.Money, error) {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(s))
	if !ok || r.Sign() < 0 {
		return domain.Money{}, fmt.Errorf("bad amount %q", s)
	}
	exp := domain.MinorUnits(currency)
	r.Mul(r, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exp)), nil)))
	n := new(big.Int).Quo(r.Num(), r.Denom())
	if new(big.Rat).SetInt(n).Cmp(r) < 0 {
		n.Add(n, big.NewInt(1)) // round up
	}
	return domain.Money{Amount: n.Int64(), Currency: strings.ToUpper(currency)}, nil
}

func cabinOr(c string) string {
	if c == "" {
		return "economy"
	}
	return c
}

func mapErr(err error) error {
	var se *platform.StatusError
	switch {
	case errors.As(err, &se) && se.Status == 429:
		return fmt.Errorf("%w: %v", ports.ErrRateLimited, err)
	case errors.As(err, &se) && (se.Status == 400 || se.Status == 422):
		return fmt.Errorf("%w: %v", ports.ErrInvalidQuery, err)
	case errors.As(err, &se) && se.Status == 404:
		return fmt.Errorf("%w: %v", ports.ErrNotFound, err)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %v", ports.ErrTimeout, err)
	}
	return fmt.Errorf("%w: %v", ports.ErrUnavailable, err)
}
