package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/reference"
)

func jakarta() *time.Location { return platform.Jakarta }

// DefaultNights is the stated default trip length when the user gives none.
const DefaultNights = 5

// clarify applies FR-3: required is budget OR destination; everything else
// is assumed, recorded and shown. It never asks twice in a row. It returns
// true when the run should end with a question.
func (r *Runner) clarify(ctx context.Context, st *runState) (bool, error) {
	s := &st.state
	asked := false
	switch {
	case s.Destination == nil && s.Budget == nil:
		if !s.LastTurnClarified {
			asked = true
			break
		}
		// Second turn without either: assume the flagship destination.
		osa, _ := reference.DestinationCity("OSA")
		s.Destination = &domain.Place{Code: osa.Code, Label: osa.Label, Level: "city"}
		s.SetAssumption(domain.Assumption{Slot: "destination", Value: osa.Label, Reason: "Belum ada tujuan atau budget; dipakai Osaka sebagai contoh.", Source: "default"})
		st.route.Intent = domain.IntentPlanTrip
	case s.Destination == nil && s.Budget != nil:
		st.route.Intent = domain.IntentBudgetToDestination
	case s.Destination != nil && st.route.Intent == domain.IntentBudgetToDestination:
		st.route.Intent = domain.IntentPlanTrip
	}
	if asked {
		st.emit(ctx, "clarify.decided", false, "clarify", map[string]any{"decision": "ask", "missing": []string{"budget_or_destination"}}, nil)
		return true, nil
	}
	if st.route.Intent != domain.IntentBudgetToDestination && st.route.Intent != domain.IntentRefine {
		st.route.Intent = domain.IntentPlanTrip
	}
	if err := r.assume(ctx, st); err != nil {
		return false, err
	}
	req, err := r.request(ctx, st)
	if err != nil {
		return false, err
	}
	st.req = req
	st.emit(ctx, "clarify.decided", false, "clarify", map[string]any{"decision": "assume", "assumptions": s.Assumptions, "mode": st.route.Intent}, nil)
	if len(s.Assumptions) > 0 {
		st.emit(ctx, "assumptions", true, "clarify", map[string]any{"items": s.Assumptions}, nil)
	}
	return false, nil
}

// assume fills missing slots from profile → defaults, recording each.
func (r *Runner) assume(_ context.Context, st *runState) error {
	s := &st.state
	today := domain.DateOf(r.d.Clock.Now().In(jakarta()))
	if s.Origin == nil {
		if st.user.HomeCityCode != nil {
			if c, ok := reference.OriginCity(*st.user.HomeCityCode); ok {
				s.Origin = &domain.Place{Code: c.Code, Label: c.Label, Level: "city"}
				s.SetAssumption(domain.Assumption{Slot: "origin", Value: c.Label, Reason: "Kota asal dari profil Anda.", Source: "profile"})
			}
		}
		if s.Origin == nil {
			s.Origin = &domain.Place{Code: "JKT", Label: "Jakarta", Level: "city"}
			s.SetAssumption(domain.Assumption{Slot: "origin", Value: "Jakarta", Reason: "Kota asal belum disebut; diasumsikan Jakarta.", Source: "default"})
		}
	}
	if s.Destination != nil && s.Destination.Level == "country" {
		if p, ok := reference.CountryDefault(s.Destination.Code); ok {
			country := s.Destination.Label
			s.Destination = &p
			s.SetAssumption(domain.Assumption{Slot: "destination", Value: p.Label, Reason: fmt.Sprintf("Anda menyebut %s; kota yang dipakai %s. Bisa diganti Kyoto atau Tokyo.", country, p.Label), Source: "default"})
		}
	}
	if s.Pax.Adults == 0 {
		s.Pax = domain.Pax{Adults: 1}
		s.SetAssumption(domain.Assumption{Slot: "pax", Value: 1, Reason: "Jumlah orang belum disebut; dihitung untuk 1 orang dewasa.", Source: "default"})
	}
	if s.Nights == nil {
		n := DefaultNights
		s.Nights = &n
		s.SetAssumption(domain.Assumption{Slot: "nights", Value: n, Reason: fmt.Sprintf("Durasi belum disebut; dipakai %d malam, durasi umum perjalanan pertama ke Jepang.", n), Source: "default"})
	}
	if s.DateWindow == nil {
		m := domain.DateOf(today.Time().AddDate(0, 2, 0))
		s.DateWindow = &domain.DateWindow{Month: int(m.Month), Year: m.Year, FlexDays: 3}
		s.SetAssumption(domain.Assumption{Slot: "date_window", Value: fmt.Sprintf("%s %d", monthID[m.Month], m.Year),
			Reason: "Tanggal belum disebut; dipakai sekitar dua bulan lagi sebagai contoh.", Source: "default"})
	}
	if s.Budget != nil && !s.BudgetIsPerPerson && s.Pax.Total() > 1 && !s.HasAssumption("budget_scope") {
		s.SetAssumption(domain.Assumption{Slot: "budget_scope", Value: "total", Reason: fmt.Sprintf("Budget dianggap total untuk %d orang.", s.Pax.Total()), Source: "default"})
	}
	return nil
}

// request resolves the state into the immutable TripRequest for this run.
func (r *Runner) request(ctx context.Context, st *runState) (domain.TripRequest, error) {
	s := st.state
	now := r.d.Clock.Now()
	today := domain.DateOf(now.In(jakarta()))
	live := today.AddDays(r.d.Cfg.Policy.BookingWindowDays)
	req := domain.TripRequest{
		ID: platform.NewID(), Mode: st.route.Intent, Origin: *s.Origin, Destination: s.Destination, Budget: s.Budget,
		Nights: *s.Nights, Pax: s.Pax, Interests: s.Interests, Special: s.Special, LiveWindowEnd: live,
	}
	if s.DateWindow != nil {
		req.Window = *s.DateWindow
	}
	start, end, err := req.Window.Range()
	if err != nil {
		start, end = today.AddDays(30), today.AddDays(60)
	}
	req.WindowStart, req.WindowEnd = start, end
	if req.Window.Exact() {
		req.DepartDate = req.Window.From
	} else {
		req.DepartDate = domain.SuggestDeparture(start, end, req.Nights+1, today, live)
	}
	// Indonesia → Japan flights are mostly overnight: the return is one day
	// after arrival + nights. The planner uses each offer's arrival date.
	req.ReturnDate = req.DepartDate.AddDays(req.Nights + 1)
	if s.Budget != nil {
		idr := s.Budget.Amount
		if s.Budget.Currency != "IDR" {
			v, _, err := r.d.FX.Convert(ctx, *s.Budget)
			if err != nil {
				return req, fail("internal_error", true, fmt.Errorf("convert budget: %w", err))
			}
			idr = v
			st.state.SetAssumption(domain.Assumption{Slot: "budget_currency", Value: s.Budget.String(), Reason: "Budget dikonversi ke rupiah dengan kurs hari ini.", Source: "fx"})
		}
		if s.BudgetIsPerPerson {
			idr *= int64(s.Pax.Total())
		}
		req.BudgetIDR = &idr
	}
	req.Assumptions = append([]domain.Assumption{}, st.state.Assumptions...)
	if req.OutOfBookingWindow() {
		req.Assumptions = append(req.Assumptions, domain.Assumption{Slot: "booking_window", Value: live.String(),
			Reason: fmt.Sprintf("Tanggal ini belum dijual maskapai (biasanya dibuka ±%d hari sebelumnya); harga penerbangan memakai estimasi.", r.d.Cfg.Policy.BookingWindowDays), Source: "policy"})
	}
	return req, nil
}
