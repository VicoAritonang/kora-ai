// Package app is the composition root (rule D8): it builds every module from
// configuration and decides which adapter implements which port.
package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/koraai/koraai/internal/account"
	"github.com/koraai/koraai/internal/adapters/arllab"
	"github.com/koraai/koraai/internal/adapters/duffel"
	"github.com/koraai/koraai/internal/adapters/email"
	"github.com/koraai/koraai/internal/adapters/fx"
	"github.com/koraai/koraai/internal/adapters/guardrails"
	"github.com/koraai/koraai/internal/adapters/jev"
	"github.com/koraai/koraai/internal/adapters/liteapi"
	"github.com/koraai/koraai/internal/adapters/openai"
	"github.com/koraai/koraai/internal/adapters/openpersona"
	"github.com/koraai/koraai/internal/adapters/sandbox"
	"github.com/koraai/koraai/internal/adapters/tracerlab"
	"github.com/koraai/koraai/internal/adapters/tracerpg"
	"github.com/koraai/koraai/internal/agent"
	"github.com/koraai/koraai/internal/auth"
	"github.com/koraai/koraai/internal/config"
	"github.com/koraai/koraai/internal/conversation"
	"github.com/koraai/koraai/internal/costnorm"
	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/jobs"
	"github.com/koraai/koraai/internal/packagebuilder"
	"github.com/koraai/koraai/internal/packages"
	"github.com/koraai/koraai/internal/pipeline"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/priceindex"
	"github.com/koraai/koraai/internal/reference"
	"github.com/koraai/koraai/internal/reminder"
	"github.com/koraai/koraai/internal/store/postgres"
	"github.com/koraai/koraai/internal/store/redis"
	"github.com/koraai/koraai/internal/tools"
	"github.com/koraai/koraai/internal/transport/http/handlers"
	"github.com/koraai/koraai/internal/usage"
	"github.com/koraai/koraai/internal/verifier"
)

// App holds the assembled system.
type App struct {
	Cfg       *config.Config
	Log       *slog.Logger
	Clock     platform.Clock
	DB        *postgres.DB
	Redis     *redis.Client
	Model     ports.ModelProvider
	Decider   ports.Decider
	Persona   *openpersona.Persona
	Memory    ports.UserMemory
	Guard     ports.Guard
	Flights   ports.FlightSupplier
	Stays     ports.StaySupplier
	Norms     *costnorm.Service
	FX        *costnorm.FX
	Index     *priceindex.Index
	Limiter   *usage.Limiter
	Email     ports.EmailSender
	Runner    *pipeline.Runner
	API       *handlers.API
	River     *river.Client[pgx.Tx]
	Reminders *reminder.Service
	// Lab streams runs to Agent Reliability Lab (local development only).
	Lab *arllab.Tracer
}

// Options tune the build for each binary.
type Options struct {
	Clock platform.Clock // nil = system clock
	// SupplierLatency overrides the sandbox's simulated latency (eval: 0).
	SupplierLatency *time.Duration
}

