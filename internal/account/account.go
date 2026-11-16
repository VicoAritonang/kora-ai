// Package account serves profile, data export, feedback, memory listing and
// the read-only run log for AI Lab, so the HTTP layer never touches the store
// directly (rule D7).
package account

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/reference"
	"github.com/koraai/koraai/internal/store/postgres"
)

// Errors.
var (
	ErrValidation = errors.New("validation failed")
	ErrNotFound   = errors.New("not found")
)

// Service is the account module.
type Service struct {
	DB       *postgres.DB
	Memory   ports.UserMemory
	MemoryOn bool
}

// Patch is a profile update.
type Patch struct {
	DisplayName   *string `json:"display_name"`
	HomeCityCode  *string `json:"home_city_code"`
	Locale        *string `json:"locale"`
	MemoryEnabled *bool   `json:"memory_enabled"`
}

// Update validates and applies a profile patch.
func (s *Service) Update(ctx context.Context, userID uuid.UUID, p Patch) (postgres.User, error) {
	if p.HomeCityCode != nil {
		if _, ok := reference.OriginCity(*p.HomeCityCode); !ok {
			return postgres.User{}, ErrValidation
		}
	}
	if p.Locale != nil && *p.Locale != "id" && *p.Locale != "en" {
		return postgres.User{}, ErrValidation
	}
	if p.DisplayName != nil && len(*p.DisplayName) > 80 {
		return postgres.User{}, ErrValidation
	}
	return s.DB.Q().UpdateUser(ctx, userID, postgres.UserPatch{DisplayName: p.DisplayName, HomeCityCode: p.HomeCityCode, Locale: p.Locale, MemoryEnabled: p.MemoryEnabled})
}

// Export returns every record the user owns (UU PDP).
func (s *Service) Export(ctx context.Context, userID uuid.UUID) (map[string]json.RawMessage, error) {
	return s.DB.Q().ExportUser(ctx, userID)
}

// Feedback stores a rating on a message or package the user owns.
func (s *Service) Feedback(ctx context.Context, userID uuid.UUID, msgID, pkgID *uuid.UUID, rating int, reason, comment *string) error {
	if (msgID == nil && pkgID == nil) || (rating != 1 && rating != -1) {
		return ErrValidation
	}
	if reason != nil {
		switch *reason {
		case "price_wrong", "not_relevant", "too_expensive", "missing_info", "other":
		default:
			return ErrValidation
		}
	}
	if comment != nil && len(*comment) > 2000 {
		return ErrValidation
	}
	if msgID != nil {
		owner, err := s.DB.Q().MessageOwner(ctx, *msgID)
		if err != nil || owner != userID {
			return ErrNotFound
		}
	}
	if pkgID != nil {
		_, m, err := s.DB.Q().GetPackage(ctx, *pkgID)
		if err != nil || m.UserID != userID {
			return ErrNotFound
		}
	}
	return s.DB.Q().InsertFeedback(ctx, platform.NewID(), userID, msgID, pkgID, rating, reason, comment)
}

// MemoryFacts lists remembered facts (empty while memory is disabled).
func (s *Service) MemoryFacts(ctx context.Context, user postgres.User) ([]ports.MemoryFact, bool, error) {
	if !s.MemoryOn || !user.MemoryEnabled || user.IsGuest {
		return []ports.MemoryFact{}, false, nil
	}
	f, err := s.Memory.List(ctx, user.ID)
	if errors.Is(err, ports.ErrNotSupported) {
		return []ports.MemoryFact{}, true, err
	}
	return f, true, err
}

// ForgetMemory deletes the user's memory.
func (s *Service) ForgetMemory(ctx context.Context, userID uuid.UUID) error {
	return s.Memory.Forget(ctx, userID)
}

// ---------------------------------------------------------- run log (AI Lab)

// Runs lists runs for the internal API.
func (s *Service) Runs(ctx context.Context, from, to time.Time, status, mode string, limit int) ([]postgres.Run, error) {
	return s.DB.Q().ListRuns(ctx, from, to, status, mode, limit)
}

// Run returns one run with its config snapshot.
func (s *Service) Run(ctx context.Context, id uuid.UUID) (postgres.Run, error) {
	return s.DB.Q().GetRun(ctx, id)
}

// Events returns every event of a run, internal ones included.
func (s *Service) Events(ctx context.Context, id uuid.UUID) ([]postgres.Event, error) {
	return s.DB.Q().ListEvents(ctx, id, 0, false, 10_000)
}

// PublicEvents returns events after seq that the SSE stream may show.
func (s *Service) PublicEvents(ctx context.Context, id uuid.UUID, after int) ([]postgres.Event, error) {
	return s.DB.Q().ListEvents(ctx, id, after, true, 500)
}

// Artifact returns a full tool result.
func (s *Service) Artifact(ctx context.Context, runID, id uuid.UUID) (postgres.Artifact, error) {
	return s.DB.Q().GetArtifact(ctx, runID, id)
}

// Ready checks the database.
func (s *Service) Ready(ctx context.Context) error { return s.DB.Ping(ctx) }
