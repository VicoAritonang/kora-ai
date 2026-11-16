package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koraai/koraai/internal/domain"
)

// Run is a runs row.
type Run struct {
	ID             uuid.UUID       `json:"id"`
	ConversationID uuid.UUID       `json:"conversation_id"`
	UserMessageID  uuid.UUID       `json:"user_message_id"`
	Status         string          `json:"status"`
	Mode           *string         `json:"mode"`
	StepCount      int             `json:"step_count"`
	InputTokens    int64           `json:"input_tokens"`
	CachedTokens   int64           `json:"cached_tokens"`
	OutputTokens   int64           `json:"output_tokens"`
	CostMicros     int64           `json:"cost_usd_micros"`
	Flags          []string        `json:"flags"`
	ErrorCode      *string         `json:"error_code"`
	ConfigSnapshot json.RawMessage `json:"config_snapshot"`
	StartedAt      time.Time       `json:"started_at"`
	FinishedAt     *time.Time      `json:"finished_at"`
}

const runCols = `id, conversation_id, user_message_id, status, mode, step_count, input_tokens, cached_tokens, output_tokens, cost_usd_micros, flags, error_code, config_snapshot, started_at, finished_at`

func scanRun(row interface{ Scan(...any) error }) (Run, error) {
	var r Run
	var cfg []byte
	err := row.Scan(&r.ID, &r.ConversationID, &r.UserMessageID, &r.Status, &r.Mode, &r.StepCount, &r.InputTokens, &r.CachedTokens, &r.OutputTokens, &r.CostMicros, &r.Flags, &r.ErrorCode, &cfg, &r.StartedAt, &r.FinishedAt)
	r.ConfigSnapshot = cfg
	return r, err
}

// InsertRun creates a running run. The partial unique index guarantees one
// active run per conversation; a violation maps to ErrConflict.
func (q *Q) InsertRun(ctx context.Context, id, convID, msgID uuid.UUID, snapshot any) (Run, error) {
	b, _ := json.Marshal(snapshot)
	r, err := scanRun(q.db.QueryRow(ctx, `insert into runs (id, conversation_id, user_message_id, config_snapshot) values ($1,$2,$3,$4) returning `+runCols, id, convID, msgID, b))
	if IsUniqueViolation(err) {
		return r, ErrConflict
	}
	return r, err
}

// GetRun loads a run.
func (q *Q) GetRun(ctx context.Context, id uuid.UUID) (Run, error) {
	r, err := scanRun(q.db.QueryRow(ctx, `select `+runCols+` from runs where id = $1`, id))
	return r, notFound(err)
}

// GetRunForUser loads a run whose conversation belongs to userID.
func (q *Q) GetRunForUser(ctx context.Context, userID, id uuid.UUID) (Run, error) {
	r, err := scanRun(q.db.QueryRow(ctx, `select `+prefixed("r", runCols)+` from runs r join conversations c on c.id = r.conversation_id where r.id = $1 and c.user_id = $2`, id, userID))
	return r, notFound(err)
}

func prefixed(p, cols string) string {
	out := ""
	start := 0
	for i := 0; i <= len(cols); i++ {
		if i == len(cols) || cols[i] == ',' {
			c := cols[start:i]
			for len(c) > 0 && c[0] == ' ' {
				c = c[1:]
			}
			if out != "" {
				out += ", "
			}
			out += p + "." + c
			start = i + 1
		}
	}
	return out
}