// Build connects to everything and wires the modules.
func Build(ctx context.Context, cfg *config.Config, log *slog.Logger, opt Options) (*App, error) {
	a := &App{Cfg: cfg, Log: log, Clock: opt.Clock}
	if a.Clock == nil {
		a.Clock = platform.SystemClock{}
	}
	var err error
	if a.DB, err = postgres.Open(ctx, cfg.DB.URL, cfg.DB.MaxConns); err != nil {
		return nil, err
	}
	if a.Redis, err = redis.Open(ctx, cfg.Redis.URL); err != nil {
		// Degraded mode: caches skipped, limits in memory, SSE polls Postgres.
		log.Warn("redis unavailable at start; running degraded", "err", err)
	}

	// --- AI ---
	a.Model = openai.New(cfg.OpenAI.BaseURL, cfg.OpenAI.APIKey, cfg.OpenAI.Timeout)
	if cfg.JEV.APIKey != "" {
		if a.Decider, err = jev.New(cfg.JEV.BaseURL, cfg.JEV.APIKey, cfg.JEV.Model, cfg.JEV.Timeout); err != nil {
			return nil, err
		}
	} else {
		log.Warn("JEV key missing: router uses the gpt-5-nano fallback and the rubric is skipped")
	}
	if p, err := openpersona.Open(cfg.Persona.PackPath, cfg.Persona.UnitsPerQuery); err != nil {
		log.Error("persona pack unavailable: planning runs will fail with kg_unavailable", "path", cfg.Persona.PackPath, "err", err)
	} else {
		a.Persona = p
		log.Info("persona pack loaded", "version", p.Version(), "name", p.Name())
	}
	if cfg.Memory.Enabled {
		a.Memory = openpersona.NewMemory(cfg.Memory.URL)
	} else {
		a.Memory = openpersona.Disabled{}
	}

	// --- guardrails ---
	if cfg.Guardrails.Mode == "off" {
		log.Warn("GUARDRAILS_MODE=off: local debugging only")
		a.Guard = guardrails.Off{}
	} else {
		g, err := guardrails.New(cfg.Guardrails.PolicyFile, cfg.Guardrails.ToolsFile, []byte(cfg.Guardrails.Secret))
		if err != nil {
			return nil, err
		}
		a.Guard = g
	}

	// --- suppliers ---
	sb := sandbox.New(a.Clock, cfg.Policy.BookingWindowDays, cfg.Suppliers.FlightTTL, cfg.Suppliers.StayTTL)
	if opt.SupplierLatency != nil {
		sb.Latency = *opt.SupplierLatency
	}
	switch cfg.Suppliers.FlightProvider {
	case "duffel":
		a.Flights = duffel.New(cfg.Suppliers.DuffelBaseURL, cfg.Suppliers.DuffelToken, a.Clock, cfg.Policy.BookingWindowDays, cfg.Suppliers.FlightTTL)
	default:
		a.Flights = sb
	}
	switch cfg.Suppliers.StayProvider {
	case "liteapi":
		a.Stays = liteapi.New(cfg.Suppliers.LiteAPIBaseURL, cfg.Suppliers.LiteAPIKey, cfg.Suppliers.LiteAPIMargin, a.Clock, cfg.Suppliers.StayTTL)
	default:
		a.Stays = sandbox.Stays{S: sb}
	}

	// --- money ---
	var fxp ports.FXProvider = fx.NewFrankfurter(cfg.FX.BaseURL)
	if cfg.FX.Provider == "static" {
		fxp = fx.DefaultStatic(a.Clock)
	}
	a.FX = costnorm.NewFX(a.DB, fxp, fx.DefaultStatic(a.Clock), a.Clock, log)
	a.Norms = costnorm.New(a.DB)
	a.Index = &priceindex.Index{DB: a.DB, Flights: a.Flights, Stays: a.Stays, FX: a.FX, Norms: a.Norms, Clock: a.Clock, Log: log, TTL: 7 * 24 * time.Hour, Window: cfg.Policy.BookingWindowDays}

	// --- usage, email ---
	a.Limiter = usage.New(a.Redis, cfg.Quota, a.Clock)
	if cfg.Email.Provider == "smtp" {
		a.Email = email.SMTP{Addr: cfg.Email.SMTPAddr, User: cfg.Email.SMTPUser, Pass: cfg.Email.SMTPPass, From: cfg.Email.From}
	} else {
		a.Email = email.Log{L: log}
	}
	a.Reminders = &reminder.Service{DB: a.DB, Email: a.Email, Clock: a.Clock, Log: log, Window: cfg.Policy.BookingWindowDays, WebOrigin: cfg.App.WebOrigin}
	return a, nil
}

