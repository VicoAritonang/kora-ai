package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Intent is what the router decided the user wants (run_mode in the DB).
type Intent string

// Intents (AI_SYSTEM_DESIGN §3.1).
const (
	IntentPlanTrip            Intent = "plan_trip"
	IntentBudgetToDestination Intent = "budget_to_destination"
	IntentRefine              Intent = "refine"
	IntentInfo                Intent = "info"
	IntentChitchat            Intent = "chitchat"
	IntentOutOfScope          Intent = "out_of_scope"
)

// Intents lists every intent in a stable order.
var Intents = []Intent{IntentPlanTrip, IntentBudgetToDestination, IntentRefine, IntentInfo, IntentChitchat, IntentOutOfScope}

// ValidIntent reports whether s names an intent.
func ValidIntent(s string) bool {
	for _, i := range Intents {
		if string(i) == s {
			return true
		}
	}
	return false
}

// Place is a normalized origin or destination. Codes come from the reference
// table, never from the model (MODULE_SPEC §5).
type Place struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	Level string `json:"level,omitempty"` // city | country
}

// Pax is the party size.
type Pax struct {
	Adults   int `json:"adults"`
	Children int `json:"children"`
}

// Total returns adults + children.
func (p Pax) Total() int { return p.Adults + p.Children }

// ErrInvalidPax is returned for party sizes outside the supported range.
var ErrInvalidPax = errors.New("invalid pax")

// Validate enforces 1–9 adults, 0–8 children, total ≤ 9 (API_SPEC §7).
func (p Pax) Validate() error {
	if p.Adults < 1 || p.Adults > 9 || p.Children < 0 || p.Children > 8 || p.Total() > 9 {
		return fmt.Errorf("%w: %d adults, %d children", ErrInvalidPax, p.Adults, p.Children)
	}
	return nil
}

// Season names.
const (
	SeasonSpring = "spring"
	SeasonSummer = "summer"
	SeasonAutumn = "autumn"
	SeasonWinter = "winter"
	SeasonYearEnd = "year_end"
)

// DateWindow is either exact dates or a fuzzy period (month or season + year).
type DateWindow struct {
	From     Date   `json:"from,omitzero"`
	To       Date   `json:"to,omitzero"`
	Season   string `json:"season,omitempty"`
	Month    int    `json:"month,omitempty"`
	Year     int    `json:"year,omitempty"`
	FlexDays int    `json:"flex_days,omitempty"`
}

// Exact reports whether the window carries a fixed departure date.
func (w DateWindow) Exact() bool { return !w.From.IsZero() }

// Range resolves the window to the inclusive span of possible departure and
// return dates. Fuzzy windows resolve to whole months or seasons.
func (w DateWindow) Range() (Date, Date, error) {
	switch {
	case !w.From.IsZero():
		to := w.To
		if to.IsZero() {
			to = w.From
		}
		return w.From, to, nil
	case w.Month >= 1 && w.Month <= 12 && w.Year > 0:
		start := Date{w.Year, time.Month(w.Month), 1}
		return start, Date{w.Year, time.Month(w.Month) + 1, 1}.normalize().AddDays(-1), nil
	case w.Season != "" && w.Year > 0:
		return SeasonRange(w.Season, w.Year)
	}
	return Date{}, Date{}, errors.New("date window is not resolvable")
}

func (d Date) normalize() Date { return DateOf(d.Time()) }

// SeasonRange returns the span of a Japanese season. Winter of year Y runs
// from December Y to February Y+1; "year_end" is mid-December to early January.
func SeasonRange(season string, year int) (Date, Date, error) {
	switch season {
	case SeasonSpring:
		return Date{year, time.March, 1}, Date{year, time.May, 31}, nil
	case SeasonSummer:
		return Date{year, time.June, 1}, Date{year, time.August, 31}, nil
	case SeasonAutumn:
		return Date{year, time.September, 1}, Date{year, time.November, 30}, nil
	case SeasonWinter:
		return Date{year, time.December, 1}, Date{year + 1, time.February, 1}.AddDays(27), nil
	case SeasonYearEnd:
		return Date{year, time.December, 15}, Date{year + 1, time.January, 5}, nil
	}
	return Date{}, Date{}, fmt.Errorf("unknown season %q", season)
}

