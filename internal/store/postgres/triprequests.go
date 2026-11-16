package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/domain"
)

// GetTripRequest rebuilds the stored snapshot of a run's slots.
func (q *Q) GetTripRequest(ctx context.Context, id uuid.UUID) (domain.TripRequest, error) {
	var r domain.TripRequest
	var origin string
	var dest, cur *string
	var amount, idr *int64
	var from, to *time.Time
	var win, sp, as []byte
	var nights *int
	err := q.db.QueryRow(ctx, `select id, coalesce(origin_code, ''), destination_code, budget_amount, budget_currency, budget_idr, date_from, date_to,
		date_window, nights, pax_adults, pax_children, interests, special_requests, assumptions from trip_requests where id = $1`, id).
		Scan(&r.ID, &origin, &dest, &amount, &cur, &idr, &from, &to, &win, &nights, &r.Pax.Adults, &r.Pax.Children, &r.Interests, &sp, &as)
	if err != nil {
		return r, notFound(err)
	}
	r.Origin = domain.Place{Code: origin}
	if dest != nil {
		r.Destination = &domain.Place{Code: *dest}
	}
	if amount != nil && cur != nil {
		r.Budget = &domain.Money{Amount: *amount, Currency: *cur}
	}
	r.BudgetIDR = idr
	if from != nil {
		r.DepartDate = domain.DateOf(*from)
	}
	if to != nil {
		r.ReturnDate = domain.DateOf(*to)
	}
	if nights != nil {
		r.Nights = *nights
	}
	var w struct {
		Window        domain.DateWindow `json:"window"`
		Start         domain.Date       `json:"window_start"`
		End           domain.Date       `json:"window_end"`
		LiveWindowEnd domain.Date       `json:"live_window_end"`
	}
	if json.Unmarshal(win, &w) == nil {
		r.Window, r.WindowStart, r.WindowEnd, r.LiveWindowEnd = w.Window, w.Start, w.End, w.LiveWindowEnd
	}
	_ = json.Unmarshal(sp, &r.Special)
	_ = json.Unmarshal(as, &r.Assumptions)
	return r, nil
}
