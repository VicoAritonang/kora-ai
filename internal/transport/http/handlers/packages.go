package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/packages"
)

type sourceDTO struct {
	Label string  `json:"label"`
	URL   *string `json:"url"`
}

type itemDTO struct {
	ID          uuid.UUID      `json:"id"`
	DayIndex    *int           `json:"day_index"`
	Category    string         `json:"category"`
	Title       string         `json:"title"`
	Quantity    int            `json:"quantity"`
	UnitPrice   domain.Money   `json:"unit_price"`
	AmountIDR   domain.Money   `json:"amount_idr"`
	PriceKind   string         `json:"price_kind"`
	Source      sourceDTO      `json:"source"`
	PricedAt    time.Time      `json:"priced_at"`
	ExpiresAt   *time.Time     `json:"expires_at"`
	HasOutbound bool           `json:"has_outbound"`
	Details     map[string]any `json:"details"`
}

type packageDTO struct {
	ID           uuid.UUID            `json:"id"`
	Tier         string               `json:"tier"`
	Title        string               `json:"title"`
	Status       string               `json:"status"`
	Feasibility  string               `json:"feasibility"`
	Budget       *domain.Money        `json:"budget"`
	Total        domain.Money         `json:"total"`
	Breakdown    map[string]domain.Money `json:"breakdown"`
	Assumptions  []domain.Assumption  `json:"assumptions"`
	Alternatives []domain.Alternative `json:"alternatives"`
	Days         []domain.PackageDay  `json:"days,omitempty"`
	Items        []itemDTO            `json:"items,omitempty"`
	CreatedAt    time.Time            `json:"created_at"`
}

func toPackage(p domain.Package, full bool) packageDTO {
	d := packageDTO{ID: p.ID, Tier: string(p.Tier), Title: p.Title, Status: string(p.Status), Feasibility: string(p.Feasibility),
		Total: domain.IDR(p.TotalIDR), Assumptions: p.Assumptions, Alternatives: p.Alternatives, CreatedAt: p.CreatedAt,
		Breakdown: map[string]domain.Money{
			"live": domain.IDR(p.LiveIDR), "estimate": domain.IDR(p.EstimateIDR), "buffer": domain.IDR(p.BufferIDR),
			"remaining": domain.IDR(p.RemainingIDR()), "shortfall": domain.IDR(p.ShortfallIDR),
		}}
	if p.BudgetIDR != nil {
		b := domain.IDR(*p.BudgetIDR)
		d.Budget = &b
	}
	if d.Assumptions == nil {
		d.Assumptions = []domain.Assumption{}
	}
	if d.Alternatives == nil {
		d.Alternatives = []domain.Alternative{}
	}
	if !full {
		return d
	}
	d.Days = p.Days
	for _, it := range p.Items {
		var url *string
		if it.SourceURL != "" {
			u := it.SourceURL
			url = &u
		}
		// Outbound links exist only for real suppliers' live offers.
		has := it.PriceKind == domain.PriceLive && it.OfferID != nil && !isSandboxLabel(it.SourceLabel)
		d.Items = append(d.Items, itemDTO{
			ID: it.ID, DayIndex: it.DayIndex, Category: string(it.Category), Title: it.Title, Quantity: it.Quantity,
			UnitPrice: domain.Money{Amount: it.UnitAmount, Currency: it.Currency}, AmountIDR: domain.IDR(it.AmountIDR),
			PriceKind: string(it.PriceKind), Source: sourceDTO{it.SourceLabel, url}, PricedAt: it.PricedAt, ExpiresAt: it.ExpiresAt,
			HasOutbound: has, Details: it.Details,
		})
	}
	return d
}

func isSandboxLabel(s string) bool { return len(s) >= 7 && s[:7] == "Sandbox" }

func (a *API) pkgError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, packages.ErrNotFound):
		notFound(w, r)
	case errors.Is(err, packages.ErrOfferExpired):
		problem(w, r, 410, "offer_expired", "Penawaran sudah kedaluwarsa", "Cek ulang harga paket ini.")
	case errors.Is(err, packages.ErrNoOutbound):
		problem(w, r, 404, "not_found", "Item ini tidak punya tautan keluar", "")
	case errors.Is(err, packages.ErrRateLimited):
		w.Header().Set("Retry-After", "300")
		problem(w, r, 429, "rate_limited", "Cek ulang harga dibatasi", "Maksimal sekali per 5 menit per paket.")
	case errors.Is(err, packages.ErrUpstream):
		problem(w, r, 502, "upstream_error", "Penyedia harga gagal", "Paket tidak diubah. Coba lagi nanti.")
	case errors.Is(err, packages.ErrNotRepriceable):
		problem(w, r, 409, "state_conflict", "Paket ini tidak bisa dicek ulang", "")
	default:
		internalError(w, r, err)
	}
}

// ListPackages handles GET /conversations/{id}/packages.
func (a *API) ListPackages(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	ps, err := a.Packages.List(r.Context(), session(r).UserID, id, limit(r))
	if err != nil {
		a.pkgError(w, r, err)
		return
	}
	out := make([]packageDTO, 0, len(ps))
	for _, p := range ps {
		out = append(out, toPackage(p, false))
	}
	writeJSON(w, 200, map[string]any{"items": out, "next_cursor": nil})
}

// GetPackage handles GET /packages/{id}.
func (a *API) GetPackage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	p, _, err := a.Packages.Get(r.Context(), session(r).UserID, id)
	if err != nil {
		a.pkgError(w, r, err)
		return
	}
	writeJSON(w, 200, toPackage(p, true))
}

// SelectPackage handles POST /packages/{id}/select.
func (a *API) SelectPackage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	p, err := a.Packages.Select(r.Context(), session(r).UserID, id)
	if err != nil {
		a.pkgError(w, r, err)
		return
	}
	writeJSON(w, 200, toPackage(p, false))
}

// RepricePackage handles POST /packages/{id}/reprice.
func (a *API) RepricePackage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	res, err := a.Packages.Reprice(r.Context(), session(r).UserID, id)
	if err != nil {
		a.pkgError(w, r, err)
		return
	}
	if res.Changes == nil {
		res.Changes = []packages.Change{}
	}
	writeJSON(w, 200, res)
}

// Outbound handles POST /packages/{id}/items/{item_id}/outbound.
func (a *API) Outbound(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	item, ok := pathID(w, r, "item_id")
	if !ok {
		return
	}
	url, err := a.Packages.Outbound(r.Context(), session(r).UserID, id, item)
	if err != nil {
		a.pkgError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]string{"url": url})
}