// NextSeasonYear picks the year for a season mentioned without one ("musim
// gugur nanti"): the first occurrence that still has at least three weeks left
// after today.
func NextSeasonYear(season string, today Date) int {
	for y := today.Year - 1; y <= today.Year+2; y++ {
		_, end, err := SeasonRange(season, y)
		if err != nil {
			return today.Year
		}
		if !end.Before(today.AddDays(21)) {
			return y
		}
	}
	return today.Year + 1
}

// NextMonthYear picks the year for a month mentioned without one.
func NextMonthYear(month int, today Date) int {
	if month < int(today.Month) || (month == int(today.Month) && today.Day > 10) {
		return today.Year + 1
	}
	return today.Year
}

// Assumption is a slot the system filled, with the reason shown to the user.
type Assumption struct {
	Slot   string `json:"slot"`
	Value  any    `json:"value"`
	Reason string `json:"reason"`
	Source string `json:"source,omitempty"` // profile | user_memory | persona | default | user
}

// SpecialRequest is a constraint the plan must satisfy (festival, halal, ...).
type SpecialRequest struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// TripState is the structured conversation state and the source of truth for
// the ongoing conversation (AI_SYSTEM_DESIGN §7).
type TripState struct {
	Version           int              `json:"version"`
	Origin            *Place           `json:"origin,omitempty"`
	Destination       *Place           `json:"destination,omitempty"`
	Budget            *Money           `json:"budget,omitempty"`
	BudgetIsPerPerson bool             `json:"budget_is_per_person,omitempty"`
	DateWindow        *DateWindow      `json:"date_window,omitempty"`
	Nights            *int             `json:"nights,omitempty"`
	Pax               Pax              `json:"pax"`
	Interests         []string         `json:"interests"`
	Special           []SpecialRequest `json:"special_requests"`
	Assumptions       []Assumption     `json:"assumptions"`
	SelectedPackageID *uuid.UUID       `json:"selected_package_id,omitempty"`
	// LastTurnClarified is true when the previous assistant turn asked a
	// clarifying question; the next turn must not ask again (FR-3).
	LastTurnClarified bool `json:"last_turn_clarified,omitempty"`
}

// Normalize fills zero-value collections so JSON is stable.
func (s *TripState) Normalize() {
	if s.Interests == nil {
		s.Interests = []string{}
	}
	if s.Special == nil {
		s.Special = []SpecialRequest{}
	}
	if s.Assumptions == nil {
		s.Assumptions = []Assumption{}
	}
}

// SetAssumption records or replaces the assumption for a slot.
func (s *TripState) SetAssumption(a Assumption) {
	for i := range s.Assumptions {
		if s.Assumptions[i].Slot == a.Slot {
			s.Assumptions[i] = a
			return
		}
	}
	s.Assumptions = append(s.Assumptions, a)
}

// DropAssumption removes the assumption for a slot (the user stated it).
func (s *TripState) DropAssumption(slot string) {
	out := s.Assumptions[:0]
	for _, a := range s.Assumptions {
		if a.Slot != slot {
			out = append(out, a)
		}
	}
	s.Assumptions = out
}

// HasAssumption reports whether slot was filled by the system.
func (s *TripState) HasAssumption(slot string) bool {
	for _, a := range s.Assumptions {
		if a.Slot == slot {
			return true
		}
	}
	return false
}

// TripStatePatch carries only the slots the user mentioned this turn. Nil
// fields are untouched; the extractor never fills unmentioned slots.
type TripStatePatch struct {
	Origin            *Place           `json:"origin,omitempty"`
	Destination       *Place           `json:"destination,omitempty"`
	Budget            *Money           `json:"budget,omitempty"`
	BudgetIsPerPerson *bool            `json:"budget_is_per_person,omitempty"`
	DateWindow        *DateWindow      `json:"date_window,omitempty"`
	Nights            *int             `json:"nights,omitempty"`
	Adults            *int             `json:"adults,omitempty"`
	Children          *int             `json:"children,omitempty"`
	Interests         []string         `json:"interests,omitempty"`
	Special           []SpecialRequest `json:"special_requests,omitempty"`
	ClearDestination  bool             `json:"clear_destination,omitempty"`
}

// Empty reports whether the patch changes nothing.
func (p TripStatePatch) Empty() bool {
	return p.Origin == nil && p.Destination == nil && p.Budget == nil && p.BudgetIsPerPerson == nil &&
		p.DateWindow == nil && p.Nights == nil && p.Adults == nil && p.Children == nil &&
		len(p.Interests) == 0 && len(p.Special) == 0 && !p.ClearDestination
}

