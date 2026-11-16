package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/conversation"
	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/reference"
	"github.com/koraai/koraai/internal/store/postgres"
	"github.com/koraai/koraai/internal/transport/http/ndjson"
	"github.com/koraai/koraai/internal/transport/http/sse"
)

type convSummaryDTO struct {
	ID            uuid.UUID  `json:"id"`
	Title         *string    `json:"title"`
	CreatedAt     time.Time  `json:"created_at"`
	LastMessageAt *time.Time `json:"last_message_at"`
}

type convDTO struct {
	ID                uuid.UUID        `json:"id"`
	Title             *string          `json:"title"`
	TripState         domain.TripState `json:"trip_state"`
	ActiveRunID       *uuid.UUID       `json:"active_run_id"`
	LastRunID         *uuid.UUID       `json:"last_run_id"`
	SelectedPackageID *uuid.UUID       `json:"selected_package_id"`
	CreatedAt         time.Time        `json:"created_at"`
}

type runDTO struct {
	ID             uuid.UUID  `json:"id"`
	ConversationID uuid.UUID  `json:"conversation_id"`
	Status         string     `json:"status"`
	Mode           *string    `json:"mode"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	ErrorCode      *string    `json:"error_code"`
	StreamURL      string     `json:"stream_url,omitempty"`
}

func toRun(r postgres.Run) runDTO {
	return runDTO{r.ID, r.ConversationID, r.Status, r.Mode, r.StartedAt, r.FinishedAt, r.ErrorCode, "/api/v1/runs/" + r.ID.String() + "/stream"}
}

func toConv(d conversation.Detail) convDTO {
	c := d.Conversation
	out := convDTO{ID: c.ID, Title: c.Title, TripState: c.TripState, SelectedPackageID: c.SelectedPackageID, CreatedAt: c.CreatedAt}
	if d.ActiveRun != nil {
		out.ActiveRunID = &d.ActiveRun.ID
	}
	return out
}

func (a *API) convError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, conversation.ErrNotFound):
		notFound(w, r)
	case errors.Is(err, conversation.ErrRunInProgress):
		problem(w, r, 409, "run_in_progress", "Masih ada proses yang berjalan", "Tunggu jawaban sebelumnya selesai.")
	case errors.Is(err, conversation.ErrIdempotencyConflict):
		problem(w, r, 409, "idempotency_conflict", "Idempotency-Key dipakai untuk isi berbeda", "")
	case errors.Is(err, conversation.ErrStateConflict):
		problem(w, r, 409, "state_conflict", "Versi data sudah berubah", "Muat ulang lalu coba lagi.")
	case errors.Is(err, conversation.ErrQuota):
		w.Header().Set("Retry-After", strconv.Itoa(secondsToJakartaMidnight()))
		w.Header().Set("X-RateLimit-Remaining", "0")
		problem(w, r, 429, "rate_limited", "Kuota harian habis", "Kuota percakapan hari ini sudah terpakai. Coba lagi besok, atau masuk untuk kuota lebih besar.")
	case errors.Is(err, conversation.ErrCapacity):
		w.Header().Set("Retry-After", "10")
		problem(w, r, 429, "capacity_exhausted", "Server sedang penuh", "Coba lagi beberapa detik lagi.")
	case errors.Is(err, conversation.ErrValidation):
		problem(w, r, 422, "validation_failed", "Validasi gagal", err.Error())
	default:
		internalError(w, r, err)
	}
}

func secondsToJakartaMidnight() int {
	loc, _ := time.LoadLocation("Asia/Jakarta")
	if loc == nil {
		loc = time.FixedZone("WIB", 7*3600)
	}
	now := time.Now().In(loc)
	mid := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, loc)
	return int(mid.Sub(now).Seconds())
}

// ListConversations handles GET /conversations.
func (a *API) ListConversations(w http.ResponseWriter, r *http.Request) {
	items, next, err := a.Conv.List(r.Context(), session(r).UserID, r.URL.Query().Get("cursor"), limit(r))
	if err != nil {
		internalError(w, r, err)
		return
	}
	out := make([]convSummaryDTO, 0, len(items))
	for _, c := range items {
		out = append(out, convSummaryDTO{c.ID, c.Title, c.CreatedAt, c.LastMessageAt})
	}
	var nc *string
	if next != "" {
		nc = &next
	}
	writeJSON(w, 200, map[string]any{"items": out, "next_cursor": nc})
}

// CreateConversation handles POST /conversations.
func (a *API) CreateConversation(w http.ResponseWriter, r *http.Request) {
	c, err := a.Conv.Create(r.Context(), session(r).UserID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, 201, toConv(conversation.Detail{Conversation: c}))
}

// GetConversation handles GET /conversations/{id}.
func (a *API) GetConversation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	d, err := a.Conv.Get(r.Context(), session(r).UserID, id)
	if err != nil {
		a.convError(w, r, err)
		return
	}
	out := toConv(d)
	out.LastRunID = a.Conv.LastRunID(r.Context(), id)
	writeJSON(w, 200, out)
}

// PatchConversation handles PATCH /conversations/{id} (title).
func (a *API) PatchConversation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if err := a.Conv.Rename(r.Context(), session(r).UserID, id, body.Title); err != nil {
		a.convError(w, r, err)
		return
	}
	a.GetConversation(w, r)
}

// DeleteConversation handles DELETE /conversations/{id}.
func (a *API) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := a.Conv.Delete(r.Context(), session(r).UserID, id); err != nil {
		a.convError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListMessages handles GET /conversations/{id}/messages (cursor = seq).
func (a *API) ListMessages(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
	n := limit(r)
	msgs, err := a.Conv.Messages(r.Context(), session(r).UserID, id, after, n)
	if err != nil {
		a.convError(w, r, err)
		return
	}
	if msgs == nil {
		msgs = []postgres.Message{}
	}
	var next *string
	if len(msgs) == n {
		s := strconv.FormatInt(msgs[len(msgs)-1].Seq, 10)
		next = &s
	}
	writeJSON(w, 200, map[string]any{"items": msgs, "next_cursor": next})
}

// PostMessage handles POST /conversations/{id}/messages → 202.
func (a *API) PostMessage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		problem(w, r, 400, "bad_request", "Header Idempotency-Key wajib", "")
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if n := len([]rune(strings.TrimSpace(body.Content))); n < 1 || n > 4000 {
		problem(w, r, 422, "validation_failed", "Validasi gagal", "Field 'content' wajib diisi (1–4.000 karakter).", map[string]string{"field": "content", "code": "length"})
		return
	}
	s := session(r)
	res, err := a.Conv.PostMessage(r.Context(), s.User, id, body.Content, key)
	if err != nil {
		a.convError(w, r, err)
		return
	}
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(a.Limiter.RunsRemaining(r.Context(), s.UserID, s.User.IsGuest)))
	writeJSON(w, 202, map[string]any{"message": res.Message, "run": toRun(res.Run)})
}

// PatchTripState handles PATCH /conversations/{id}/trip-state.
func (a *API) PatchTripState(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Version int `json:"version"`
		Set     struct {
			Nights      *int    `json:"nights"`
			Pax         *struct {
				Adults   *int `json:"adults"`
				Children *int `json:"children"`
			} `json:"pax"`
			Budget      *domain.Money `json:"budget"`
			Origin      *string       `json:"origin"`
			Destination *string       `json:"destination"`
		} `json:"set"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	var p domain.TripStatePatch
	p.Nights = body.Set.Nights
	if body.Set.Pax != nil {
		p.Adults, p.Children = body.Set.Pax.Adults, body.Set.Pax.Children
	}
	if body.Set.Budget != nil {
		if body.Set.Budget.Amount <= 0 || !domain.SupportedCurrency(body.Set.Budget.Currency) {
			problem(w, r, 422, "validation_failed", "Validasi gagal", "budget.amount > 0 dan currency didukung.")
			return
		}
		p.Budget = body.Set.Budget
	}
	if body.Set.Origin != nil {
		pl, ok := reference.Origin(*body.Set.Origin)
		if !ok {
			problem(w, r, 422, "validation_failed", "Kota asal tidak didukung", "", map[string]string{"field": "origin", "code": "unsupported"})
			return
		}
		p.Origin = &pl
	}
	if body.Set.Destination != nil {
		pl, ok := reference.Destination(*body.Set.Destination)
		if !ok {
			problem(w, r, 422, "validation_failed", "Destinasi tidak didukung", "", map[string]string{"field": "destination", "code": "unsupported"})
			return
		}
		p.Destination = &pl
	}
	st, err := a.Conv.PatchTripState(r.Context(), session(r).UserID, id, body.Version, p)
	if err != nil {
		a.convError(w, r, err)
		return
	}
	writeJSON(w, 200, st)
}

