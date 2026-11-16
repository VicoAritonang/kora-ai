package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koraai/koraai/internal/domain"
)

// Conversation is a conversations row.
type Conversation struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	Title             *string
	TripState         domain.TripState
	TripStateVersion  int
	Summary           *string
	SummaryUptoSeq    int64
	SelectedPackageID *uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
	LastMessageAt     *time.Time
}

const convCols = `id, user_id, title, trip_state, trip_state_version, summary, summary_upto_seq, selected_package_id, created_at, updated_at, last_message_at`

func scanConv(row interface{ Scan(...any) error }) (Conversation, error) {
	var c Conversation
	var ts []byte
	var sel uuid.NullUUID
	err := row.Scan(&c.ID, &c.UserID, &c.Title, &ts, &c.TripStateVersion, &c.Summary, &c.SummaryUptoSeq, &sel, &c.CreatedAt, &c.UpdatedAt, &c.LastMessageAt)
	if err != nil {
		return c, err
	}
	if sel.Valid {
		c.SelectedPackageID = &sel.UUID
	}
	_ = json.Unmarshal(ts, &c.TripState)
	c.TripState.Version = c.TripStateVersion
	c.TripState.Normalize()
	return c, nil
}

// CreateConversation inserts an empty conversation.
func (q *Q) CreateConversation(ctx context.Context, id, userID uuid.UUID, title *string, state domain.TripState) (Conversation, error) {
	state.Normalize()
	b, _ := json.Marshal(state)
	row := q.db.QueryRow(ctx, `insert into conversations (id, user_id, title, trip_state) values ($1,$2,$3,$4) returning `+convCols, id, userID, title, b)
	return scanConv(row)
}

// GetConversation loads a conversation owned by userID (else ErrNotFound,
// never revealing it exists — MODULE_SPEC §11).
func (q *Q) GetConversation(ctx context.Context, userID, id uuid.UUID) (Conversation, error) {
	c, err := scanConv(q.db.QueryRow(ctx, `select `+convCols+` from conversations where id = $1 and user_id = $2`, id, userID))
	return c, notFound(err)
}

// GetConversationByID loads without an ownership check (pipeline, worker).
func (q *Q) GetConversationByID(ctx context.Context, id uuid.UUID) (Conversation, error) {
	c, err := scanConv(q.db.QueryRow(ctx, `select `+convCols+` from conversations where id = $1`, id))
	return c, notFound(err)
}