// Tracer returns the event sinks: Postgres first (authoritative), then the
// best-effort ones: AI Lab ingest and Agent Reliability Lab when configured.
func (a *App) Tracer() ports.Tracer {
	sinks := tracerlab.Multi{&tracerpg.Tracer{DB: a.DB, Redis: a.Redis, Log: a.Log}}
	if a.Cfg.Tracer.LabURL != "" {
		sinks = append(sinks, tracerlab.New(a.Cfg.Tracer.LabURL, a.Cfg.Tracer.LabToken, a.Log))
	}
	if l := a.labTracer(); l != nil {
		sinks = append(sinks, l)
	}
	if len(sinks) == 1 {
		return sinks[0]
	}
	return sinks
}

// labTracer connects Agent Reliability Lab once per process (config only
// allows it with APP_ENV=local). Nil when STAGE≠dev or LAB_WS_URL is empty.
func (a *App) labTracer() *arllab.Tracer {
	if a.Lab != nil || !a.Cfg.Tracer.ARL.Enabled() {
		return a.Lab
	}
	c := a.Cfg.Tracer.ARL
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.Lab = arllab.New(ctx, arllab.Config{
		URL: c.URL, Stage: c.Stage, Token: c.Token, Project: c.Project, Required: c.Required, Service: c.Service,
		Metadata: map[string]any{"build": a.Cfg.Policy.BuilderVersion, "supplier_flights": a.Cfg.Suppliers.FlightProvider, "supplier_stays": a.Cfg.Suppliers.StayProvider},
	}, a.Log)
	if a.Lab != nil {
		a.Log.Info("agent reliability lab: streaming runs", "url", c.URL, "service", c.Service)
	}
	return a.Lab
}

// Policy is the package-builder policy from config.
func (a *App) Policy() packagebuilder.Policy {
	p := packagebuilder.DefaultPolicy()
	p.ContingencyPct, p.FXBufferPct, p.MinStayRating, p.Version = a.Cfg.Policy.ContingencyPct, a.Cfg.Policy.FXBufferPct, a.Cfg.Policy.MinStayRating, a.Cfg.Policy.BuilderVersion
	return p
}

// BuildPipeline wires the runner and the HTTP API (api binary, eval runner).
func (a *App) BuildPipeline(queue pipeline.JobQueue) {
	cfg := a.Cfg
	td := &tools.Deps{
		Store: offerStore{a.DB}, Flights: a.Flights, Stays: a.Stays, FX: a.FX, Index: a.Index, Attractions: a.Norms,
		Memory: a.Memory, MemoryOn: cfg.Memory.Enabled, Researcher: a.Model, ResearchModel: cfg.Models.Researcher,
		Clock: a.Clock, Window: cfg.Policy.BookingWindowDays, Policy: a.Policy(),
		Limits: agent.Limits{FlightSearches: cfg.Limits.FlightSearches, StaySearches: cfg.Limits.StaySearches,
			ActivitySearch: cfg.Limits.ActivitySearch, WebResearch: cfg.Limits.WebResearch},
		Areas: func(code string) []string {
			c, _ := reference.DestinationCity(code)
			return c.Areas
		},
	}
	if a.Redis != nil {
		td.Cache = a.Redis
	}
	if a.Persona != nil {
		td.Persona = a.Persona
	}
	deps := pipeline.Deps{
		Cfg: cfg, DB: a.DB, Redis: a.Redis, Model: a.Model, Decider: a.Decider, Memory: a.Memory, Guard: a.Guard,
		Tracer: a.Tracer(), Registry: agent.NewRegistry(tools.All(td)...), ToolDeps: td, Norms: a.Norms, FX: a.FX,
		Index: a.Index, Verifier: &verifier.Verifier{Decider: a.Decider, Threshold: cfg.JEV.Threshold}, Jobs: queue,
		Clock: a.Clock, Log: a.Log,
	}
	if a.Persona != nil {
		deps.Persona = a.Persona
	}
	a.Runner = pipeline.NewRunner(deps)

	authSvc := &auth.Service{DB: a.DB, Email: a.Email, Limiter: a.Limiter, Clock: a.Clock, Log: a.Log, Key: []byte(cfg.App.SessionKey),
		WebOrigin: cfg.App.WebOrigin, GuestPerIPHour: cfg.Quota.GuestSessionsPerIPHour}
	a.API = &handlers.API{
		Cfg: cfg, Auth: authSvc, Limiter: a.Limiter, Redis: a.Redis, Reminders: a.Reminders,
		Conv: &conversation.Service{DB: a.DB, Runner: a.Runner, Limiter: a.Limiter, Cfg: cfg, Guard: a.Guard, Clock: a.Clock},
		Packages: &packages.Service{DB: a.DB, Flights: a.Flights, Stays: a.Stays, Norms: a.Norms, FX: a.FX, Limiter: a.Limiter,
			Clock: a.Clock, Policy: a.Policy(), FXMaxAgeDays: cfg.Policy.FXMaxAgeDays},
		Account: &account.Service{DB: a.DB, Memory: a.Memory, MemoryOn: cfg.Memory.Enabled},
	}
}

