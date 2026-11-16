package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/koraai/koraai/internal/account"
	"github.com/koraai/koraai/internal/auth"
	"github.com/koraai/koraai/internal/store/postgres"
	"github.com/koraai/koraai/internal/transport/http/middleware"
)

type meDTO struct {
	ID            string  `json:"id"`
	IsGuest       bool    `json:"is_guest"`
	Email         *string `json:"email"`
	DisplayName   *string `json:"display_name"`
	HomeCityCode  *string `json:"home_city_code"`
	Locale        string  `json:"locale"`
	MemoryEnabled bool    `json:"memory_enabled"`
	CSRFToken     string  `json:"csrf_token"`
	Quota         struct {
		RunsPerDay    int `json:"runs_per_day"`
		RunsRemaining int `json:"runs_remaining"`
	} `json:"quota"`
	MemoryAvailable bool `json:"memory_available"`
}

func (a *API) me(r *http.Request, u postgres.User, csrf string) meDTO {
	d := meDTO{ID: u.ID.String(), IsGuest: u.IsGuest, Email: u.Email, DisplayName: u.DisplayName, HomeCityCode: u.HomeCityCode,
		Locale: u.Locale, MemoryEnabled: u.MemoryEnabled, CSRFToken: csrf, MemoryAvailable: a.Cfg.Memory.Enabled}
	d.Quota.RunsPerDay = a.Limiter.DailyRuns(u.IsGuest)
	d.Quota.RunsRemaining = a.Limiter.RunsRemaining(r.Context(), u.ID, u.IsGuest)
	return d
}

// Guest handles POST /auth/guest. An existing valid session is returned as is.
func (a *API) Guest(w http.ResponseWriter, r *http.Request) {
	if s := session(r); s != nil {
		writeJSON(w, 200, a.me(r, s.User, a.Auth.CSRFToken(s.ID)))
		return
	}
	tok, u, err := a.Auth.CreateGuest(r.Context(), r.UserAgent(), middleware.ClientIP(r))
	if errors.Is(err, auth.ErrRateLimited) {
		w.Header().Set("Retry-After", "3600")
		problem(w, r, 429, "rate_limited", "Terlalu banyak sesi tamu dari alamat ini", "")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.setSessionCookie(w, tok)
	s, err := a.Auth.Authenticate(r.Context(), tok)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, 201, a.me(r, u, a.Auth.CSRFToken(s.ID)))
}

// MagicLink handles POST /auth/magic-link (always 202 for valid emails).
func (a *API) MagicLink(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if !a.Limiter.Allow(r.Context(), "magic:"+middleware.ClientIP(r), 20, time.Hour) {
		problem(w, r, 429, "rate_limited", "Terlalu banyak permintaan", "")
		return
	}
	err := a.Auth.RequestMagicLink(r.Context(), body.Email)
	if errors.Is(err, auth.ErrInvalidEmail) {
		problem(w, r, 422, "validation_failed", "Validasi gagal", "Email tidak valid.", map[string]string{"field": "email", "code": "invalid"})
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// VerifyMagicLink handles POST /auth/magic-link/verify.
func (a *API) VerifyMagicLink(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	tok, u, err := a.Auth.VerifyMagicLink(r.Context(), body.Token, session(r), r.UserAgent(), middleware.ClientIP(r))
	if errors.Is(err, auth.ErrInvalidToken) {
		problem(w, r, 422, "validation_failed", "Tautan tidak valid atau sudah dipakai", "", map[string]string{"field": "token", "code": "invalid"})
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.setSessionCookie(w, tok)
	s, err := a.Auth.Authenticate(r.Context(), tok)
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, 200, a.me(r, u, a.Auth.CSRFToken(s.ID)))
}

// Logout handles POST /auth/logout.
func (a *API) Logout(w http.ResponseWriter, r *http.Request) {
	if s := session(r); s != nil {
		_ = a.Auth.Logout(r.Context(), s.ID)
	}
	a.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// GetMe handles GET /me.
func (a *API) GetMe(w http.ResponseWriter, r *http.Request) {
	s := session(r)
	writeJSON(w, 200, a.me(r, s.User, a.Auth.CSRFToken(s.ID)))
}

// PatchMe handles PATCH /me.
func (a *API) PatchMe(w http.ResponseWriter, r *http.Request) {
	var p account.Patch
	if !readJSON(w, r, &p) {
		return
	}
	s := session(r)
	if p.MemoryEnabled != nil && *p.MemoryEnabled && s.User.IsGuest {
		problem(w, r, 422, "validation_failed", "Memori hanya untuk akun terdaftar", "", map[string]string{"field": "memory_enabled", "code": "guest"})
		return
	}
	u, err := a.Account.Update(r.Context(), s.UserID, p)
	if errors.Is(err, account.ErrValidation) {
		problem(w, r, 422, "validation_failed", "Validasi gagal", "Periksa home_city_code, locale, dan display_name.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, 200, a.me(r, u, a.Auth.CSRFToken(s.ID)))
}

// DeleteMe handles DELETE /me (grace period, then cascade delete).
func (a *API) DeleteMe(w http.ResponseWriter, r *http.Request) {
	if err := a.Auth.DeleteAccount(r.Context(), session(r).UserID); err != nil {
		internalError(w, r, err)
		return
	}
	a.clearSessionCookie(w)
	w.WriteHeader(http.StatusAccepted)
}

// ExportMe handles GET /me/export.
func (a *API) ExportMe(w http.ResponseWriter, r *http.Request) {
	data, err := a.Account.Export(r.Context(), session(r).UserID)
	if err != nil {
		internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="kora-export.json"`)
	writeJSON(w, 200, data)
}