// Apply merges the patch into s. Slots the user states replace assumptions.
func (s *TripState) Apply(p TripStatePatch) error {
	if p.Origin != nil {
		s.Origin = p.Origin
		s.DropAssumption("origin")
	}
	if p.ClearDestination {
		s.Destination = nil
	}
	if p.Destination != nil {
		s.Destination = p.Destination
		s.DropAssumption("destination")
	}
	if p.Budget != nil {
		if p.Budget.Amount <= 0 || !SupportedCurrency(p.Budget.Currency) {
			return fmt.Errorf("invalid budget %v", *p.Budget)
		}
		b := *p.Budget
		s.Budget = &b
		s.DropAssumption("budget")
	}
	if p.BudgetIsPerPerson != nil {
		s.BudgetIsPerPerson = *p.BudgetIsPerPerson
	}
	if p.DateWindow != nil {
		w := *p.DateWindow
		s.DateWindow = &w
		s.DropAssumption("date_window")
	}
	if p.Nights != nil {
		if *p.Nights < 1 || *p.Nights > 60 {
			return fmt.Errorf("nights %d out of range", *p.Nights)
		}
		n := *p.Nights
		s.Nights = &n
		s.DropAssumption("nights")
	}
	if p.Adults != nil {
		s.Pax.Adults = *p.Adults
		s.DropAssumption("pax")
	}
	if p.Children != nil {
		s.Pax.Children = *p.Children
	}
	if s.Pax.Adults == 0 && (p.Adults != nil || p.Children != nil) {
		s.Pax.Adults = 1
	}
	if s.Pax.Adults > 0 {
		if err := s.Pax.Validate(); err != nil {
			return err
		}
	}
	for _, i := range p.Interests {
		if !contains(s.Interests, i) {
			s.Interests = append(s.Interests, i)
		}
	}
	for _, r := range p.Special {
		dup := false
		for _, e := range s.Special {
			if e.Kind == r.Kind && e.Text == r.Text {
				dup = true
			}
		}
		if !dup {
			s.Special = append(s.Special, r)
		}
	}
	return nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// TripRequest is the immutable, complete snapshot of slots one run uses
// (trip_requests table). Every slot is resolved, with assumptions recorded.
type TripRequest struct {
	ID          uuid.UUID        `json:"id"`
	Mode        Intent           `json:"mode"`
	Origin      Place            `json:"origin"`
	Destination *Place           `json:"destination,omitempty"`
	Budget      *Money           `json:"budget,omitempty"`
	BudgetIDR   *int64           `json:"budget_idr,omitempty"`
	Window      DateWindow       `json:"date_window"`
	WindowStart Date             `json:"window_start"`
	WindowEnd   Date             `json:"window_end"`
	DepartDate  Date             `json:"depart_date"`
	ReturnDate  Date             `json:"return_date"`
	Nights      int              `json:"nights"`
	Pax         Pax              `json:"pax"`
	Interests   []string         `json:"interests"`
	Special     []SpecialRequest `json:"special_requests"`
	Assumptions []Assumption     `json:"assumptions"`
	// LiveWindowEnd is the last date suppliers are expected to sell; dates
	// after it can only be estimated (FR-14).
	LiveWindowEnd Date `json:"live_window_end"`
}

// OutOfBookingWindow reports whether the return flight falls after the
// supplier sales window.
func (r TripRequest) OutOfBookingWindow() bool {
	return !r.LiveWindowEnd.IsZero() && r.ReturnDate.After(r.LiveWindowEnd)
}

// SuggestDeparture picks a departure inside [start,end] such that the whole
// trip is at least two weeks away and, when possible, still inside the live
// sales window. It prefers the date closest to the window's midpoint.
func SuggestDeparture(start, end Date, nights int, today, liveEnd Date) Date {
	earliest := today.AddDays(14)
	if start.Before(earliest) {
		start = earliest
	}
	if end.Before(start) {
		end = start
	}
	mid := start.AddDays(start.DaysUntil(end) / 2)
	// Latest departure whose return is still sellable.
	if !liveEnd.IsZero() {
		latestLive := liveEnd.AddDays(-nights)
		if !latestLive.Before(start) && mid.After(latestLive) {
			mid = latestLive
		}
	}
	return mid
}
