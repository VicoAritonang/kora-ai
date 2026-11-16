package domain

import (
	"time"

	"github.com/google/uuid"
)

// OfferKind is the offers.kind enum.
type OfferKind string

// Offer kinds.
const (
	OfferFlight   OfferKind = "flight"
	OfferStay     OfferKind = "stay"
	OfferActivity OfferKind = "activity"
)

// Segment is one flight leg.
type Segment struct {
	Carrier      string    `json:"carrier"`       // IATA code
	CarrierName  string    `json:"carrier_name"`
	FlightNumber string    `json:"flight_number"`
	From         string    `json:"from"` // airport IATA
	To           string    `json:"to"`
	DepartAt     time.Time `json:"depart_at"` // with the airport's local offset
	ArriveAt     time.Time `json:"arrive_at"`
}

// BaggageInfo summarizes included baggage.
type BaggageInfo struct {
	CheckedKg int    `json:"checked_kg"`
	Cabin     bool   `json:"cabin"`
	Note      string `json:"note,omitempty"`
}

// FlightOffer is a normalized round-trip flight offer.
type FlightOffer struct {
	ID          uuid.UUID     `json:"id"`
	Supplier    string        `json:"supplier"`
	SupplierRef string        `json:"supplier_ref"`
	SearchKey   string        `json:"search_key"`
	Price       Money         `json:"price"` // total for all passengers
	PriceIDR    int64         `json:"price_idr"`
	FX          FXQuote       `json:"fx"`
	Outbound    []Segment     `json:"outbound"`
	Inbound     []Segment     `json:"inbound"`
	Stops       int           `json:"stops"` // max stops on either direction
	Duration    time.Duration `json:"duration"` // outbound + inbound travel time
	Baggage     BaggageInfo   `json:"baggage"`
	Cabin       string        `json:"cabin"`
	Pax         Pax           `json:"pax"`
	FetchedAt   time.Time     `json:"fetched_at"`
	ExpiresAt   time.Time     `json:"expires_at"`
	Deeplink    string        `json:"deeplink,omitempty"`
	Raw         []byte        `json:"-"`
}

// ArrivalDate is the local civil date the outbound lands.
func (f FlightOffer) ArrivalDate() Date {
	if len(f.Outbound) == 0 {
		return Date{}
	}
	return DateOf(f.Outbound[len(f.Outbound)-1].ArriveAt)
}

// ArrivalHour is the local hour the outbound lands.
func (f FlightOffer) ArrivalHour() int {
	if len(f.Outbound) == 0 {
		return 12
	}
	return f.Outbound[len(f.Outbound)-1].ArriveAt.Hour()
}

// DepartureDate is the local date the outbound leaves.
func (f FlightOffer) DepartureDate() Date {
	if len(f.Outbound) == 0 {
		return Date{}
	}
	return DateOf(f.Outbound[0].DepartAt)
}

// ReturnDepartureDate is the local date the inbound leaves the destination.
func (f FlightOffer) ReturnDepartureDate() Date {
	if len(f.Inbound) == 0 {
		return Date{}
	}
	return DateOf(f.Inbound[0].DepartAt)
}

// ReturnDepartureHour is the local hour the inbound leaves.
func (f FlightOffer) ReturnDepartureHour() int {
	if len(f.Inbound) == 0 {
		return 12
	}
	return f.Inbound[0].DepartAt.Hour()
}

// Carriers returns the distinct marketing carriers.
func (f FlightOffer) Carriers() []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]Segment{}, f.Outbound...), f.Inbound...) {
		if !seen[s.CarrierName] {
			seen[s.CarrierName] = true
			out = append(out, s.CarrierName)
		}
	}
	return out
}