// ActiveRun returns the running run of a conversation, if any.
func (q *Q) ActiveRun(ctx context.Context, convID uuid.UUID) (*Run, error) {
	r, err := scanRun(q.db.QueryRow(ctx, `select `+runCols+` from runs where conversation_id = $1 and status = 'running'`, convID))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// CountActiveRunsForUser counts running runs across a user's conversations.
func (q *Q) CountActiveRunsForUser(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := q.db.QueryRow(ctx, `select count(*) from runs r join conversations c on c.id = r.conversation_id where c.user_id = $1 and r.status = 'running'`, userID).Scan(&n)
	return n, err
}

// SetRunMode records the routed intent.
func (q *Q) SetRunMode(ctx context.Context, id uuid.UUID, mode domain.Intent) error {
	_, err := q.db.Exec(ctx, `update runs set mode = $2 where id = $1`, id, string(mode))
	return err
}

// RunTotals are the counters written when a run finishes.
type RunTotals struct {
	Steps        int
	InputTokens  int64
	CachedTokens int64
	OutputTokens int64
	CostMicros   int64
	Flags        []string
}

// FinishRun sets the terminal status. It only transitions running runs, so a
// late finish cannot overwrite a sweeper's failure or a cancellation.
func (q *Q) FinishRun(ctx context.Context, id uuid.UUID, status string, errorCode *string, t RunTotals) error {
	if t.Flags == nil {
		t.Flags = []string{}
	}
	_, err := q.db.Exec(ctx, `update runs set status = $2, error_code = $3, step_count = $4, input_tokens = $5, cached_tokens = $6,
		output_tokens = $7, cost_usd_micros = $8, flags = $9, finished_at = now() where id = $1 and status = 'running'`,
		id, status, errorCode, t.Steps, t.InputTokens, t.CachedTokens, t.OutputTokens, t.CostMicros, t.Flags)
	return err
}

// FailStaleRuns marks runs running longer than maxAge as failed and returns
// their ids (sweeper, NFR-14).
func (q *Q) FailStaleRuns(ctx context.Context, maxAge time.Duration, code string) ([]uuid.UUID, error) {
	rows, err := q.db.Query(ctx, `update runs set status = 'failed', error_code = $2, finished_at = now()
		where status = 'running' and started_at < now() - make_interval(secs => $1) returning id`, maxAge.Seconds(), code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListRuns returns runs for the internal API, newest first.
func (q *Q) ListRuns(ctx context.Context, from, to time.Time, status, mode string, limit int) ([]Run, error) {
	rows, err := q.db.Query(ctx, `select `+runCols+` from runs where started_at between $1 and $2
		and ($3 = '' or status::text = $3) and ($4 = '' or mode::text = $4) order by started_at desc limit $5`, from, to, status, mode, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Event is a run_events row.
type Event struct {
	ID           int64           `json:"id"`
	RunID        uuid.UUID       `json:"run_id"`
	Seq          int             `json:"seq"`
	Type         string          `json:"type"`
	Visibility   string          `json:"visibility"`
	Step         *string         `json:"step"`
	Payload      json.RawMessage `json:"payload"`
	Model        *string         `json:"model"`
	InputTokens  *int64          `json:"input_tokens"`
	CachedTokens *int64          `json:"cached_tokens"`
	OutputTokens *int64          `json:"output_tokens"`
	CostMicros   *int64          `json:"cost_usd_micros"`
	LatencyMs    *int64          `json:"latency_ms"`
	CreatedAt    time.Time       `json:"created_at"`
}

// InsertEvent appends a run event.
func (q *Q) InsertEvent(ctx context.Context, e Event) error {
	_, err := q.db.Exec(ctx, `insert into run_events (run_id, seq, type, visibility, step, payload, model, input_tokens, cached_tokens, output_tokens, cost_usd_micros, latency_ms, created_at)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		e.RunID, e.Seq, e.Type, e.Visibility, e.Step, []byte(e.Payload), e.Model, e.InputTokens, e.CachedTokens, e.OutputTokens, e.CostMicros, e.LatencyMs, e.CreatedAt)
	return err
}

// ListEvents returns events with seq > afterSeq; publicOnly filters to the
// events the SSE stream may show.
func (q *Q) ListEvents(ctx context.Context, runID uuid.UUID, afterSeq int, publicOnly bool, limit int) ([]Event, error) {
	rows, err := q.db.Query(ctx, `select id, run_id, seq, type, visibility, step, payload, model, input_tokens, cached_tokens, output_tokens, cost_usd_micros, latency_ms, created_at
		from run_events where run_id = $1 and seq > $2 and (not $3 or visibility = 'public') order by seq limit $4`, runID, afterSeq, publicOnly, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var payload []byte
		if err := rows.Scan(&e.ID, &e.RunID, &e.Seq, &e.Type, &e.Visibility, &e.Step, &payload, &e.Model, &e.InputTokens, &e.CachedTokens, &e.OutputTokens, &e.CostMicros, &e.LatencyMs, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Payload = payload
		out = append(out, e)
	}
	return out, rows.Err()
}

// EnsureEventPartition creates the monthly partition for t.
func (q *Q) EnsureEventPartition(ctx context.Context, t time.Time) error {
	_, err := q.db.Exec(ctx, `select kora_ensure_run_events_partition($1::date)`, t)
	return err
}

// InsertArtifact stores a full tool result outside the model context.
func (q *Q) InsertArtifact(ctx context.Context, id, runID uuid.UUID, tool string, args, result any, digest string) error {
	a, _ := json.Marshal(args)
	r, _ := json.Marshal(result)
	_, err := q.db.Exec(ctx, `insert into tool_artifacts (id, run_id, tool_name, args, result, digest) values ($1,$2,$3,$4,$5,$6)`, id, runID, tool, a, r, digest)
	return err
}

// Artifact is a tool_artifacts row.
type Artifact struct {
	ID        uuid.UUID       `json:"id"`
	RunID     uuid.UUID       `json:"run_id"`
	ToolName  string          `json:"tool_name"`
	Args      json.RawMessage `json:"args"`
	Result    json.RawMessage `json:"result"`
	Digest    string          `json:"digest"`
	CreatedAt time.Time       `json:"created_at"`
}

// GetArtifact loads one artifact of a run.
func (q *Q) GetArtifact(ctx context.Context, runID, id uuid.UUID) (Artifact, error) {
	var a Artifact
	var args, res []byte
	err := q.db.QueryRow(ctx, `select id, run_id, tool_name, args, result, digest, created_at from tool_artifacts where id = $1 and run_id = $2`, id, runID).
		Scan(&a.ID, &a.RunID, &a.ToolName, &args, &res, &a.Digest, &a.CreatedAt)
	a.Args, a.Result = args, res
	return a, notFound(err)
}

// InsertTripRequest stores the immutable slot snapshot of a run.
func (q *Q) InsertTripRequest(ctx context.Context, convID, runID uuid.UUID, r domain.TripRequest) error {
	var dest, cur any
	var amount, idr any
	if r.Destination != nil {
		dest = r.Destination.Code
	}
	if r.Budget != nil {
		amount, cur = r.Budget.Amount, r.Budget.Currency
	}
	if r.BudgetIDR != nil {
		idr = *r.BudgetIDR
	}
	if r.Destination == nil && r.Budget == nil {
		// The table requires one of the two; budget_to_destination always has
		// a budget, plan_trip always a destination. Info runs are not stored.
		return nil
	}
	win, _ := json.Marshal(map[string]any{"window": r.Window, "window_start": r.WindowStart, "window_end": r.WindowEnd, "live_window_end": r.LiveWindowEnd})
	sp, _ := json.Marshal(r.Special)
	as, _ := json.Marshal(r.Assumptions)
	interests := r.Interests
	if interests == nil {
		interests = []string{}
	}
	var nights any
	if r.Nights > 0 {
		nights = r.Nights
	}
	_, err := q.db.Exec(ctx, `insert into trip_requests (id, conversation_id, run_id, origin_code, destination_code, budget_amount, budget_currency, budget_idr,
		date_from, date_to, date_window, nights, pax_adults, pax_children, interests, special_requests, assumptions)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		r.ID, convID, runID, r.Origin.Code, dest, amount, cur, idr, dateOrNil(r.DepartDate.String()), dateOrNil(r.ReturnDate.String()),
		win, nights, r.Pax.Adults, r.Pax.Children, interests, sp, as)
	return err
}

// LatestRunID returns the newest run of a conversation.
func (q *Q) LatestRunID(ctx context.Context, convID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := q.db.QueryRow(ctx, `select id from runs where conversation_id = $1 order by started_at desc limit 1`, convID).Scan(&id)
	return id, notFound(err)
}
