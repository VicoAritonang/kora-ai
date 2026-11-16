// Package conversation manages conversations and messages and creates runs
// (MODULE_SPEC §11). It hands runs to the pipeline and never runs the agent
// itself.
package conversation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/agent/prompts"
	"github.com/koraai/koraai/internal/config"
	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/pipeline"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/session"
	"github.com/koraai/koraai/internal/store/postgres"
	"github.com/koraai/koraai/internal/usage"
)

// Errors mapped to HTTP codes by the transport.
var (
	ErrNotFound            = errors.New("not found")
	ErrRunInProgress       = errors.New("run in progress")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
	ErrQuota               = errors.New("quota exhausted")
	ErrCapacity            = errors.New("capacity exhausted")
	ErrValidation          = errors.New("validation failed")
	ErrStateConflict       = errors.New("state conflict")
)

// Runner is the pipeline surface this module needs.
type Runner interface {
	Start(ctx context.Context, in pipeline.RunInput) error
	Cancel(ctx context.Context, runID uuid.UUID) error
	Capacity() bool
}

// Service is the conversation module.
type Service struct {
	DB      *postgres.DB
	Runner  Runner
	Limiter *usage.Limiter
	Cfg     *config.Config
	Guard   ports.Guard
	Clock   platform.Clock
}

// Create starts an empty conversation.
func (s *Service) Create(ctx context.Context, userID uuid.UUID) (postgres.Conversation, error) {
	return s.DB.Q().CreateConversation(ctx, platform.NewID(), userID, nil, domain.TripState{})
}

// List returns a page of conversations; cursor is an RFC 3339 time.
func (s *Service) List(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]postgres.Conversation, string, error) {
	var before *time.Time
	if cursor != "" {
		if t, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
			before = &t
		}
	}
	items, err := s.DB.Q().ListConversations(ctx, userID, before, limit)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) == limit {
		last := items[len(items)-1]
		t := last.CreatedAt
		if last.LastMessageAt != nil {
			t = *last.LastMessageAt
		}
		next = t.Format(time.RFC3339Nano)
	}
	return items, next, nil
}

// Detail is a conversation with its active run.
type Detail struct {
	Conversation postgres.Conversation
	ActiveRun    *postgres.Run
}

// Get loads a conversation the user owns.
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (Detail, error) {
	c, err := s.DB.Q().GetConversation(ctx, userID, id)
	if errors.Is(err, postgres.ErrNotFound) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, err
	}
	run, err := s.DB.Q().ActiveRun(ctx, id)
	return Detail{c, run}, err
}

