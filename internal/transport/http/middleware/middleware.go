// Package middleware holds the HTTP middleware chain: request id, logging,
// panic recovery, CORS, security headers, session, CSRF, rate limit.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/koraai/koraai/internal/auth"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/store/postgres"
	"github.com/koraai/koraai/internal/usage"
)

type ctxKey int

const keySession ctxKey = iota

// CookieName is the session cookie.
const CookieName = "sid"

// Problem writes an RFC 9457 problem+json response.
func Problem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string, fields ...map[string]string) {
	body := map[string]any{
		"type": "https://docs.kora.local/errors/" + code, "title": title, "status": status, "code": code,
		"detail": detail, "request_id": platform.RequestID(r.Context()),
	}
	if len(fields) > 0 {
		body["errors"] = fields
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// RequestID assigns X-Request-Id.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(platform.WithRequestID(r.Context(), id)))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }

// Flush supports SSE through the wrapper.
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Logger logs one line per request (no bodies, no PII).
func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(sw, r)
			lvl := slog.LevelInfo
			if sw.status >= 500 {
				lvl = slog.LevelError
			}
			log.Log(r.Context(), lvl, "http request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "duration_ms", time.Since(start).Milliseconds())
		})
	}
}

// Recover turns panics into 500s.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					log.Error("http panic", "panic", p, "stack", string(debug.Stack()))
					Problem(w, r, 500, "internal_error", "Kesalahan internal", "")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// CORS allows only the web origin, with credentials.
func CORS(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if o := r.Header.Get("Origin"); o != "" && o == origin {
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Access-Control-Allow-Credentials", "true")
				h.Set("Vary", "Origin")
				h.Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, Idempotency-Key, X-Request-Id, Last-Event-ID")
				h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Expose-Headers", "X-Request-Id, Retry-After, X-RateLimit-Remaining")
				h.Set("Access-Control-Max-Age", "600")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders sets the baseline headers (ENGINEERING_STANDARDS §7.1).
func SecurityHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			h.Set("X-Frame-Options", "DENY")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// BodyLimit caps request bodies.
func BodyLimit(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

// Session loads the session from the cookie when present.
func Session(a *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
				if s, err := a.Authenticate(r.Context(), c.Value); err == nil {
					ctx := context.WithValue(r.Context(), keySession, &s)
					ctx = platform.WithUser(ctx, s.UserID)
					r = r.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SessionFrom returns the request's session, if any.
func SessionFrom(ctx context.Context) *postgres.Session {
	s, _ := ctx.Value(keySession).(*postgres.Session)
	return s
}

// RequireSession answers 401 without a session.
func RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if SessionFrom(r.Context()) == nil {
			Problem(w, r, 401, "unauthenticated", "Sesi tidak ditemukan", "Panggil POST /auth/guest terlebih dulu.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CSRF requires X-CSRF-Token on mutating requests with a session, and a JSON
// content type on bodies (blocks cross-site form posts).
func CSRF(a *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if r.ContentLength > 0 && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				Problem(w, r, 400, "bad_request", "Content-Type harus application/json", "")
				return
			}
			s := SessionFrom(r.Context())
			if s != nil && !a.VerifyCSRF(s.ID, r.Header.Get("X-CSRF-Token")) {
				Problem(w, r, 403, "forbidden", "Token CSRF tidak valid", "Ambil token dari GET /me.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimit applies the per-session (or per-IP) request limit.
func RateLimit(l *usage.Limiter, perMinute int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := "ip:" + ClientIP(r)
			if s := SessionFrom(r.Context()); s != nil {
				key = "sess:" + s.ID.String()
			}
			if !l.Allow(r.Context(), key, perMinute, time.Minute) {
				w.Header().Set("Retry-After", "60")
				Problem(w, r, 429, "rate_limited", "Terlalu banyak permintaan", "Coba lagi sebentar lagi.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP returns the client address (first X-Forwarded-For hop when set by
// the load balancer).
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ServiceToken protects /internal/v1.
func ServiceToken(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" || r.Header.Get("Authorization") != "Bearer "+token {
				Problem(w, r, 401, "unauthenticated", "Token layanan tidak valid", "")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
