// Package handlers implements the /api/v1 and /internal/v1 endpoints
// (API_SPEC §5). Handlers validate shapes, call application modules and
// serialize; they hold no domain logic (rule D7).
package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/account"
	"github.com/koraai/koraai/internal/auth"
	"github.com/koraai/koraai/internal/config"
	"github.com/koraai/koraai/internal/conversation"
	"github.com/koraai/koraai/internal/packages"
	"github.com/koraai/koraai/internal/reminder"
	"github.com/koraai/koraai/internal/store/postgres"
	"github.com/koraai/koraai/internal/store/redis"
	"github.com/koraai/koraai/internal/transport/http/middleware"
	"github.com/koraai/koraai/internal/usage"
)

// API holds the modules the handlers call.
type API struct {
	Cfg       *config.Config
	Auth      *auth.Service
	Conv      *conversation.Service
	Packages  *packages.Service
	Reminders *reminder.Service
	Account   *account.Service
	Limiter   *usage.Limiter
	Redis     *redis.Client // SSE fan-out; nil → polling
	Ready     func(r *http.Request) error
}

var problem = middleware.Problem

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		problem(w, r, 400, "bad_request", "Bentuk request salah", err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		problem(w, r, 404, "not_found", "Tidak ditemukan", "")
		return uuid.Nil, false
	}
	return id, true
}

func session(r *http.Request) *postgres.Session { return middleware.SessionFrom(r.Context()) }

func limit(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n < 1 {
		return 20
	}
	if n > 100 {
		return 100
	}
	return n
}

// internalError logs nothing here: the Logger middleware records the 500
// with the request id, and modules log at the point of decision.
func internalError(w http.ResponseWriter, r *http.Request, err error) {
	_ = err
	problem(w, r, 500, "internal_error", "Kesalahan internal", "Coba lagi; sertakan request_id bila melapor.")
}

func notFound(w http.ResponseWriter, r *http.Request) {
	problem(w, r, 404, "not_found", "Tidak ditemukan", "")
}

func (a *API) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: middleware.CookieName, Value: token, Path: "/", HttpOnly: true, Secure: a.Cfg.Secure(),
		SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(auth.SessionTTL), MaxAge: int(auth.SessionTTL.Seconds()),
	})
}

func (a *API) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: middleware.CookieName, Value: "", Path: "/", HttpOnly: true, Secure: a.Cfg.Secure(), SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