// StayOffer is a normalized accommodation offer for the whole stay.
type StayOffer struct {
	ID                 uuid.UUID `json:"id"`
	Supplier           string    `json:"supplier"`
	SupplierRef        string    `json:"supplier_ref"`
	SearchKey          string    `json:"search_key"`
	Name               string    `json:"name"` // untrusted supplier text
	City               string    `json:"city"`
	Area               string    `json:"area"`
	Rating             float64   `json:"rating"` // guest rating out of 10
	Stars              int       `json:"stars"`
	PricePerNight      Money     `json:"price_per_night"`
	PriceTotal         Money     `json:"price_total"`
	PriceIDR           int64     `json:"price_idr"` // total, rupiah
	FX                 FXQuote   `json:"fx"`
	CheckIn            Date      `json:"check_in"`
	CheckOut           Date      `json:"check_out"`
	Guests             int       `json:"guests"`
	RoomName           string    `json:"room_name"` // untrusted
	DistanceToStationM *int      `json:"distance_to_station_m,omitempty"`
	RefundableNote     string    `json:"refundable_note,omitempty"`
	FetchedAt          time.Time `json:"fetched_at"`
	ExpiresAt          time.Time `json:"expires_at"`
	Deeplink           string    `json:"deeplink,omitempty"`
	Raw                []byte    `json:"-"`
}

// Nights returns the number of nights booked.
func (s StayOffer) Nights() int { return s.CheckIn.DaysUntil(s.CheckOut) }

// ActivityOffer is a bookable activity from a marketplace.
type ActivityOffer struct {
	ID          uuid.UUID     `json:"id"`
	Supplier    string        `json:"supplier"`
	SupplierRef string        `json:"supplier_ref"`
	Title       string        `json:"title"` // untrusted
	City        string        `json:"city"`
	Duration    time.Duration `json:"duration"`
	Price       Money         `json:"price"` // per person
	PriceIDR    int64         `json:"price_idr"`
	FX          FXQuote       `json:"fx"`
	Date        Date          `json:"date"`
	FetchedAt   time.Time     `json:"fetched_at"`
	ExpiresAt   time.Time     `json:"expires_at"`
	Deeplink    string        `json:"deeplink,omitempty"`
}

// Norm is one structured reference cost (cost_norms row).
type Norm struct {
	ID          uuid.UUID      `json:"id"`
	Destination string         `json:"destination_code"`
	Category    string         `json:"category"` // meal | local_transport_daily | transport_link | attraction | visa | other
	Tier        string         `json:"tier,omitempty"` // hemat | sedang | nyaman
	Label       string         `json:"label"`
	Unit        string         `json:"unit"` // per_meal | per_day | per_ride | per_person | per_trip
	Amount      Money          `json:"amount"`
	ChildFactor *float64       `json:"child_factor,omitempty"`
	Details     map[string]any `json:"details,omitempty"`
	SourceLabel string         `json:"source_label"`
	SourceURL   string         `json:"source_url,omitempty"`
	VerifiedAt  Date           `json:"verified_at"`
	ValidFrom   Date           `json:"valid_from,omitzero"`
	ValidTo     Date           `json:"valid_to,omitzero"`
}

// Tags returns the attraction tags stored in Details["tags"].
func (n Norm) Tags() []string {
	raw, _ := n.Details["tags"].([]any)
	var out []string
	for _, t := range raw {
		if s, ok := t.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// ValidOn reports whether the norm applies on d.
func (n Norm) ValidOn(d Date) bool {
	if !n.ValidFrom.IsZero() && d.Before(n.ValidFrom) {
		return false
	}
	if !n.ValidTo.IsZero() && d.After(n.ValidTo) {
		return false
	}
	return true
}

// Estimate is an aggregated price from the Destination Cost Index.
type Estimate struct {
	PriceCacheID uuid.UUID `json:"price_cache_id"`
	Kind         string    `json:"kind"` // flight_route_month | stay_city_month
	Origin       string    `json:"origin,omitempty"`
	Destination  string    `json:"destination"`
	Month        Date      `json:"month"`
	Tier         string    `json:"tier,omitempty"`
	MinIDR       int64     `json:"min_idr"`
	MedianIDR    int64     `json:"median_idr"`
	SampleCount  int       `json:"sample_count"`
	Source       string    `json:"source"`
	RefreshedAt  time.Time `json:"refreshed_at"`
	Stale        bool      `json:"stale"`
	Note         string    `json:"note,omitempty"`
}