// Rename sets the title.
func (s *Service) Rename(ctx context.Context, userID, id uuid.UUID, title string) error {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 120 {
		return ErrValidation
	}
	err := s.DB.Q().UpdateTitle(ctx, userID, id, title)
	if errors.Is(err, postgres.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// Delete removes a conversation.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	err := s.DB.Q().DeleteConversation(ctx, userID, id)
	if errors.Is(err, postgres.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// Messages returns the history after a sequence number.
func (s *Service) Messages(ctx context.Context, userID, id uuid.UUID, afterSeq int64, limit int) ([]postgres.Message, error) {
	if _, err := s.DB.Q().GetConversation(ctx, userID, id); err != nil {
		if errors.Is(err, postgres.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.DB.Q().ListMessages(ctx, id, afterSeq, limit)
}

// PostResult is the 202 body.
type PostResult struct {
	Message postgres.Message `json:"message"`
	Run     postgres.Run     `json:"run"`
}

// PostMessage stores the message, creates the run and starts it. In one
// transaction: idempotency → no active run → insert message → insert run.
func (s *Service) PostMessage(ctx context.Context, user postgres.User, convID uuid.UUID, content, idemKey string) (PostResult, error) {
	return s.post(ctx, user, convID, content, idemKey, nil)
}

func (s *Service) post(ctx context.Context, user postgres.User, convID uuid.UUID, content, idemKey string, blocks any) (PostResult, error) {
	content = strings.TrimSpace(content)
	if n := utf8.RuneCountInString(content); n < 1 || n > 4000 {
		return PostResult{}, ErrValidation
	}
	if idemKey == "" || len(idemKey) > 200 {
		return PostResult{}, ErrValidation
	}
	q := s.DB.Q()
	if _, err := q.GetConversation(ctx, user.ID, convID); err != nil {
		if errors.Is(err, postgres.ErrNotFound) {
			return PostResult{}, ErrNotFound
		}
		return PostResult{}, err
	}
	sum := sha256.Sum256([]byte(convID.String() + "\x00" + content))
	if prev, err := q.GetIdempotency(ctx, user.ID, idemKey); err == nil {
		if !bytes.Equal(prev.RequestHash, sum[:]) {
			return PostResult{}, ErrIdempotencyConflict
		}
		var r PostResult
		if json.Unmarshal(prev.Response, &r) == nil && r.Run.ID != uuid.Nil {
			return r, nil
		}
	}
	// Quota and capacity are checked before anything is written (§11).
	if s.Limiter.RunsRemaining(ctx, user.ID, user.IsGuest) <= 0 {
		return PostResult{}, ErrQuota
	}
	if !s.Runner.Capacity() {
		return PostResult{}, ErrCapacity
	}
	if n, err := q.CountActiveRunsForUser(ctx, user.ID); err == nil && n >= s.Cfg.Quota.MaxActiveRunsPerUser {
		return PostResult{}, ErrRunInProgress
	}
	snapshot := s.Cfg.Snapshot()
	snapshot["prompts"] = prompts.Versions()
	snapshot["guardrails_policy"] = s.Guard.PolicyVersion()

	var res PostResult
	err := s.DB.WithTx(ctx, func(tx *postgres.Q) error {
		active, err := tx.ActiveRun(ctx, convID)
		if err != nil {
			return err
		}
		if active != nil {
			return ErrRunInProgress
		}
		msg, err := tx.InsertMessage(ctx, platform.NewID(), convID, "user", content, blocks, nil)
		if err != nil {
			return err
		}
		run, err := tx.InsertRun(ctx, platform.NewID(), convID, msg.ID, snapshot)
		if errors.Is(err, postgres.ErrConflict) {
			return ErrRunInProgress
		}
		if err != nil {
			return err
		}
		if err := tx.SetMessageRun(ctx, msg.ID, run.ID); err != nil {
			return err
		}
		msg.RunID = &run.ID
		res = PostResult{Message: msg, Run: run}
		return tx.SaveIdempotency(ctx, user.ID, idemKey, sum[:], res)
	})
	if err != nil {
		return PostResult{}, err
	}
	s.Limiter.ConsumeRun(ctx, user.ID, user.IsGuest)
	if err := s.Runner.Start(ctx, pipeline.RunInput{RunID: res.Run.ID, ConversationID: convID, UserID: user.ID, MessageID: res.Message.ID}); err != nil {
		code := "capacity_exhausted"
		_ = q.FinishRun(context.WithoutCancel(ctx), res.Run.ID, "failed", &code, postgres.RunTotals{})
		return PostResult{}, ErrCapacity
	}
	return res, nil
}

// PatchTripState applies a correction with optimistic locking; it does not
// start a run (API_SPEC §5.2).
func (s *Service) PatchTripState(ctx context.Context, userID, convID uuid.UUID, version int, p domain.TripStatePatch) (domain.TripState, error) {
	if _, err := s.DB.Q().GetConversation(ctx, userID, convID); err != nil {
		return domain.TripState{}, ErrNotFound
	}
	st, err := (session.StateStore{DB: s.DB}).Apply(ctx, convID, p, version)
	if errors.Is(err, session.ErrStateConflict) {
		return st, ErrStateConflict
	}
	if err != nil && !errors.Is(err, postgres.ErrNotFound) {
		return st, errors.Join(ErrValidation, err)
	}
	return st, err
}

// Run returns a run the user owns.
func (s *Service) Run(ctx context.Context, userID, runID uuid.UUID) (postgres.Run, error) {
	r, err := s.DB.Q().GetRunForUser(ctx, userID, runID)
	if errors.Is(err, postgres.ErrNotFound) {
		return r, ErrNotFound
	}
	return r, err
}

// Cancel cancels a running run the user owns.
func (s *Service) Cancel(ctx context.Context, userID, runID uuid.UUID) error {
	r, err := s.Run(ctx, userID, runID)
	if err != nil {
		return err
	}
	if r.Status != "running" {
		return nil
	}
	return s.Runner.Cancel(ctx, runID)
}
