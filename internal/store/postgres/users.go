package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// User is a users row.
type User struct {
	ID            uuid.UUID  `json:"id"`
	Email         *string    `json:"email"`
	DisplayName   *string    `json:"display_name"`
	IsGuest       bool       `json:"is_guest"`
	HomeCityCode  *string    `json:"home_city_code"`
	Locale        string     `json:"locale"`
	MemoryEnabled bool       `json:"memory_enabled"`
	CreatedAt     time.Time  `json:"created_at"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
}

const userCols = `id, email::text, display_name, is_guest, home_city_code, locale, memory_enabled, created_at, deleted_at`

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.IsGuest, &u.HomeCityCode, &u.Locale, &u.MemoryEnabled, &u.CreatedAt, &u.DeletedAt)
	return u, err
}

// CreateUser inserts a user.
func (q *Q) CreateUser(ctx context.Context, id uuid.UUID, email *string, guest bool) (User, error) {
	row := q.db.QueryRow(ctx, `insert into users (id, email, is_guest, last_seen_at) values ($1, $2, $3, now()) returning `+userCols, id, email, guest)
	return scanUser(row)
}

// GetUser loads a user by id.
func (q *Q) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := scanUser(q.db.QueryRow(ctx, `select `+userCols+` from users where id = $1`, id))
	return u, notFound(err)
}

// GetUserByEmail loads a registered user by email.
func (q *Q) GetUserByEmail(ctx context.Context, email string) (User, error) {
	u, err := scanUser(q.db.QueryRow(ctx, `select `+userCols+` from users where email = $1 and deleted_at is null`, email))
	return u, notFound(err)
}

// UserPatch is a partial profile update.
type UserPatch struct {
	DisplayName   *string
	HomeCityCode  *string
	Locale        *string
	MemoryEnabled *bool
}

// UpdateUser applies a profile patch.
func (q *Q) UpdateUser(ctx context.Context, id uuid.UUID, p UserPatch) (User, error) {
	row := q.db.QueryRow(ctx, `
		update users set
			display_name   = coalesce($2, display_name),
			home_city_code = coalesce($3, home_city_code),
			locale         = coalesce($4, locale),
			memory_enabled = coalesce($5, memory_enabled),
			updated_at     = now()
		where id = $1 returning `+userCols, id, p.DisplayName, p.HomeCityCode, p.Locale, p.MemoryEnabled)
	u, err := scanUser(row)
	return u, notFound(err)
}

// PromoteGuest turns a guest into a registered user with an email.
func (q *Q) PromoteGuest(ctx context.Context, id uuid.UUID, email string) (User, error) {
	u, err := scanUser(q.db.QueryRow(ctx, `update users set email = $2, is_guest = false, updated_at = now() where id = $1 returning `+userCols, id, email))
	return u, notFound(err)
}

// TouchUser updates last_seen_at.
func (q *Q) TouchUser(ctx context.Context, id uuid.UUID) error {
	_, err := q.db.Exec(ctx, `update users set last_seen_at = now() where id = $1`, id)
	return err
}

// MarkUserDeleted starts the deletion grace period.
func (q *Q) MarkUserDeleted(ctx context.Context, id uuid.UUID) error {
	_, err := q.db.Exec(ctx, `update users set deleted_at = now(), updated_at = now() where id = $1`, id)
	return err
}

// MergeGuestInto moves a guest's conversations to the account and deletes
// the guest row (DATABASE_SCHEMA §4.1).
func (q *Q) MergeGuestInto(ctx context.Context, guestID, userID uuid.UUID) error {
	if guestID == userID {
		return nil
	}
	if _, err := q.db.Exec(ctx, `update conversations set user_id = $2 where user_id = $1`, guestID, userID); err != nil {
		return err
	}
	if _, err := q.db.Exec(ctx, `update reminders set user_id = $2 where user_id = $1`, guestID, userID); err != nil {
		return err
	}
	_, err := q.db.Exec(ctx, `delete from users where id = $1 and is_guest`, guestID)
	return err
}

// UpsertIdentity records a login identity.
func (q *Q) UpsertIdentity(ctx context.Context, id, userID uuid.UUID, provider, subject string) error {
	_, err := q.db.Exec(ctx, `insert into auth_identities (id, user_id, provider, provider_subject) values ($1,$2,$3,$4)
		on conflict (provider, provider_subject) do nothing`, id, userID, provider, subject)
	return err
}

// Session is an auth_sessions row joined with its user.
type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	ExpiresAt time.Time
	User      User
}

// CreateSession inserts a session.
func (q *Q) CreateSession(ctx context.Context, id, userID uuid.UUID, tokenHash []byte, ua string, ipHash []byte, expires time.Time) error {
	_, err := q.db.Exec(ctx, `insert into auth_sessions (id, user_id, token_hash, user_agent, ip_hash, expires_at) values ($1,$2,$3,$4,$5,$6)`,
		id, userID, tokenHash, ua, ipHash, expires)
	return err
}

// SessionByToken returns a live session for the token hash.
func (q *Q) SessionByToken(ctx context.Context, tokenHash []byte) (Session, error) {
	var s Session
	row := q.db.QueryRow(ctx, `
		select s.id, s.user_id, s.expires_at,
		       u.id, u.email::text, u.display_name, u.is_guest, u.home_city_code, u.locale, u.memory_enabled, u.created_at, u.deleted_at
		from auth_sessions s join users u on u.id = s.user_id
		where s.token_hash = $1 and s.revoked_at is null and s.expires_at > now() and u.deleted_at is null`, tokenHash)
	err := row.Scan(&s.ID, &s.UserID, &s.ExpiresAt,
		&s.User.ID, &s.User.Email, &s.User.DisplayName, &s.User.IsGuest, &s.User.HomeCityCode, &s.User.Locale, &s.User.MemoryEnabled, &s.User.CreatedAt, &s.User.DeletedAt)
	return s, notFound(err)
}

// RevokeSession revokes one session.
func (q *Q) RevokeSession(ctx context.Context, id uuid.UUID) error {
	_, err := q.db.Exec(ctx, `update auth_sessions set revoked_at = now() where id = $1`, id)
	return err
}

// RevokeUserSessions revokes every session of a user.
func (q *Q) RevokeUserSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := q.db.Exec(ctx, `update auth_sessions set revoked_at = now() where user_id = $1 and revoked_at is null`, userID)
	return err
}

// CreateMagicToken stores a hashed login token.
func (q *Q) CreateMagicToken(ctx context.Context, id uuid.UUID, email string, tokenHash []byte, expires time.Time) error {
	_, err := q.db.Exec(ctx, `insert into magic_link_tokens (id, email, token_hash, expires_at) values ($1,$2,$3,$4)`, id, email, tokenHash, expires)
	return err
}

// ConsumeMagicToken marks a token used and returns its email. A token is
// usable once (MODULE_SPEC §14).
func (q *Q) ConsumeMagicToken(ctx context.Context, tokenHash []byte) (string, error) {
	var email string
	err := q.db.QueryRow(ctx, `update magic_link_tokens set used_at = now()
		where token_hash = $1 and used_at is null and expires_at > now() returning email::text`, tokenHash).Scan(&email)
	return email, notFound(err)
}

// CountMagicTokensSince counts tokens issued to email since t (rate limit).
func (q *Q) CountMagicTokensSince(ctx context.Context, email string, since time.Time) (int, error) {
	var n int
	err := q.db.QueryRow(ctx, `select count(*) from magic_link_tokens where email = $1 and created_at > $2`, email, since).Scan(&n)
	return n, err
}
