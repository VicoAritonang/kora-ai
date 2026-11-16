package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koraai/koraai/internal/domain"
)

// ---------------------------------------------------------------- norms --

// ListNorms returns every norm of a destination.
func (q *Q) ListNorms(ctx context.Context, dest string) ([]domain.Norm, error) {
	rows, err := q.db.Query(ctx, `select id, destination_code, category, coalesce(tier, ''), label, unit, amount, currency, child_factor::float8, details,
		source_label, coalesce(source_url, ''), verified_at, to_char(valid_from, 'YYYY-MM-DD'), to_char(valid_to, 'YYYY-MM-DD')
		from cost_norms where destination_code = $1 order by category, tier, label`, dest)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Norm
	for rows.Next() {
		var n domain.Norm
		var det []byte
		var verified time.Time
		var vf, vt *string
		if err := rows.Scan(&n.ID, &n.Destination, &n.Category, &n.Tier, &n.Label, &n.Unit, &n.Amount.Amount, &n.Amount.Currency, &n.ChildFactor, &det,
			&n.SourceLabel, &n.SourceURL, &verified, &vf, &vt); err != nil {
			return nil, err
		}
		n.VerifiedAt = domain.DateOf(verified)
		if vf != nil {
			n.ValidFrom, _ = domain.ParseDate(*vf)
		}
		if vt != nil {
			n.ValidTo, _ = domain.ParseDate(*vt)
		}
		_ = json.Unmarshal(det, &n.Details)
		out = append(out, n)
	}
	return out, rows.Err()
}

// UpsertNorm inserts or updates a norm that no package references yet.
func (q *Q) UpsertNorm(ctx context.Context, n domain.Norm) error {
	det, _ := json.Marshal(n.Details)
	_, err := q.db.Exec(ctx, `insert into cost_norms (id, destination_code, category, tier, label, unit, amount, currency, child_factor, details, source_label, source_url, verified_at, valid_from, valid_to)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		on conflict (id) do update set label = excluded.label, amount = excluded.amount, currency = excluded.currency, child_factor = excluded.child_factor,
			details = excluded.details, source_label = excluded.source_label, source_url = excluded.source_url, verified_at = excluded.verified_at,
			valid_from = excluded.valid_from, valid_to = excluded.valid_to, updated_at = now()
		where not exists (select 1 from package_items pi where pi.norm_id = cost_norms.id)`,
		n.ID, n.Destination, n.Category, strOrNil(n.Tier), n.Label, n.Unit, n.Amount.Amount, n.Amount.Currency, n.ChildFactor, det,
		n.SourceLabel, strOrNil(n.SourceURL), n.VerifiedAt.Time(), dateOrNil(n.ValidFrom.String()), dateOrNil(n.ValidTo.String()))
	return err
}

// ----------------------------------------------------------- price cache --

// UpsertPriceCache writes one index row (overwritten on refresh).
func (q *Q) UpsertPriceCache(ctx context.Context, e domain.Estimate, expires time.Time) error {
	_, err := q.db.Exec(ctx, `insert into price_cache (id, kind, origin_code, destination_code, month, tier, min_idr, median_idr, sample_count, source, refreshed_at, expires_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		on conflict (kind, origin_code, destination_code, month, tier) do update set min_idr = excluded.min_idr, median_idr = excluded.median_idr,
			sample_count = excluded.sample_count, source = excluded.source, refreshed_at = excluded.refreshed_at, expires_at = excluded.expires_at`,
		e.PriceCacheID, e.Kind, e.Origin, e.Destination, e.Month.Time(), e.Tier, e.MinIDR, e.MedianIDR, e.SampleCount, e.Source, e.RefreshedAt, expires)
	return err
}

