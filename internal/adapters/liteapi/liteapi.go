// Package liteapi adapts LiteAPI (Nuitée) v3 hotel rates to
// ports.StaySupplier.
//
// Status: written against the public v3.0 reference; the Fase 0 spike
// (F0-2) must confirm latency, area data and deep links for Osaka before it
// is switched on (SUPPLIER_STAYS). Hotel names and room names are untrusted
// third-party text and are labeled so downstream.
package liteapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
)

// Client is the LiteAPI adapter.
type Client struct {
	BaseURL string
	APIKey  string
	Margin  int
	HTTP    *platform.HTTPClient
	Clock   platform.Clock
	TTL     time.Duration
}

// New returns a LiteAPI client.
func New(baseURL, key string, margin int, clock platform.Clock, ttl time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: key, Margin: margin, HTTP: platform.NewHTTPClient("liteapi", 20*time.Second), Clock: clock, TTL: ttl}
}

// Name identifies the supplier.
func (c *Client) Name() string { return "liteapi" }

var cityNames = map[string]string{"OSA": "Osaka", "UKY": "Kyoto", "TYO": "Tokyo"}

type wireResponse struct {
	Data []struct {
		HotelID   string `json:"hotelId"`
		RoomTypes []struct {
			OfferID string `json:"offerId"`
			Rates   []struct {
				Name       string `json:"name"`
				RetailRate struct {
					Total []struct {
						Amount   float64 `json:"amount"`
						Currency string  `json:"currency"`
					} `json:"total"`
				} `json:"retailRate"`
				CancellationPolicies struct {
					RefundableTag string `json:"refundableTag"`
				} `json:"cancellationPolicies"`
			} `json:"rates"`
		} `json:"roomTypes"`
	} `json:"data"`
	Hotels []struct {
		ID      string  `json:"id"`
		Name    string  `json:"name"`
		Address string  `json:"address"`
		Rating  float64 `json:"rating"`
		Stars   float64 `json:"stars"`
	} `json:"hotels"`
}

func (c *Client) rates(ctx context.Context, body map[string]any) (wireResponse, error) {
	var r wireResponse
	b, _ := json.Marshal(body)
	out, _, err := c.HTTP.Do(ctx, "POST", c.BaseURL+"/hotels/rates", b, map[string]string{"X-API-Key": c.APIKey, "Content-Type": "application/json", "Accept": "application/json"})
	if err != nil {
		return r, mapErr(err)
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return r, fmt.Errorf("liteapi: decode: %w", err)
	}
	return r, nil
}

// occupancies splits the party into rooms of about two, every room with at
// least one adult and the party size preserved exactly. Child ages are an
// assumption (8) until the trip state carries them.
func occupancies(p domain.Pax) []map[string]any {
	rooms := min((p.Total()+1)/2, p.Adults)
	if rooms < 1 {
		rooms = 1
	}
	adults := make([]int, rooms)
	kids := make([][]int, rooms)
	for i := 0; i < p.Adults; i++ {
		adults[i%rooms]++
	}
	for i := 0; i < p.Children; i++ {
		kids[i%rooms] = append(kids[i%rooms], 8)
	}
	occ := make([]map[string]any, rooms)
	for i := range occ {
		occ[i] = map[string]any{"adults": adults[i], "children": kids[i]}
	}
	return occ
}

// Search returns the cheapest rate per hotel for the city and dates.
func (c *Client) Search(ctx context.Context, q ports.StayQuery) ([]domain.StayOffer, error) {
	city, ok := cityNames[strings.ToUpper(q.City)]
	if !ok {
		return nil, fmt.Errorf("%w: city %s", ports.ErrInvalidQuery, q.City)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 40
	}
	body := map[string]any{
		"checkin": q.CheckIn.String(), "checkout": q.CheckOut.String(), "currency": "JPY", "guestNationality": "ID",
		"occupancies": occupancies(q.Guests), "cityName": city, "countryCode": "JP", "limit": limit, "timeout": 12,
	}
	if c.Margin > 0 {
		body["margin"] = c.Margin
	}
	if q.MinRating > 0 {
		body["minRating"] = q.MinRating
	}
	r, err := c.rates(ctx, body)
	if err != nil {
		return nil, err
	}
	return c.mapResponse(r, q), nil
}

