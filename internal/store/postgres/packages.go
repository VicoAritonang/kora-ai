package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/domain"
)

// PackageMeta locates a package in its conversation and run.
type PackageMeta struct {
	ConversationID uuid.UUID
	RunID          uuid.UUID
	TripRequestID  uuid.UUID
	UserID         uuid.UUID
	RunStatus      string
}

// InsertPackage stores a package with its days and items. Call inside a
// transaction together with the other packages of the run.
func (q *Q) InsertPackage(ctx context.Context, convID, runID, tripReqID uuid.UUID, p domain.Package) error {
	as, _ := json.Marshal(p.Assumptions)
	alt, _ := json.Marshal(p.Alternatives)
	_, err := q.db.Exec(ctx, `insert into packages (id, conversation_id, run_id, trip_request_id, tier, title, status, feasibility, budget_idr,
		total_idr, live_idr, estimate_idr, buffer_idr, shortfall_idr, assumptions, alternatives, builder_version, flight_offer_id, stay_offer_id, created_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		p.ID, convID, runID, tripReqID, string(p.Tier), p.Title, string(p.Status), string(p.Feasibility), p.BudgetIDR,
		p.TotalIDR, p.LiveIDR, p.EstimateIDR, p.BufferIDR, p.ShortfallIDR, as, alt, p.BuilderVersion, p.FlightOfferID, p.StayOfferID, p.CreatedAt)
	if err != nil {
		return err
	}
	for _, d := range p.Days {
		if _, err := q.db.Exec(ctx, `insert into package_days (id, package_id, day_index, date, title, summary, area) values ($1,$2,$3,$4,$5,$6,$7)`,
			uuid.NewSHA1(p.ID, []byte{byte(d.DayIndex), byte(d.DayIndex >> 8)}), p.ID, d.DayIndex, dateOrNil(d.Date.String()), d.Title, strOrNil(d.Summary), strOrNil(d.Area)); err != nil {
			return err
		}
	}
	for _, it := range p.Items {
		det, _ := json.Marshal(it.Details)
		rate := it.FX.RateString()
		if it.FX.RateE8 == 0 {
			rate = "1.00000000"
		}
		if _, err := q.db.Exec(ctx, `insert into package_items (id, package_id, day_index, sort_order, category, title, quantity, unit_amount, currency, amount_idr,
			fx_rate, fx_date, price_kind, offer_id, norm_id, price_cache_id, source_label, source_url, priced_at, expires_at, details)
			values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,($11::text)::numeric,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
			it.ID, p.ID, it.DayIndex, it.SortOrder, string(it.Category), it.Title, max1(it.Quantity), it.UnitAmount, it.Currency, it.AmountIDR,
			rate, dateOrNil(it.FX.Date.String()), string(it.PriceKind), it.OfferID, it.NormID, it.PriceCacheID, it.SourceLabel, strOrNil(it.SourceURL),
			it.PricedAt, it.ExpiresAt, det); err != nil {
			return err
		}
	}
	return nil
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

const pkgCols = `p.id, p.tier, p.title, p.status, p.feasibility, p.budget_idr, p.total_idr, p.live_idr, p.estimate_idr, p.buffer_idr, p.shortfall_idr,
	p.assumptions, p.alternatives, p.builder_version, p.flight_offer_id, p.stay_offer_id, p.created_at,
	p.conversation_id, p.run_id, p.trip_request_id, c.user_id, r.status`

func scanPkg(row interface{ Scan(...any) error }) (domain.Package, PackageMeta, error) {
	var p domain.Package
	var m PackageMeta
	var tier, status, feas string
	var as, alt []byte
	var fo, so uuid.NullUUID
	err := row.Scan(&p.ID, &tier, &p.Title, &status, &feas, &p.BudgetIDR, &p.TotalIDR, &p.LiveIDR, &p.EstimateIDR, &p.BufferIDR, &p.ShortfallIDR,
		&as, &alt, &p.BuilderVersion, &fo, &so, &p.CreatedAt, &m.ConversationID, &m.RunID, &m.TripRequestID, &m.UserID, &m.RunStatus)
	if err != nil {
		return p, m, err
	}
	p.Tier, p.Status, p.Feasibility = domain.Tier(tier), domain.PackageStatus(status), domain.Feasibility(feas)
	_ = json.Unmarshal(as, &p.Assumptions)
	_ = json.Unmarshal(alt, &p.Alternatives)
	if fo.Valid {
		p.FlightOfferID = &fo.UUID
	}
	if so.Valid {
		p.StayOfferID = &so.UUID
	}
	return p, m, nil
}

// GetPackage loads a full package (days and items).
func (q *Q) GetPackage(ctx context.Context, id uuid.UUID) (domain.Package, PackageMeta, error) {
	p, m, err := scanPkg(q.db.QueryRow(ctx, `select `+pkgCols+` from packages p join conversations c on c.id = p.conversation_id join runs r on r.id = p.run_id where p.id = $1`, id))
	if err != nil {
		return p, m, notFound(err)
	}
	if err := q.loadPackageDetail(ctx, &p); err != nil {
		return p, m, err
	}
	return p, m, nil
}

