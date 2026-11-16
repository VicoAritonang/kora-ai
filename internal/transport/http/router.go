// Package httpapi assembles the chi router: /api/v1 for the web app,
// /internal/v1 for AI Lab (service token), /healthz and /readyz.
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/koraai/koraai/internal/transport/http/handlers"
	mw "github.com/koraai/koraai/internal/transport/http/middleware"
)

// Router builds the HTTP handler.
func Router(a *handlers.API, log *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Use(mw.RequestID, mw.Recover(log), mw.Logger(log), mw.SecurityHeaders(a.Cfg.Secure()), mw.CORS(a.Cfg.App.WebOrigin))

	// The API has no pages; point people opening it in a browser to the web app.
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"service":"kora-api","message":"Ini API Kora. Buka aplikasi web di ` + a.Cfg.App.WebOrigin + `","api":"/api/v1","health":"/readyz"}`))
	})
	r.Get("/healthz", a.Healthz)
	r.Get("/readyz", a.Readyz)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(mw.BodyLimit(64<<10), mw.Session(a.Auth), mw.CSRF(a.Auth))
		r.Get("/meta", a.Meta)
		r.Group(func(r chi.Router) {
			r.Use(mw.RateLimit(a.Limiter, 20))
			r.Post("/auth/guest", a.Guest)
			r.Post("/auth/magic-link", a.MagicLink)
			r.Post("/auth/magic-link/verify", a.VerifyMagicLink)
		})
		r.Post("/auth/logout", a.Logout)

		r.Group(func(r chi.Router) {
			r.Use(mw.RequireSession, mw.RateLimit(a.Limiter, a.Cfg.Quota.RequestsPerMinute))
			r.Get("/me", a.GetMe)
			r.Patch("/me", a.PatchMe)
			r.Delete("/me", a.DeleteMe)
			r.Get("/me/export", a.ExportMe)
			r.Get("/me/memory", a.ListMemory)
			r.Delete("/me/memory", a.DeleteMemory)
			r.Delete("/me/memory/{id}", a.DeleteMemory)

			r.Get("/conversations", a.ListConversations)
			r.Post("/conversations", a.CreateConversation)
			r.Get("/conversations/{id}", a.GetConversation)
			r.Patch("/conversations/{id}", a.PatchConversation)
			r.Delete("/conversations/{id}", a.DeleteConversation)
			r.Get("/conversations/{id}/messages", a.ListMessages)
			r.Post("/conversations/{id}/messages", a.PostMessage)
			r.Patch("/conversations/{id}/trip-state", a.PatchTripState)
			r.Post("/conversations/{id}/actions", a.PostAction)
			r.Get("/conversations/{id}/packages", a.ListPackages)

			r.Get("/runs/{id}", a.GetRun)
			r.Get("/runs/{id}/stream", a.StreamRun)
			r.Get("/runs/{id}/events", a.StreamRunNDJSON)
			r.Post("/runs/{id}/cancel", a.CancelRun)

			r.Get("/packages/{id}", a.GetPackage)
			r.Post("/packages/{id}/select", a.SelectPackage)
			r.Post("/packages/{id}/reprice", a.RepricePackage)
			r.Post("/packages/{id}/items/{item_id}/outbound", a.Outbound)

			r.Get("/reminders", a.ListReminders)
			r.Post("/reminders", a.CreateReminder)
			r.Delete("/reminders/{id}", a.DeleteReminder)
			r.Post("/feedback", a.Feedback)
		})
	})

	r.Route("/internal/v1", func(r chi.Router) {
		r.Use(mw.ServiceToken(a.Cfg.App.ServiceToken))
		r.Get("/runs", a.InternalRuns)
		r.Get("/runs/{id}", a.InternalRun)
		r.Get("/runs/{id}/events", a.InternalEvents)
		r.Get("/runs/{id}/artifacts/{artifact_id}", a.InternalArtifact)
	})
	return r
}
