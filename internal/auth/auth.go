// Package auth handles guest sessions, magic-link login, guest → account
// merge, session rotation and CSRF tokens. Tokens are stored only as SHA-256
// hashes (ENGINEERING_STANDARDS §7.1).
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/store/postgres"
	"github.com/koraai/koraai/internal/usage"
)

// Errors.
var (
	ErrUnauthenticated = errors.New("auth: unauthenticated")
	ErrInvalidToken    = errors.New("auth: invalid or used token")
	ErrInvalidEmail    = errors.New("auth: invalid email")
	ErrRateLimited     = errors.New("auth: rate limited")
)

// SessionTTL is the cookie lifetime.
const SessionTTL = 30 * 24 * time.Hour

// Service is the auth module.
type Service struct {
	DB        *postgres.DB
	Email     ports.EmailSender
	Limiter   *usage.Limiter
	Clock     platform.Clock
	Log       *slog.Logger
	Key       []byte // CSRF HMAC key
	WebOrigin string
	GuestPerIPHour int
}

func newToken() (string, []byte) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	t := base64.RawURLEncoding.EncodeToString(b)
	return t, hash(t)
}

func hash(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}

// HashIP hashes a client IP for storage.
func HashIP(ip string) []byte {
	h := sha256.Sum256([]byte("ip:" + ip))
	return h[:16]
}

// CreateGuest creates a guest user and session, rate limited per IP.
func (s *Service) CreateGuest(ctx context.Context, ua, ip string) (string, postgres.User, error) {
	if s.Limiter != nil && s.GuestPerIPHour > 0 && !s.Limiter.Allow(ctx, "guest:"+base64.RawURLEncoding.EncodeToString(HashIP(ip)), s.GuestPerIPHour, time.Hour) {
		return "", postgres.User{}, ErrRateLimited
	}
	u, err := s.DB.Q().CreateUser(ctx, platform.NewID(), nil, true)
	if err != nil {
		return "", u, err
	}
	tok, err := s.newSession(ctx, u.ID, ua, ip)
	return tok, u, err
}

func (s *Service) newSession(ctx context.Context, userID uuid.UUID, ua, ip string) (string, error) {
	tok, h := newToken()
	if len(ua) > 300 {
		ua = ua[:300]
	}
	err := s.DB.Q().CreateSession(ctx, platform.NewID(), userID, h, ua, HashIP(ip), s.Clock.Now().Add(SessionTTL))
	return tok, err
}

// Authenticate resolves a cookie token to a live session.
func (s *Service) Authenticate(ctx context.Context, token string) (postgres.Session, error) {
	if token == "" {
		return postgres.Session{}, ErrUnauthenticated
	}
	sess, err := s.DB.Q().SessionByToken(ctx, hash(token))
	if errors.Is(err, postgres.ErrNotFound) {
		return sess, ErrUnauthenticated
	}
	return sess, err
}

// RequestMagicLink emails a login link. It always succeeds from the caller's
// point of view (no account enumeration) except for malformed emails.
func (s *Service) RequestMagicLink(ctx context.Context, email string) error {
	addr, err := mail.ParseAddress(strings.TrimSpace(email))
	if err != nil || len(addr.Address) > 254 {
		return ErrInvalidEmail
	}
	e := strings.ToLower(addr.Address)
	n, err := s.DB.Q().CountMagicTokensSince(ctx, e, s.Clock.Now().Add(-time.Hour))
	if err != nil {
		return err
	}
	if n >= 5 {
		return nil // silently drop: same response either way
	}
	tok, h := newToken()
	if err := s.DB.Q().CreateMagicToken(ctx, platform.NewID(), e, h, s.Clock.Now().Add(15*time.Minute)); err != nil {
		return err
	}
	link := fmt.Sprintf("%s/auth/verify?token=%s", strings.TrimRight(s.WebOrigin, "/"), tok)
	body := "Halo,\n\nKlik tautan berikut untuk masuk ke Kora (berlaku 15 menit, sekali pakai):\n\n" + link + "\n\nAbaikan email ini bila Anda tidak memintanya."
	return s.Email.Send(ctx, e, "Tautan masuk Kora", body)
}

// VerifyMagicLink exchanges a token for a new session. A current guest is
// promoted (new email) or merged into the existing account (MODULE_SPEC §14).
// The old session is revoked: the session id rotates at login.
func (s *Service) VerifyMagicLink(ctx context.Context, token string, current *postgres.Session, ua, ip string) (string, postgres.User, error) {
	var user postgres.User
	var newTok string
	err := s.DB.WithTx(ctx, func(q *postgres.Q) error {
		email, err := q.ConsumeMagicToken(ctx, hash(token))
		if errors.Is(err, postgres.ErrNotFound) {
			return ErrInvalidToken
		}
		if err != nil {
			return err
		}
		existing, err := q.GetUserByEmail(ctx, email)
		switch {
		case err == nil:
			user = existing
			if current != nil && current.User.IsGuest && current.UserID != existing.ID {
				if err := q.MergeGuestInto(ctx, current.UserID, existing.ID); err != nil {
					return err
				}
			}
		case errors.Is(err, postgres.ErrNotFound):
			if current != nil && current.User.IsGuest {
				if user, err = q.PromoteGuest(ctx, current.UserID, email); err != nil {
					return err
				}
			} else if user, err = q.CreateUser(ctx, platform.NewID(), &email, false); err != nil {
				return err
			}
		default:
			return err
		}
		if err := q.UpsertIdentity(ctx, platform.NewID(), user.ID, "magic_link", email); err != nil {
			return err
		}
		if current != nil {
			if err := q.RevokeSession(ctx, current.ID); err != nil {
				return err
			}
		}
		tok, h := newToken()
		newTok = tok
		return q.CreateSession(ctx, platform.NewID(), user.ID, h, ua, HashIP(ip), s.Clock.Now().Add(SessionTTL))
	})
	return newTok, user, err
}

// Logout revokes the session.
func (s *Service) Logout(ctx context.Context, sessionID uuid.UUID) error {
	return s.DB.Q().RevokeSession(ctx, sessionID)
}

// CSRFToken derives the CSRF token bound to a session.
func (s *Service) CSRFToken(sessionID uuid.UUID) string {
	m := hmac.New(sha256.New, s.Key)
	m.Write(sessionID[:])
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// VerifyCSRF checks a submitted CSRF token in constant time.
func (s *Service) VerifyCSRF(sessionID uuid.UUID, token string) bool {
	return token != "" && hmac.Equal([]byte(token), []byte(s.CSRFToken(sessionID)))
}

// DeleteAccount starts deletion: mark, revoke all sessions. Rows are removed
// by the retention job after the grace period.
func (s *Service) DeleteAccount(ctx context.Context, userID uuid.UUID) error {
	if err := s.DB.Q().MarkUserDeleted(ctx, userID); err != nil {
		return err
	}
	return s.DB.Q().RevokeUserSessions(ctx, userID)
}
