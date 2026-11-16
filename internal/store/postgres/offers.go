package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/domain"
)

// OfferRow is a stored offer snapshot. Summary holds the normalized domain
// offer as JSON.
type OfferRow struct {
	ID          uuid.UUID
	Kind        domain.OfferKind
	Supplier    string
	SupplierRef string
	SearchKey   string
	Price       domain.Money
	PriceIDR    int64
	FX          domain.FXQuote
	Summary     json.RawMessage
	Deeplink    *string
	FetchedAt   time.Time
	ExpiresAt   time.Time
}

func (q *Q) insertOffer(ctx context.Context, kind domain.OfferKind, id uuid.UUID, supplier, ref, key string, price domain.Money, priceIDR int64, fx domain.FXQuote, summary any, raw []byte, deeplink string, fetched, expires time.Time) error {
	s, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	var rawArg any
	if len(raw) > 0 && json.Valid(raw) {
		rawArg = raw
	}
	fxDate := fx.Date
	if fxDate.IsZero() {
		fxDate = domain.DateOf(fetched)
	}
	rate := fx.RateString()
	if fx.RateE8 == 0 {
		rate = "1.00000000"
	}
	_, err = q.db.Exec(ctx, `insert into offers (id, kind, supplier, supplier_ref, search_key, price_amount, price_currency, price_idr, fx_rate, fx_date, summary, raw, deeplink_url, fetched_at, expires_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8,($9::text)::numeric,$10,$11,$12,$13,$14,$15) on conflict (id) do nothing`,
		id, string(kind), supplier, ref, key, price.Amount, price.Currency, priceIDR, rate, fxDate.Time(), s, rawArg, strOrNil(deeplink), fetched, expires)
	return err
}

// SaveFlightOffer stores a flight offer snapshot.
func (q *Q) SaveFlightOffer(ctx context.Context, o domain.FlightOffer) error {
	return q.insertOffer(ctx, domain.OfferFlight, o.ID, o.Supplier, o.SupplierRef, o.SearchKey, o.Price, o.PriceIDR, o.FX, o, o.Raw, o.Deeplink, o.FetchedAt, o.ExpiresAt)
}

// SaveStayOffer stores a stay offer snapshot.
func (q *Q) SaveStayOffer(ctx context.Context, o domain.StayOffer) error {
	return q.insertOffer(ctx, domain.OfferStay, o.ID, o.Supplier, o.SupplierRef, o.SearchKey, o.PriceTotal, o.PriceIDR, o.FX, o, o.Raw, o.Deeplink, o.FetchedAt, o.ExpiresAt)
}

// SaveActivityOffer stores an activity offer snapshot.
func (q *Q) SaveActivityOffer(ctx context.Context, o domain.ActivityOffer) error {
	return q.insertOffer(ctx, domain.OfferActivity, o.ID, o.Supplier, o.SupplierRef, "", o.Price, o.PriceIDR, o.FX, o, nil, o.Deeplink, o.FetchedAt, o.ExpiresAt)
}

// GetOffer loads one offer.
func (q *Q) GetOffer(ctx context.Context, id uuid.UUID) (OfferRow, error) {
	var r OfferRow
	var kind string
	var summary []byte
	var rate string
	var fxDate time.Time
	err := q.db.QueryRow(ctx, `select id, kind, supplier, supplier_ref, search_key, price_amount, price_currency, price_idr, fx_rate::text, fx_date, summary, deeplink_url, fetched_at, expires_at
		from offers where id = $1`, id).Scan(&r.ID, &kind, &r.Supplier, &r.SupplierRef, &r.SearchKey, &r.Price.Amount, &r.Price.Currency, &r.PriceIDR, &rate, &fxDate, &summary, &r.Deeplink, &r.FetchedAt, &r.ExpiresAt)
	if err != nil {
		return r, notFound(err)
	}
	r.Kind = domain.OfferKind(kind)
	r.Summary = summary
	r.FX.RateE8, _ = domain.ParseRateE8(rate)
	r.FX.Date = domain.DateOf(fxDate)
	return r, nil
}

// FlightOffer decodes a stored flight offer.
func (r OfferRow) FlightOffer() (domain.FlightOffer, error) {
	var o domain.FlightOffer
	if r.Kind != domain.OfferFlight {
		return o, fmt.Errorf("offer %s is a %s", r.ID, r.Kind)
	}
	err := json.Unmarshal(r.Summary, &o)
	return o, err
}

// StayOffer decodes a stored stay offer.
func (r OfferRow) StayOffer() (domain.StayOffer, error) {
	var o domain.StayOffer
	if r.Kind != domain.OfferStay {
		return o, fmt.Errorf("offer %s is a %s", r.ID, r.Kind)
	}
	err := json.Unmarshal(r.Summary, &o)
	return o, err
}

// FreshOffers returns unexpired offers stored for a search key, newest
// fetch first (supplier cache in Postgres, SYSTEM_ARCHITECTURE §8).
func (q *Q) FreshOffers(ctx context.Context, kind domain.OfferKind, searchKey string, now time.Time) ([]OfferRow, error) {
	rows, err := q.db.Query(ctx, `
		with latest as (select max(fetched_at) as f from offers where kind = $1 and search_key = $2 and expires_at > $3)
		select id, kind, supplier, supplier_ref, search_key, price_amount, price_currency, price_idr, fx_rate::text, fx_date, summary, deeplink_url, fetched_at, expires_at
		from offers, latest where kind = $1 and search_key = $2 and fetched_at = latest.f order by price_idr`, string(kind), searchKey, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OfferRow
	for rows.Next() {
		var r OfferRow
		var k, rate string
		var summary []byte
		var fxDate time.Time
		if err := rows.Scan(&r.ID, &k, &r.Supplier, &r.SupplierRef, &r.SearchKey, &r.Price.Amount, &r.Price.Currency, &r.PriceIDR, &rate, &fxDate, &summary, &r.Deeplink, &r.FetchedAt, &r.ExpiresAt); err != nil {
			return nil, err
		}
		r.Kind = domain.OfferKind(k)
		r.Summary = summary
		r.FX.RateE8, _ = domain.ParseRateE8(rate)
		r.FX.Date = domain.DateOf(fxDate)
		out = append(out, r)
	}
	return out, rows.Err()
}