// ListConversations returns a page ordered by recent activity. The cursor is
// the created_at|id of the last row.
func (q *Q) ListConversations(ctx context.Context, userID uuid.UUID, before *time.Time, limit int) ([]Conversation, error) {
	var rows pgx.Rows
	var err error
	if before == nil {
		rows, err = q.db.Query(ctx, `select `+convCols+` from conversations where user_id = $1
			order by coalesce(last_message_at, created_at) desc limit $2`, userID, limit)
	} else {
		rows, err = q.db.Query(ctx, `select `+convCols+` from conversations where user_id = $1 and coalesce(last_message_at, created_at) < $2
			order by coalesce(last_message_at, created_at) desc limit $3`, userID, *before, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Conversation
	for rows.Next() {
		c, err := scanConv(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateTitle renames a conversation.
func (q *Q) UpdateTitle(ctx context.Context, userID, id uuid.UUID, title string) error {
	tag, err := q.db.Exec(ctx, `update conversations set title = $3, updated_at = now() where id = $1 and user_id = $2`, id, userID, title)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// SetTitleIfEmpty sets an automatic title once.
func (q *Q) SetTitleIfEmpty(ctx context.Context, id uuid.UUID, title string) error {
	_, err := q.db.Exec(ctx, `update conversations set title = $2 where id = $1 and (title is null or title = '')`, id, title)
	return err
}

// DeleteConversation removes a conversation and everything under it.
func (q *Q) DeleteConversation(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := q.db.Exec(ctx, `delete from conversations where id = $1 and user_id = $2`, id, userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// SaveTripState writes the state if the stored version still equals
// expected (optimistic locking) and returns the new version.
func (q *Q) SaveTripState(ctx context.Context, id uuid.UUID, state domain.TripState, expected int) (int, error) {
	state.Normalize()
	state.Version = expected + 1
	b, _ := json.Marshal(state)
	var v int
	err := q.db.QueryRow(ctx, `update conversations set trip_state = $2, trip_state_version = trip_state_version + 1, updated_at = now()
		where id = $1 and trip_state_version = $3 returning trip_state_version`, id, b, expected).Scan(&v)
	if err == pgx.ErrNoRows {
		return 0, ErrConflict
	}
	return v, err
}

// GuardSession loads the stored guard-rails session.
func (q *Q) GuardSession(ctx context.Context, id uuid.UUID) ([]byte, error) {
	var b []byte
	err := q.db.QueryRow(ctx, `select guard_session from conversations where id = $1`, id).Scan(&b)
	return b, notFound(err)
}

// SaveGuardSession stores the guard-rails session (never from the client).
func (q *Q) SaveGuardSession(ctx context.Context, id uuid.UUID, sess []byte) error {
	if len(sess) == 0 {
		return nil
	}
	_, err := q.db.Exec(ctx, `update conversations set guard_session = $2 where id = $1`, id, sess)
	return err
}

// SetSelectedPackage marks a package as selected on the conversation.
func (q *Q) SetSelectedPackage(ctx context.Context, convID, pkgID uuid.UUID) error {
	_, err := q.db.Exec(ctx, `update conversations set selected_package_id = $2, updated_at = now() where id = $1`, convID, pkgID)
	return err
}

// SaveSummary stores the rolling summary.
func (q *Q) SaveSummary(ctx context.Context, id uuid.UUID, summary string, uptoSeq int64) error {
	_, err := q.db.Exec(ctx, `update conversations set summary = $2, summary_upto_seq = $3 where id = $1 and summary_upto_seq < $3`, id, summary, uptoSeq)
	return err
}

// Message is a messages row.
type Message struct {
	ID             uuid.UUID       `json:"id"`
	ConversationID uuid.UUID       `json:"conversation_id"`
	Seq            int64           `json:"seq"`
	Role           string          `json:"role"`
	Content        string          `json:"content"`
	Blocks         json.RawMessage `json:"blocks,omitempty"`
	RunID          *uuid.UUID      `json:"run_id,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

const msgCols = `id, conversation_id, seq, role, content, blocks, run_id, created_at`

func scanMsg(row interface{ Scan(...any) error }) (Message, error) {
	var m Message
	var blocks []byte
	var run uuid.NullUUID
	err := row.Scan(&m.ID, &m.ConversationID, &m.Seq, &m.Role, &m.Content, &blocks, &run, &m.CreatedAt)
	if run.Valid {
		m.RunID = &run.UUID
	}
	if len(blocks) > 0 {
		m.Blocks = blocks
	}
	return m, err
}

// InsertMessage appends a message with the next sequence number. Callers run
// it in a transaction so seq stays unique per conversation.
func (q *Q) InsertMessage(ctx context.Context, id, convID uuid.UUID, role, content string, blocks any, runID *uuid.UUID) (Message, error) {
	var b []byte
	if blocks != nil {
		b, _ = json.Marshal(blocks)
	}
	row := q.db.QueryRow(ctx, `
		with next as (select coalesce(max(seq), 0) + 1 as seq from messages where conversation_id = $2)
		insert into messages (id, conversation_id, seq, role, content, blocks, run_id)
		select $1, $2, next.seq, $3, $4, $5, $6 from next
		returning `+msgCols, id, convID, role, content, b, runID)
	m, err := scanMsg(row)
	if err != nil {
		return m, err
	}
	_, err = q.db.Exec(ctx, `update conversations set last_message_at = now(), updated_at = now() where id = $1`, convID)
	return m, err
}

// SetMessageRun links a user message to the run it started.
func (q *Q) SetMessageRun(ctx context.Context, msgID, runID uuid.UUID) error {
	_, err := q.db.Exec(ctx, `update messages set run_id = $2 where id = $1`, msgID, runID)
	return err
}

// ListMessages returns messages after afterSeq in order.
func (q *Q) ListMessages(ctx context.Context, convID uuid.UUID, afterSeq int64, limit int) ([]Message, error) {
	rows, err := q.db.Query(ctx, `select `+msgCols+` from messages where conversation_id = $1 and seq > $2 order by seq limit $3`, convID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMsg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// LastMessages returns the last n messages in chronological order.
func (q *Q) LastMessages(ctx context.Context, convID uuid.UUID, n int) ([]Message, error) {
	rows, err := q.db.Query(ctx, `select * from (select `+msgCols+` from messages where conversation_id = $1 order by seq desc limit $2) t order by seq`, convID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMsg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetMessage loads one message.
func (q *Q) GetMessage(ctx context.Context, id uuid.UUID) (Message, error) {
	m, err := scanMsg(q.db.QueryRow(ctx, `select `+msgCols+` from messages where id = $1`, id))
	return m, notFound(err)
}

// MessageOwner returns the user owning a message's conversation.
func (q *Q) MessageOwner(ctx context.Context, msgID uuid.UUID) (uuid.UUID, error) {
	var u uuid.UUID
	err := q.db.QueryRow(ctx, `select c.user_id from messages m join conversations c on c.id = m.conversation_id where m.id = $1`, msgID).Scan(&u)
	return u, notFound(err)
}

// ConversationsNeedingSummary returns conversations whose unsummarized turns
// outside the recent window exceed threshold.
func (q *Q) ConversationsNeedingSummary(ctx context.Context, window, threshold int, limit int) ([]uuid.UUID, error) {
	rows, err := q.db.Query(ctx, `
		select c.id from conversations c
		where (select count(*) from messages m where m.conversation_id = c.id and m.seq > c.summary_upto_seq) > $1 + $2
		order by c.last_message_at desc nulls last limit $3`, window, threshold, limit)
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