// GetRun handles GET /runs/{id}.
func (a *API) GetRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	run, err := a.Conv.Run(r.Context(), session(r).UserID, id)
	if err != nil {
		a.convError(w, r, err)
		return
	}
	d := toRun(run)
	d.StreamURL = ""
	writeJSON(w, 200, d)
}

// CancelRun handles POST /runs/{id}/cancel.
func (a *API) CancelRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := a.Conv.Cancel(r.Context(), session(r).UserID, id); err != nil {
		a.convError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// StreamRun handles GET /runs/{id}/stream (SSE). Old events are replayed
// from Postgres after Last-Event-ID; new ones arrive through Redis run:{id}
// (or polling when Redis is down). The stream closes after the terminal
// event (API_SPEC §6, NFR-13).
func (a *API) StreamRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	if _, err := a.Conv.Run(ctx, session(r).UserID, id); err != nil {
		a.convError(w, r, err)
		return
	}
	last, _ := strconv.Atoi(r.Header.Get("Last-Event-ID"))
	if q := r.URL.Query().Get("last_event_id"); q != "" && last == 0 {
		last, _ = strconv.Atoi(q)
	}
	s := sse.New(w)
	_ = s.Retry(2000)
	a.follow(r, id, last, s)
}

// eventWriter is the output format of a run stream (SSE or NDJSON).
type eventWriter interface {
	Event(seq int, typ string, data []byte) error
	Ping() error
}