// PriceCacheRows returns index rows for a destination (and origin for
// flights), optionally for one month.
func (q *Q) PriceCacheRows(ctx context.Context, kind, origin, dest string, month *domain.Date, now time.Time) ([]domain.Estimate, error) {
	var m any
	if month != nil {
		m = month.Time()
	}
	rows, err := q.db.Query(ctx, `select id, kind, origin_code, destination_code, month, tier, min_idr, median_idr, sample_count, source, refreshed_at, expires_at
		from price_cache where kind = $1 and origin_code = $2 and destination_code = $3 and ($4::date is null or month = $4::date) order by month`, kind, origin, dest, m)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Estimate
	for rows.Next() {
		var e domain.Estimate
		var month time.Time
		var expires time.Time
		if err := rows.Scan(&e.PriceCacheID, &e.Kind, &e.Origin, &e.Destination, &month, &e.Tier, &e.MinIDR, &e.MedianIDR, &e.SampleCount, &e.Source, &e.RefreshedAt, &expires); err != nil {
			return nil, err
		}
		e.Month = domain.DateOf(month)
		e.Stale = expires.Before(now)
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountPriceCache counts index rows (worker bootstrap).
func (q *Q) CountPriceCache(ctx context.Context) (int, error) {
	var n int
	err := q.db.QueryRow(ctx, `select count(*) from price_cache`).Scan(&n)
	return n, err
}

// ------------------------------------------------------------------- FX --

// SaveFXRate stores a daily rate.
func (q *Q) SaveFXRate(ctx context.Context, fx domain.FXQuote) error {
	_, err := q.db.Exec(ctx, `insert into fx_rates (base, quote, rate_date, rate, source) values ($1,$2,$3,($4::text)::numeric,$5)
		on conflict (base, quote, rate_date) do update set rate = excluded.rate, source = excluded.source`,
		fx.Base, fx.Quote, fx.Date.Time(), fx.RateString(), fx.Source)
	return err
}

// LatestFXRate returns the newest rate on or before a date.
func (q *Q) LatestFXRate(ctx context.Context, base, quote string, on domain.Date) (domain.FXQuote, error) {
	var fx domain.FXQuote
	var rate string
	var d time.Time
	err := q.db.QueryRow(ctx, `select base, quote, rate_date, rate::text, source from fx_rates where base = $1 and quote = $2 and rate_date <= $3 order by rate_date desc limit 1`,
		base, quote, on.Time()).Scan(&fx.Base, &fx.Quote, &d, &rate, &fx.Source)
	if err != nil {
		return fx, notFound(err)
	}
	fx.Date = domain.DateOf(d)
	fx.RateE8, err = domain.ParseRateE8(rate)
	return fx, err
}

// ------------------------------------------------------------ reminders --

// Reminder is a reminders row.
type Reminder struct {
	ID             uuid.UUID       `json:"id"`
	UserID         uuid.UUID       `json:"-"`
	ConversationID *uuid.UUID      `json:"conversation_id"`
	Kind           string          `json:"kind"`
	Payload        json.RawMessage `json:"payload"`
	TriggerAt      time.Time       `json:"trigger_at"`
	Status         string          `json:"status"`
	Attempts       int             `json:"-"`
	CreatedAt      time.Time       `json:"created_at"`
}

const remCols = `id, user_id, conversation_id, kind, payload, trigger_at, status, attempts, created_at`

func scanRem(row interface{ Scan(...any) error }) (Reminder, error) {
	var r Reminder
	var conv uuid.NullUUID
	var p []byte
	err := row.Scan(&r.ID, &r.UserID, &conv, &r.Kind, &p, &r.TriggerAt, &r.Status, &r.Attempts, &r.CreatedAt)
	if conv.Valid {
		r.ConversationID = &conv.UUID
	}
	r.Payload = p
	return r, err
}

// InsertReminder creates a reminder.
func (q *Q) InsertReminder(ctx context.Context, r Reminder) (Reminder, error) {
	out, err := scanRem(q.db.QueryRow(ctx, `insert into reminders (id, user_id, conversation_id, kind, payload, trigger_at) values ($1,$2,$3,$4,$5,$6) returning `+remCols,
		r.ID, r.UserID, r.ConversationID, r.Kind, []byte(r.Payload), r.TriggerAt))
	return out, err
}

// ListReminders returns a user's reminders.
func (q *Q) ListReminders(ctx context.Context, userID uuid.UUID) ([]Reminder, error) {
	rows, err := q.db.Query(ctx, `select `+remCols+` from reminders where user_id = $1 order by trigger_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reminder
	for rows.Next() {
		r, err := scanRem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CancelReminder cancels a pending reminder owned by userID.
func (q *Q) CancelReminder(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := q.db.Exec(ctx, `update reminders set status = 'cancelled' where id = $1 and user_id = $2 and status = 'pending'`, id, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// DueReminders locks and returns due reminders (FOR UPDATE SKIP LOCKED).
func (q *Q) DueReminders(ctx context.Context, now time.Time, limit int) ([]Reminder, error) {
	rows, err := q.db.Query(ctx, `select `+remCols+` from reminders where status = 'pending' and trigger_at <= $1 order by trigger_at limit $2 for update skip locked`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reminder
	for rows.Next() {
		r, err := scanRem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkReminder sets a reminder's status after an attempt. A failed attempt
// below maxAttempts is retried later with backoff.
func (q *Q) MarkReminder(ctx context.Context, id uuid.UUID, sent bool, retryAt *time.Time, maxAttempts int) error {
	if sent {
		_, err := q.db.Exec(ctx, `update reminders set status = 'sent', attempts = attempts + 1 where id = $1`, id)
		return err
	}
	_, err := q.db.Exec(ctx, `update reminders set attempts = attempts + 1,
		status = case when attempts + 1 >= $3 then 'failed' else 'pending' end,
		trigger_at = coalesce($2, trigger_at) where id = $1`, id, retryAt, maxAttempts)
	return err
}

// UserEmail returns a user's email for reminders (nil for guests).
func (q *Q) UserEmail(ctx context.Context, userID uuid.UUID) (*string, error) {
	var e *string
	err := q.db.QueryRow(ctx, `select email::text from users where id = $1 and deleted_at is null`, userID).Scan(&e)
	return e, notFound(err)
}

// ------------------------------------------------------------- feedback --

// InsertFeedback stores a rating.
func (q *Q) InsertFeedback(ctx context.Context, id uuid.UUID, userID uuid.UUID, msgID, pkgID *uuid.UUID, rating int, reason, comment *string) error {
	_, err := q.db.Exec(ctx, `insert into feedback (id, user_id, message_id, package_id, rating, reason, comment) values ($1,$2,$3,$4,$5,$6,$7)`,
		id, userID, msgID, pkgID, rating, reason, comment)
	return err
}

// -------------------------------------------------------- idempotency ----

// Idempotency is a stored idempotency key.
type Idempotency struct {
	RequestHash []byte
	Response    json.RawMessage
}

// GetIdempotency loads a key (ErrNotFound if absent).
func (q *Q) GetIdempotency(ctx context.Context, userID uuid.UUID, key string) (Idempotency, error) {
	var i Idempotency
	var resp []byte
	err := q.db.QueryRow(ctx, `select request_hash, response from idempotency_keys where user_id = $1 and key = $2 and created_at > now() - interval '24 hours'`, userID, key).Scan(&i.RequestHash, &resp)
	i.Response = resp
	return i, notFound(err)
}

// SaveIdempotency stores a key with its response.
func (q *Q) SaveIdempotency(ctx context.Context, userID uuid.UUID, key string, hash []byte, response any) error {
	b, _ := json.Marshal(response)
	_, err := q.db.Exec(ctx, `insert into idempotency_keys (key, user_id, request_hash, response) values ($1,$2,$3,$4)
		on conflict (user_id, key) do update set request_hash = excluded.request_hash, response = excluded.response, created_at = now()`, key, userID, hash, b)
	return err
}

// ------------------------------------------------------------ retention --

// RetentionResult counts rows affected by one retention pass.
type RetentionResult map[string]int64

// RunRetention applies DATABASE_SCHEMA §7 in one pass.
func (q *Q) RunRetention(ctx context.Context, now time.Time) (RetentionResult, error) {
	res := RetentionResult{}
	steps := []struct {
		name string
		sql  string
	}{
		{"guest_users", `delete from users where is_guest and coalesce(last_seen_at, created_at) < $1::timestamptz - interval '30 days'`},
		{"deleted_users", `delete from users where deleted_at is not null and deleted_at < $1::timestamptz - interval '7 days'`},
		{"offers_raw", `update offers set raw = null where raw is not null and created_at < $1::timestamptz - interval '30 days'`},
		{"offers", `delete from offers o where created_at < $1::timestamptz - interval '30 days'
			and not exists (select 1 from package_items i where i.offer_id = o.id)
			and not exists (select 1 from packages p where p.flight_offer_id = o.id or p.stay_offer_id = o.id)`},
		{"tool_artifacts", `delete from tool_artifacts where created_at < $1::timestamptz - interval '90 days'`},
		{"idempotency_keys", `delete from idempotency_keys where created_at < $1::timestamptz - interval '24 hours'`},
		{"auth_sessions", `delete from auth_sessions where expires_at < $1::timestamptz or revoked_at < $1::timestamptz - interval '1 day'`},
		{"magic_link_tokens", `delete from magic_link_tokens where expires_at < $1::timestamptz`},
	}
	for _, s := range steps {
		tag, err := q.db.Exec(ctx, s.sql, now)
		if err != nil {
			return res, err
		}
		res[s.name] = tag.RowsAffected()
	}
	return res, nil
}

// DeletedUsersDue lists users whose grace period has ended (memory forget).
func (q *Q) DeletedUsersDue(ctx context.Context, now time.Time) ([]uuid.UUID, error) {
	rows, err := q.db.Query(ctx, `select id from users where deleted_at is not null and deleted_at < $1::timestamptz - interval '7 days'`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ExportUser returns every row a user owns as JSON documents (UU PDP).
func (q *Q) ExportUser(ctx context.Context, userID uuid.UUID) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	queries := map[string]string{
		"user":          `select to_jsonb(u) - 'deleted_at' from users u where id = $1`,
		"conversations": `select coalesce(jsonb_agg(to_jsonb(c) - 'guard_session'), '[]') from conversations c where user_id = $1`,
		"messages":      `select coalesce(jsonb_agg(to_jsonb(m) order by m.conversation_id, m.seq), '[]') from messages m join conversations c on c.id = m.conversation_id where c.user_id = $1`,
		"packages":      `select coalesce(jsonb_agg(to_jsonb(p)), '[]') from packages p join conversations c on c.id = p.conversation_id where c.user_id = $1`,
		"reminders":     `select coalesce(jsonb_agg(to_jsonb(r)), '[]') from reminders r where user_id = $1`,
		"feedback":      `select coalesce(jsonb_agg(to_jsonb(f)), '[]') from feedback f where user_id = $1`,
	}
	for k, sql := range queries {
		var b []byte
		if err := q.db.QueryRow(ctx, sql, userID).Scan(&b); err != nil && err != pgx.ErrNoRows {
			return nil, err
		}
		out[k] = b
	}
	return out, nil
}
