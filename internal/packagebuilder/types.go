// Package packagebuilder assembles priced trip packages from candidate
// offers and cost norms. It is a pure function (rule D3): no network, no
// system clock (Now is injected), no LLM, and it imports only domain.
package packagebuilder

import (
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/domain"
)

// CostNorms are the reference costs for one destination, already filtered to
// the trip dates (from the costnorm module).
type CostNorms struct {
	Meals           map[string]domain.Norm // norm tier → per_meal
	LocalDaily      map[string]domain.Norm // norm tier → per_day
	AirportTransfer *domain.Norm           // transport_link, per_ride, one way
	Visa            *domain.Norm           // per_person
	Attractions     map[uuid.UUID]domain.Norm
}

// PlanDay is one day of the agent's proposed itinerary.
type PlanDay struct {
	DayIndex         int         `json:"day_index"`
	Area             string      `json:"area"`
	Title            string      `json:"title"`
	Summary          string      `json:"summary"`
	NormIDs          []uuid.UUID `json:"activity_norm_ids"`
	ActivityOfferIDs []uuid.UUID `json:"activity_offer_ids"`
}

// DayPlan is the validated itinerary (propose_day_plan).
type DayPlan struct {
	ID   uuid.UUID `json:"id"`
	Days []PlanDay `json:"days"`
}

// DestinationOption is an alternative destination from the cost index.
type DestinationOption struct {
	Code     string
	Label    string
	TotalIDR int64
}

// Policy is configurable package policy (MODULE_SPEC §9 step 5).
type Policy struct {
	ContingencyPct      int
	FXBufferPct         int
	MinStayRating       float64
	MaxReasonableStops  int
	NearStationM        int
	HematMaxActivityIDR int64 // per person; hemat keeps only cheaper sights
	SeimbangMaxActivityIDR int64
	Version             string
}

// DefaultPolicy returns the documented defaults.
func DefaultPolicy() Policy {
	return Policy{
		ContingencyPct: 10, FXBufferPct: 3, MinStayRating: 7.0,
		MaxReasonableStops: 1, NearStationM: 800,
		HematMaxActivityIDR: 120_000, SeimbangMaxActivityIDR: 450_000,
		Version: "pb-1.0.0",
	}
}

// Input is everything Build needs.
type Input struct {
	Request    domain.TripRequest
	Flights    []domain.FlightOffer
	Stays      []domain.StayOffer
	Activities []domain.ActivityOffer
	DayPlan    *DayPlan
	Norms      CostNorms
	FX         domain.FXTable
	Policy     Policy
	Now        time.Time

	// FlightEstimate prices the flight when no live offer exists (supplier
	// down, or dates outside the sales window). Per adult, round trip.
	FlightEstimate *domain.Estimate
	// StayEstimates price accommodation per night per room by tier when no
	// live stay exists.
	StayEstimates map[domain.Tier]domain.Estimate
	// Destinations feeds the "other destination" alternative (Phase 2).
	Destinations []DestinationOption
	// NewID generates item ids; nil means deterministic ids derived from the
	// request id, which keeps golden tests stable.
	NewID func() uuid.UUID
	// DestinationLabel is the display name used in titles.
	DestinationLabel string
}

// Rejection records why a candidate was not used.
type Rejection struct {
	Kind    string    `json:"kind"` // flight | stay | activity | norm | combo
	OfferID uuid.UUID `json:"offer_id,omitzero"`
	Reason  string    `json:"reason"`
}

// Output is the result of Build.
type Output struct {
	Packages []domain.Package `json:"packages"`
	Rejected []Rejection      `json:"rejected"`
	Gaps     []string         `json:"gaps"` // norms that were missing
}