func (q *Q) loadPackageDetail(ctx context.Context, p *domain.Package) error {
	rows, err := q.db.Query(ctx, `select day_index, to_char(date, 'YYYY-MM-DD'), title, coalesce(summary, ''), coalesce(area, '') from package_days where package_id = $1 order by day_index`, p.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var d domain.PackageDay
		var date *string
		if err := rows.Scan(&d.DayIndex, &date, &d.Title, &d.Summary, &d.Area); err != nil {
			rows.Close()
			return err
		}
		if date != nil {
			d.Date, _ = domain.ParseDate(*date)
		}
		p.Days = append(p.Days, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	rows, err = q.db.Query(ctx, `select id, day_index, sort_order, category, title, quantity, unit_amount, currency, amount_idr, fx_rate::text, to_char(fx_date, 'YYYY-MM-DD'),
		price_kind, offer_id, norm_id, price_cache_id, source_label, coalesce(source_url, ''), priced_at, expires_at, details
		from package_items where package_id = $1 order by sort_order`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var it domain.PackageItem
		var cat, kind, rate string
		var fxDate *string
		var offer, norm, pc uuid.NullUUID
		var det []byte
		if err := rows.Scan(&it.ID, &it.DayIndex, &it.SortOrder, &cat, &it.Title, &it.Quantity, &it.UnitAmount, &it.Currency, &it.AmountIDR, &rate, &fxDate,
			&kind, &offer, &norm, &pc, &it.SourceLabel, &it.SourceURL, &it.PricedAt, &it.ExpiresAt, &det); err != nil {
			return err
		}
		it.Category, it.PriceKind = domain.ItemCategory(cat), domain.PriceKind(kind)
		it.FX.RateE8, _ = domain.ParseRateE8(rate)
		it.FX.Base, it.FX.Quote = it.Currency, "IDR"
		if fxDate != nil {
			it.FX.Date, _ = domain.ParseDate(*fxDate)
		}
		if offer.Valid {
			it.OfferID = &offer.UUID
		}
		if norm.Valid {
			it.NormID = &norm.UUID
		}
		if pc.Valid {
			it.PriceCacheID = &pc.UUID
		}
		_ = json.Unmarshal(det, &it.Details)
		p.Items = append(p.Items, it)
	}
	return rows.Err()
}

// ListPackages returns a conversation's packages, newest first, without items.
func (q *Q) ListPackages(ctx context.Context, convID uuid.UUID, limit int) ([]domain.Package, error) {
	rows, err := q.db.Query(ctx, `select `+pkgCols+` from packages p join conversations c on c.id = p.conversation_id join runs r on r.id = p.run_id
		where p.conversation_id = $1 order by p.created_at desc, p.tier limit $2`, convID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Package
	for rows.Next() {
		p, _, err := scanPkg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RunPackages returns the packages produced by a run, in tier order.
func (q *Q) RunPackages(ctx context.Context, runID uuid.UUID) ([]domain.Package, error) {
	rows, err := q.db.Query(ctx, `select `+pkgCols+` from packages p join conversations c on c.id = p.conversation_id join runs r on r.id = p.run_id
		where p.run_id = $1 order by p.tier`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Package
	for rows.Next() {
		p, _, err := scanPkg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// MarkConversationPackagesStale marks every non-stale package of a
// conversation stale (slots changed; MODULE_SPEC §4).
func (q *Q) MarkConversationPackagesStale(ctx context.Context, convID uuid.UUID, exceptRun uuid.UUID) error {
	_, err := q.db.Exec(ctx, `update packages set status = 'stale' where conversation_id = $1 and run_id <> $2 and status <> 'stale'`, convID, exceptRun)
	return err
}

// MarkPackageStale marks one package stale.
func (q *Q) MarkPackageStale(ctx context.Context, id uuid.UUID) error {
	_, err := q.db.Exec(ctx, `update packages set status = 'stale' where id = $1`, id)
	return err
}

// SelectPackage marks a package selected and unselects the others.
func (q *Q) SelectPackage(ctx context.Context, convID, id uuid.UUID) error {
	if _, err := q.db.Exec(ctx, `update packages set status = 'proposed' where conversation_id = $1 and status = 'selected'`, convID); err != nil {
		return err
	}
	if _, err := q.db.Exec(ctx, `update packages set status = 'selected' where id = $1 and status <> 'stale'`, id); err != nil {
		return err
	}
	return q.SetSelectedPackage(ctx, convID, id)
}

// MarkExpiredPackagesStale marks packages whose live items expired as stale.
func (q *Q) MarkExpiredPackagesStale(ctx context.Context, now time.Time) (int64, error) {
	tag, err := q.db.Exec(ctx, `update packages p set status = 'stale' where status = 'proposed'
		and exists (select 1 from package_items i where i.package_id = p.id and i.price_kind = 'live' and i.expires_at < $1)`, now)
	return tag.RowsAffected(), err
}

// RecordClick logs an outbound click.
func (q *Q) RecordClick(ctx context.Context, id uuid.UUID, userID *uuid.UUID, itemID, offerID uuid.UUID, supplier string) error {
	_, err := q.db.Exec(ctx, `insert into outbound_clicks (id, user_id, package_item_id, offer_id, supplier) values ($1,$2,$3,$4,$5)`, id, userID, itemID, offerID, supplier)
	return err
}
