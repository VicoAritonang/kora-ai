package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/account"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/reference"
	"github.com/koraai/koraai/internal/reminder"
)

// ListReminders handles GET /reminders.
func (a *API) ListReminders(w http.ResponseWriter, r *http.Request) {
	items, err := a.Reminders.List(r.Context(), session(r).UserID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if items == nil {
		writeJSON(w, 200, map[string]any{"items": []any{}})
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

// CreateReminder handles POST /reminders (registered users only).
func (a *API) CreateReminder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ConversationID uuid.UUID `json:"conversation_id"`
		Kind           string    `json:"kind"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	rem, err := a.Reminders.Create(r.Context(), session(r).User, body.ConversationID, body.Kind)
	switch {
	case errors.Is(err, reminder.ErrGuest):
		problem(w, r, 403, "forbidden", "Masuk dulu untuk membuat pengingat", "")
	case errors.Is(err, reminder.ErrNotFound):
		notFound(w, r)
	case errors.Is(err, reminder.ErrValidation):
		problem(w, r, 422, "validation_failed", "Validasi gagal", "kind: booking_window_open | price_recheck; percakapan butuh tanggal.")
	case err != nil:
		internalError(w, r, err)
	default:
		writeJSON(w, 201, rem)
	}
}

// DeleteReminder handles DELETE /reminders/{id}.
func (a *API) DeleteReminder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := a.Reminders.Cancel(r.Context(), session(r).UserID, id); err != nil {
		if errors.Is(err, reminder.ErrNotFound) {
			notFound(w, r)
			return
		}
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Feedback handles POST /feedback.
func (a *API) Feedback(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MessageID *uuid.UUID `json:"message_id"`
		PackageID *uuid.UUID `json:"package_id"`
		Rating    int        `json:"rating"`
		Reason    *string    `json:"reason"`
		Comment   *string    `json:"comment"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	err := a.Account.Feedback(r.Context(), session(r).UserID, body.MessageID, body.PackageID, body.Rating, body.Reason, body.Comment)
	switch {
	case errors.Is(err, account.ErrValidation):
		problem(w, r, 422, "validation_failed", "Validasi gagal", "rating -1/1, reason dari daftar, dan message_id atau package_id.")
	case errors.Is(err, account.ErrNotFound):
		notFound(w, r)
	case err != nil:
		internalError(w, r, err)
	default:
		w.WriteHeader(http.StatusCreated)
	}
}

// ListMemory handles GET /me/memory.
func (a *API) ListMemory(w http.ResponseWriter, r *http.Request) {
	facts, enabled, err := a.Account.MemoryFacts(r.Context(), session(r).User)
	note := ""
	if errors.Is(err, ports.ErrNotSupported) {
		note = "Daftar fakta belum didukung layanan memori."
	} else if err != nil {
		problem(w, r, 503, "unavailable", "Layanan memori tidak tersedia", "")
		return
	}
	writeJSON(w, 200, map[string]any{"items": facts, "enabled": enabled, "note": note})
}

// DeleteMemory handles DELETE /me/memory and /me/memory/{id}. Deletion is
// not available until OpenPersona supports it (API_SPEC §5.5).
func (a *API) DeleteMemory(w http.ResponseWriter, r *http.Request) {
	err := a.Account.ForgetMemory(r.Context(), session(r).UserID)
	if errors.Is(err, ports.ErrNotSupported) {
		problem(w, r, 503, "unavailable", "Penghapusan memori belum tersedia", "Menunggu fitur penghapusan scope di OpenPersona; memori tidak diaktifkan untuk pengguna sampai fitur ini ada.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Healthz handles GET /healthz.
func (a *API) Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// Readyz handles GET /readyz (Postgres and Redis reachable).
func (a *API) Readyz(w http.ResponseWriter, r *http.Request) {
	if err := a.Account.Ready(r.Context()); err != nil {
		problem(w, r, 503, "unavailable", "Database tidak terjangkau", "")
		return
	}
	redisOK := a.Redis != nil && a.Redis.Ping(r.Context()) == nil
	writeJSON(w, 200, map[string]any{"status": "ready", "postgres": true, "redis": redisOK})
}

// Meta handles GET /meta.
func (a *API) Meta(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{
		"api_version": "v1", "build": a.Cfg.App.Build,
		"origins": reference.Origins, "destinations": reference.Destinations,
		"limits": map[string]any{"message_max_chars": 4000, "nights_max": 60, "pax_max": 9,
			"runs_per_day_guest": a.Cfg.Quota.GuestRunsPerDay, "runs_per_day_user": a.Cfg.Quota.UserRunsPerDay},
		"currencies": []string{"IDR", "USD", "JPY", "SGD", "MYR", "EUR"},
		"suppliers":  map[string]string{"flights": a.Cfg.Suppliers.FlightProvider, "stays": a.Cfg.Suppliers.StayProvider},
		"sandbox":    a.Cfg.Suppliers.FlightProvider == "sandbox" || a.Cfg.Suppliers.StayProvider == "sandbox",
	})
}

// ------------------------------------------------------------ internal ---

// InternalRuns handles GET /internal/v1/runs?from=&to=&status=&mode=.
func (a *API) InternalRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	to := time.Now()
	from := to.Add(-24 * time.Hour)
	if t, err := time.Parse(time.RFC3339, q.Get("from")); err == nil {
		from = t
	}
	if t, err := time.Parse(time.RFC3339, q.Get("to")); err == nil {
		to = t
	}
	runs, err := a.Account.Runs(r.Context(), from, to, q.Get("status"), q.Get("mode"), limit(r))
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": runs})
}

// InternalRun handles GET /internal/v1/runs/{id}.
func (a *API) InternalRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	run, err := a.Account.Run(r.Context(), id)
	if err != nil {
		notFound(w, r)
		return
	}
	writeJSON(w, 200, run)
}

// InternalEvents handles GET /internal/v1/runs/{id}/events.
func (a *API) InternalEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	evs, err := a.Account.Events(r.Context(), id)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": evs})
}

// InternalArtifact handles GET /internal/v1/runs/{id}/artifacts/{artifact_id}.
func (a *API) InternalArtifact(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	aid, ok := pathID(w, r, "artifact_id")
	if !ok {
		return
	}
	art, err := a.Account.Artifact(r.Context(), id, aid)
	if err != nil {
		notFound(w, r)
		return
	}
	writeJSON(w, 200, art)
}
