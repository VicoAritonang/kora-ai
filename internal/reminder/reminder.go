// Package reminder creates and sends reminders: "booking window open" for
// trips beyond the sales window, and "re-check price" (MODULE_SPEC §15).
package reminder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/store/postgres"
)

// Errors.
var (
	ErrGuest      = errors.New("reminders need a registered account")
	ErrNotFound   = errors.New("not found")
	ErrValidation = errors.New("validation failed")
)

// MaxAttempts before a reminder is marked failed.
const MaxAttempts = 4

// Service is the reminder module.
type Service struct {
	DB        *postgres.DB
	Email     ports.EmailSender
	Clock     platform.Clock
	Log       *slog.Logger
	Window    int // booking window days
	WebOrigin string
}

// Create schedules a reminder for a conversation's trip.
func (s *Service) Create(ctx context.Context, user postgres.User, convID uuid.UUID, kind string) (postgres.Reminder, error) {
	if user.IsGuest {
		return postgres.Reminder{}, ErrGuest
	}
	c, err := s.DB.Q().GetConversation(ctx, user.ID, convID)
	if err != nil {
		return postgres.Reminder{}, ErrNotFound
	}
	now := s.Clock.Now()
	var trigger time.Time
	payload := map[string]any{"title": c.Title}
	switch kind {
	case "booking_window_open":
		w := c.TripState.DateWindow
		if w == nil {
			return postgres.Reminder{}, ErrValidation
		}
		start, _, err := w.Range()
		if err != nil {
			return postgres.Reminder{}, ErrValidation
		}
		trigger = start.AddDays(-s.Window).Time().Add(2 * time.Hour)
		if trigger.Before(now) {
			trigger = now.Add(time.Hour)
		}
		payload["window_start"] = start.String()
	case "price_recheck":
		trigger = now.Add(72 * time.Hour)
	default:
		return postgres.Reminder{}, ErrValidation
	}
	b, _ := json.Marshal(payload)
	cid := convID
	return s.DB.Q().InsertReminder(ctx, postgres.Reminder{ID: platform.NewID(), UserID: user.ID, ConversationID: &cid, Kind: kind, Payload: b, TriggerAt: trigger})
}

// List returns the user's reminders.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]postgres.Reminder, error) {
	return s.DB.Q().ListReminders(ctx, userID)
}

// Cancel cancels a pending reminder.
func (s *Service) Cancel(ctx context.Context, userID, id uuid.UUID) error {
	if err := s.DB.Q().CancelReminder(ctx, userID, id); errors.Is(err, postgres.ErrNotFound) {
		return ErrNotFound
	} else {
		return err
	}
}

// SendDue sends due reminders; failures retry with exponential backoff.
func (s *Service) SendDue(ctx context.Context) (int, error) {
	sent := 0
	err := s.DB.WithTx(ctx, func(q *postgres.Q) error {
		due, err := q.DueReminders(ctx, s.Clock.Now(), 50)
		if err != nil {
			return err
		}
		for _, r := range due {
			email, err := q.UserEmail(ctx, r.UserID)
			if err != nil || email == nil {
				_ = q.MarkReminder(ctx, r.ID, false, nil, 0)
				continue
			}
			var p map[string]any
			_ = json.Unmarshal(r.Payload, &p)
			subject, body := text(r, p, s.WebOrigin)
			if err := s.Email.Send(ctx, *email, subject, body); err != nil {
				retry := s.Clock.Now().Add(time.Duration(1<<r.Attempts) * 15 * time.Minute)
				_ = q.MarkReminder(ctx, r.ID, false, &retry, MaxAttempts)
				s.Log.Warn("reminder email failed", "err", err)
				continue
			}
			_ = q.MarkReminder(ctx, r.ID, true, nil, MaxAttempts)
			sent++
		}
		return nil
	})
	return sent, err
}

func text(r postgres.Reminder, p map[string]any, web string) (string, string) {
	link := web
	if r.ConversationID != nil {
		link = fmt.Sprintf("%s/c/%s", web, r.ConversationID)
	}
	if r.Kind == "booking_window_open" {
		return "Tiket perjalanan Anda mulai bisa dicek harga live-nya",
			fmt.Sprintf("Halo,\n\nMaskapai mulai membuka penjualan untuk periode perjalanan Anda (%v). Buka Kora untuk menyusun ulang paket dengan harga live:\n\n%s\n", p["window_start"], link)
	}
	return "Waktunya cek ulang harga paket Anda", "Halo,\n\nHarga penerbangan dan hotel bisa berubah. Cek ulang harga paket Anda di Kora:\n\n" + link + "\n"
}
