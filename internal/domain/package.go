package domain

import (
	"time"

	"github.com/google/uuid"
)

// Tier is the package_tier enum.
type Tier string

// Tiers in display order.
const (
	TierHemat    Tier = "hemat"
	TierSeimbang Tier = "seimbang"
	TierNyaman   Tier = "nyaman"
)

// Tiers lists all tiers in order.
var Tiers = []Tier{TierHemat, TierSeimbang, TierNyaman}

// NormTier maps a package tier to the cost-norm tier it uses for meals and
// local transport.
func (t Tier) NormTier() string {
	switch t {
	case TierHemat:
		return "hemat"
	case TierNyaman:
		return "nyaman"
	}
	return "sedang"
}

// Label returns the Indonesian display name.
func (t Tier) Label() string {
	switch t {
	case TierHemat:
		return "Hemat"
	case TierNyaman:
		return "Nyaman"
	}
	return "Seimbang"
}

// Feasibility is the feasibility enum.
type Feasibility string

// Feasibility values (MODULE_SPEC §9 step 7). Unknown is used when no budget
// was given; it is stored as NULL-equivalent "fits" never silently.
const (
	FeasibilityFits       Feasibility = "fits"
	FeasibilityTight      Feasibility = "tight"
	FeasibilityOverBudget Feasibility = "over_budget"
	FeasibilityUnbounded  Feasibility = "unbounded" // no budget: not computed
)

// PackageStatus is the package_status enum.
type PackageStatus string

// Package statuses.
const (
	PackageProposed PackageStatus = "proposed"
	PackageSelected PackageStatus = "selected"
	PackageStale    PackageStatus = "stale"
)

// PriceKind tells where a number came from (goal G2).
type PriceKind string

// Price kinds.
const (
	PriceLive     PriceKind = "live"
	PriceEstimate PriceKind = "estimate"
)

// ItemCategory is the item_category enum.
type ItemCategory string

// Item categories.
const (
	CatFlight         ItemCategory = "flight"
	CatStay           ItemCategory = "stay"
	CatActivity       ItemCategory = "activity"
	CatMeal           ItemCategory = "meal"
	CatLocalTransport ItemCategory = "local_transport"
	CatVisa           ItemCategory = "visa"
	CatInsurance      ItemCategory = "insurance"
	CatOther          ItemCategory = "other"
)

// PackageItem is one priced line. Exactly one provenance reference is set:
// OfferID for live items; NormID, PriceCacheID or SourceURL for estimates.
type PackageItem struct {
	ID           uuid.UUID      `json:"id"`
	DayIndex     *int           `json:"day_index"`
	SortOrder    int            `json:"sort_order"`
	Category     ItemCategory   `json:"category"`
	Title        string         `json:"title"`
	Quantity     int            `json:"quantity"`
	UnitAmount   int64          `json:"unit_amount"` // in Currency minor units
	Currency     string         `json:"currency"`
	AmountIDR    int64          `json:"amount_idr"`
	FX           FXQuote        `json:"fx"`
	PriceKind    PriceKind      `json:"price_kind"`
	OfferID      *uuid.UUID     `json:"offer_id,omitempty"`
	NormID       *uuid.UUID     `json:"norm_id,omitempty"`
	PriceCacheID *uuid.UUID     `json:"price_cache_id,omitempty"`
	SourceLabel  string         `json:"source_label"`
	SourceURL    string         `json:"source_url,omitempty"`
	PricedAt     time.Time      `json:"priced_at"`
	ExpiresAt    *time.Time     `json:"expires_at,omitempty"`
	Details      map[string]any `json:"details"`
}

// PackageDay is one day of the itinerary.
type PackageDay struct {
	DayIndex int    `json:"day_index"`
	Date     Date   `json:"date"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Area     string `json:"area,omitempty"`
}

// Alternative is one ordered suggestion when the budget does not fit (FR-10).
type Alternative struct {
	Kind      string `json:"kind"` // shift_dates | fewer_nights | cheaper_area | other_destination | add_funds
	Label     string `json:"label"`
	SavingIDR int64  `json:"saving_idr,omitempty"`
	Value     any    `json:"value,omitempty"`
}

// Package is one complete, priced trip.
type Package struct {
	ID             uuid.UUID     `json:"id"`
	Tier           Tier          `json:"tier"`
	Title          string        `json:"title"`
	Status         PackageStatus `json:"status"`
	Feasibility    Feasibility   `json:"feasibility"`
	BudgetIDR      *int64        `json:"budget_idr,omitempty"`
	TotalIDR       int64         `json:"total_idr"`
	LiveIDR        int64         `json:"live_idr"`
	EstimateIDR    int64         `json:"estimate_idr"`
	BufferIDR      int64         `json:"buffer_idr"`
	ShortfallIDR   int64         `json:"shortfall_idr"`
	Assumptions    []Assumption  `json:"assumptions"`
	Alternatives   []Alternative `json:"alternatives"`
	Days           []PackageDay  `json:"days"`
	Items          []PackageItem `json:"items"`
	BuilderVersion string        `json:"builder_version"`
	// Combination identifies the candidates the package was built from, so
	// re-pricing can rebuild the same combination.
	FlightOfferID *uuid.UUID `json:"flight_offer_id,omitempty"`
	StayOfferID   *uuid.UUID `json:"stay_offer_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// RemainingIDR returns budget − total − buffer, floored at zero.
func (p Package) RemainingIDR() int64 {
	if p.BudgetIDR == nil {
		return 0
	}
	r := *p.BudgetIDR - p.TotalIDR - p.BufferIDR
	if r < 0 {
		return 0
	}
	return r
}
