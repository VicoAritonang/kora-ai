// Package platform holds the small cross-cutting utilities every module uses:
// an injectable clock, UUIDv7 generation, structured logging, and a resilient
// HTTP client. It imports no other internal package.
package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
)

// Clock is injected everywhere logic needs the current time, so tests are not
// tied to the wall clock (ENGINEERING_STANDARDS §1.1).
type Clock interface {
	Now() time.Time
}

// SystemClock reads the real UTC time.
type SystemClock struct{}

// Now returns the current UTC time.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// FixedClock always returns T. Used in tests and the eval runner.
type FixedClock struct{ T time.Time }

// Now returns the fixed time.
func (c FixedClock) Now() time.Time { return c.T }

// NewID returns a UUIDv7: time-ordered, which keeps B-tree indexes compact.
func NewID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		// NewV7 only fails when the system random source fails; nothing
		// sensible can continue without randomness.
		panic("platform: uuid v7: " + err.Error())
	}
	return id
}

// Jakarta is the time zone daily quotas reset in (MODULE_SPEC §14).
var Jakarta = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return time.FixedZone("WIB", 7*3600)
	}
	return loc
}()

// NewLogger builds the JSON slog logger. Level is one of debug|info|warn|error.
func NewLogger(level string) *slog.Logger {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(&ctxHandler{slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l})})
}

type ctxKey int

const (
	keyRequestID ctxKey = iota
	keyRunID
	keyConversationID
	keyUserHash
)

// WithRequestID stores the request id for log correlation.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyRequestID, id)
}

// RequestID returns the request id stored in ctx, if any.
func RequestID(ctx context.Context) string {
	s, _ := ctx.Value(keyRequestID).(string)
	return s
}

// WithRun stores run and conversation ids for log correlation.
func WithRun(ctx context.Context, runID, convID uuid.UUID) context.Context {
	ctx = context.WithValue(ctx, keyRunID, runID.String())
	return context.WithValue(ctx, keyConversationID, convID.String())
}

// WithUser stores a hash of the user id; raw ids never reach logs.
func WithUser(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, keyUserHash, HashUser(userID))
}

// HashUser returns a short, stable, non-reversible id for logs.
func HashUser(id uuid.UUID) string {
	sum := sha256.Sum256(id[:])
	return hex.EncodeToString(sum[:6])
}

// ctxHandler adds correlation ids from the context to every record.
type ctxHandler struct{ slog.Handler }

func (h *ctxHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, k := range []struct {
		key  ctxKey
		name string
	}{{keyRequestID, "request_id"}, {keyRunID, "run_id"}, {keyConversationID, "conversation_id"}, {keyUserHash, "user_hash"}} {
		if v, ok := ctx.Value(k.key).(string); ok && v != "" {
			r.AddAttrs(slog.String(k.name, v))
		}
	}
	return h.Handler.Handle(ctx, r)
}

func (h *ctxHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ctxHandler{h.Handler.WithAttrs(attrs)}
}

func (h *ctxHandler) WithGroup(name string) slog.Handler {
	return &ctxHandler{h.Handler.WithGroup(name)}
}

// EstimateTokens approximates token count for budgeting context layers. It is
// deliberately conservative (about 3.5 characters per token for mixed
// Indonesian/English text); exact counts come back from the provider's usage.
func EstimateTokens(s string) int {
	n := 0
	for range s {
		n++
	}
	return n*2/7 + 1
}

// Go runs fn in a goroutine that recovers panics and logs them, so a bug in a
// background task cannot crash the process (ENGINEERING_STANDARDS §1.1).
func Go(log *slog.Logger, name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("goroutine panic", "task", name, "panic", r)
			}
		}()
		fn()
	}()
}