// InsertOnlyRiver returns a River client that can insert jobs but runs none
// (the api binary).
func (a *App) InsertOnlyRiver() (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(a.DB.Pool), &river.Config{})
}

// WorkerRiver returns the worker's River client with every job registered.
func (a *App) WorkerRiver() (*river.Client[pgx.Tx], error) {
	d := &jobs.Deps{Cfg: a.Cfg, DB: a.DB, Model: a.Model, Guard: a.Guard, Memory: a.Memory, Index: a.Index, Reminders: a.Reminders, Clock: a.Clock, Log: a.Log}
	return river.NewClient(riverpgxv5.New(a.DB.Pool), &river.Config{
		Queues:       map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 4}},
		Workers:      jobs.Workers(d),
		PeriodicJobs: jobs.Periodic(),
		// Neon's pooled endpoint does not keep LISTEN sessions; poll instead.
		PollOnly:          true,
		FetchPollInterval: 2 * time.Second,
		Logger:            a.Log,
	})
}

// Close releases resources.
func (a *App) Close() {
	if a.Lab != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = a.Lab.Close(ctx) // drain queued events before exit
		cancel()
	}
	if a.Persona != nil {
		_ = a.Persona.Close()
	}
	if a.Redis != nil {
		_ = a.Redis.Close()
	}
	a.DB.Close()
}

// ----------------------------------------------------------- offer store --

// offerStore implements tools.Store over Postgres.
type offerStore struct{ db *postgres.DB }

func (s offerStore) SaveFlightOffer(ctx context.Context, o domain.FlightOffer) error {
	return s.db.Q().SaveFlightOffer(ctx, o)
}

func (s offerStore) SaveStayOffer(ctx context.Context, o domain.StayOffer) error {
	return s.db.Q().SaveStayOffer(ctx, o)
}

func (s offerStore) InsertArtifact(ctx context.Context, id, runID uuid.UUID, tool string, args, result any, digest string) error {
	return s.db.Q().InsertArtifact(ctx, id, runID, tool, args, result, digest)
}

func (s offerStore) CachedFlights(ctx context.Context, key string, now time.Time) ([]domain.FlightOffer, error) {
	rows, err := s.db.Q().FreshOffers(ctx, domain.OfferFlight, key, now)
	if err != nil {
		return nil, err
	}
	var out []domain.FlightOffer
	for _, r := range rows {
		var o domain.FlightOffer
		if json.Unmarshal(r.Summary, &o) == nil {
			out = append(out, o)
		}
	}
	return out, nil
}

func (s offerStore) CachedStays(ctx context.Context, key string, now time.Time) ([]domain.StayOffer, error) {
	rows, err := s.db.Q().FreshOffers(ctx, domain.OfferStay, key, now)
	if err != nil {
		return nil, err
	}
	var out []domain.StayOffer
	for _, r := range rows {
		var o domain.StayOffer
		if json.Unmarshal(r.Summary, &o) == nil {
			out = append(out, o)
		}
	}
	return out, nil
}