// StreamRunNDJSON handles GET /runs/{id}/events: the same events as
// newline-delimited JSON for the web workspace store. Resume with ?after=N.
func (a *API) StreamRunNDJSON(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if _, err := a.Conv.Run(r.Context(), session(r).UserID, id); err != nil {
		a.convError(w, r, err)
		return
	}
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	a.follow(r, id, after, ndjson.New(w))
}

// follow replays events after last from Postgres, then follows new ones via
// Redis run:{id} (polling as backup) until the terminal event.
func (a *API) follow(r *http.Request, id uuid.UUID, last int, s eventWriter) {
	ctx := r.Context()
	var notify <-chan struct{}
	if a.Redis != nil {
		sub := a.Redis.Subscribe(ctx, "run:"+id.String())
		defer sub.Close()
		ch := make(chan struct{}, 1)
		go func() {
			for range sub.Channel() {
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		}()
		notify = ch
	}
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	poll := time.NewTicker(time.Second)
	defer poll.Stop()

	flush := func() (bool, error) {
		evs, err := a.Account.PublicEvents(ctx, id, last)
		if err != nil {
			return false, err
		}
		for _, e := range evs {
			if err := s.Event(e.Seq, e.Type, e.Payload); err != nil {
				return false, err
			}
			last = e.Seq
			if e.Type == "run.completed" || e.Type == "run.failed" {
				return true, nil
			}
		}
		return false, nil
	}
	for {
		done, err := flush()
		if err != nil || done {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-notify:
		case <-poll.C:
			// Polling backs up Redis: catches events published while the
			// subscription was being set up, and runs failed by a sweeper.
			if run, err := a.Conv.Run(context.WithoutCancel(ctx), session(r).UserID, id); err == nil && run.Status != "running" {
				if done, _ := flush(); done {
					return
				}
				if run.FinishedAt != nil && time.Since(*run.FinishedAt) > 5*time.Second {
					// Terminal event missing (e.g. interrupted run): synthesize it.
					b, _ := json.Marshal(map[string]any{"run_id": id, "code": derefOr(run.ErrorCode, "interrupted"), "message": "Proses berakhir.", "retryable": true})
					_ = s.Event(last+1, "run.failed", b)
					return
				}
			}
		case <-ping.C:
			if err := s.Ping(); err != nil {
				return
			}
		}
	}
}

func derefOr(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}


// PostAction handles POST /conversations/{id}/actions: a structured action
// from the web canvas, applied to the trip state and answered by a run.
func (a *API) PostAction(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		problem(w, r, 400, "bad_request", "Header Idempotency-Key wajib", "")
		return
	}
	var act conversation.Action
	if !readJSON(w, r, &act) {
		return
	}
	if act.Payload == nil {
		act.Payload = map[string]any{}
	}
	s := session(r)
	res, err := a.Conv.PostAction(r.Context(), s.User, id, act, key)
	if errors.Is(err, conversation.ErrUnknownAction) {
		problem(w, r, 422, "validation_failed", "Aksi tidak dikenal", act.Type)
		return
	}
	if err != nil {
		a.convError(w, r, err)
		return
	}
	writeJSON(w, 202, map[string]any{"message": res.Message, "run": toRun(res.Run)})
}