// Refresh re-prices one hotel for the same dates.
func (c *Client) Refresh(ctx context.Context, o domain.StayOffer) (domain.StayOffer, error) {
	hotelID, _, _ := strings.Cut(o.SupplierRef, "|")
	r, err := c.rates(ctx, map[string]any{
		"hotelIds": []string{hotelID}, "checkin": o.CheckIn.String(), "checkout": o.CheckOut.String(), "currency": "JPY",
		"guestNationality": "ID", "occupancies": occupancies(domain.Pax{Adults: o.Guests}), "timeout": 12,
	})
	if err != nil {
		return o, err
	}
	offers := c.mapResponse(r, ports.StayQuery{City: o.City, CheckIn: o.CheckIn, CheckOut: o.CheckOut, Guests: domain.Pax{Adults: o.Guests}})
	if len(offers) == 0 {
		return o, fmt.Errorf("%w: hotel %s no longer has rooms", ports.ErrNotFound, hotelID)
	}
	n := offers[0]
	n.SearchKey = o.SearchKey
	return n, nil
}

func (c *Client) mapResponse(r wireResponse, q ports.StayQuery) []domain.StayOffer {
	info := map[string]int{}
	for i, h := range r.Hotels {
		info[h.ID] = i
	}
	now := c.Clock.Now()
	nights := q.CheckIn.DaysUntil(q.CheckOut)
	var out []domain.StayOffer
	for _, d := range r.Data {
		best := math.MaxFloat64
		var cur, room, offerID, refundable string
		for _, rt := range d.RoomTypes {
			for _, rate := range rt.Rates {
				if len(rate.RetailRate.Total) == 0 {
					continue
				}
				t := rate.RetailRate.Total[0]
				if t.Amount < best {
					best, cur, room, offerID, refundable = t.Amount, t.Currency, rate.Name, rt.OfferID, rate.CancellationPolicies.RefundableTag
				}
			}
		}
		if best == math.MaxFloat64 || nights <= 0 {
			continue
		}
		exp := domain.MinorUnits(cur)
		total := int64(math.Ceil(best * math.Pow10(exp)))
		o := domain.StayOffer{
			ID: platform.NewID(), Supplier: "liteapi", SupplierRef: d.HotelID + "|" + offerID,
			City: strings.ToUpper(q.City), CheckIn: q.CheckIn, CheckOut: q.CheckOut, Guests: q.Guests.Total(),
			PriceTotal: domain.Money{Amount: total, Currency: cur}, PricePerNight: domain.Money{Amount: (total + int64(nights) - 1) / int64(nights), Currency: cur},
			RoomName: room, FetchedAt: now, ExpiresAt: now.Add(c.TTL),
		}
		if refundable == "RFN" {
			o.RefundableNote = "Dapat dibatalkan (cek syarat penjual)"
		} else {
			o.RefundableNote = "Tidak dapat dikembalikan"
		}
		if i, ok := info[d.HotelID]; ok {
			h := r.Hotels[i]
			o.Name, o.Rating, o.Stars = h.Name, h.Rating, int(h.Stars)
			o.Area = areaFrom(h.Address)
		}
		if o.Name == "" {
			o.Name = "Hotel " + d.HotelID
		}
		if q.Area != "" && !strings.EqualFold(o.Area, q.Area) {
			continue
		}
		raw, _ := json.Marshal(d)
		o.Raw = raw
		out = append(out, o)
	}
	return out
}

// areaFrom takes the ward/district from an address ("1-2-3 Namba, Chuo Ward,
// Osaka" → "Namba"). Area quality is one of the things F0-2 must measure.
func areaFrom(addr string) string {
	parts := strings.Split(addr, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || strings.ContainsAny(p[:1], "0123456789") {
			fields := strings.Fields(p)
			if len(fields) > 1 {
				return strings.Join(fields[1:], " ")
			}
			continue
		}
		return p
	}
	return ""
}

func mapErr(err error) error {
	var se *platform.StatusError
	switch {
	case errors.As(err, &se) && se.Status == 429:
		return fmt.Errorf("%w: %v", ports.ErrRateLimited, err)
	case errors.As(err, &se) && se.Status >= 400 && se.Status < 500:
		return fmt.Errorf("%w: %v", ports.ErrInvalidQuery, err)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %v", ports.ErrTimeout, err)
	}
	return fmt.Errorf("%w: %v", ports.ErrUnavailable, err)
}
