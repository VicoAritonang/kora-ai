// Package config loads every setting from environment variables into one
// struct (PROJECT_STRUCTURE §5). The application refuses to start when a
// required value is missing.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the complete runtime configuration.
type Config struct {
	App        App
	DB         DB
	Redis      Redis
	OpenAI     OpenAI
	JEV        JEV
	Models     Models
	Limits     Limits
	Policy     Policy
	Suppliers  Suppliers
	FX         FX
	Persona    Persona
	Memory     Memory
	Guardrails Guardrails
	Tracer     Tracer
	Quota      Quota
	Email      Email
}

// App is process-level settings.
type App struct {
	Env          string // local | staging | production
	Port         int
	PublicURL    string // base URL of the API, e.g. http://localhost:8080
	WebOrigin    string // allowed CORS origin and magic-link host
	LogLevel     string
	SessionKey   string // HMAC key for CSRF tokens
	ServiceToken string // bearer token for /internal/v1
	Build        string
	Destinations []string // supported destination codes
	Origins      []string // supported origin codes
}

// DB is the Postgres connection.
type DB struct {
	URL      string
	MaxConns int32
}

// Redis is the cache/pub-sub connection.
type Redis struct {
	URL string
}

// OpenAI is the generative provider.
type OpenAI struct {
	APIKey  string
	BaseURL string
	Timeout time.Duration
}

// JEV is the decision model, reached through OpenRouter's decisions API.
type JEV struct {
	APIKey    string
	BaseURL   string
	Model     string // pinned version, never "latest"
	Timeout   time.Duration
	Threshold float64
}

// Models maps each role to a model and its fallback (AI_SYSTEM_DESIGN §2).
type Models struct {
	Planner           string
	PlannerFallback   string
	Extractor         string
	ExtractorFallback string
	Narrator          string
	NarratorFallback  string
	Researcher        string
	Summarizer        string
	PlannerEffort     string // auto | minimal | low | medium | high
	ExtractorEffort   string
	NarratorEffort    string
	EffortMax         string // ceiling for auto and escalation
}

// Limits bounds a run (AI_SYSTEM_DESIGN §5).
type Limits struct {
	MaxSteps        int
	FlightSearches  int
	StaySearches    int
	ActivitySearch  int
	WebResearch     int
	CostUSDMicros   int64
	RunTimeout      time.Duration
	ToolTimeout     time.Duration
	WebTimeout      time.Duration
	MaxConcurrent   int
	MaxOutputTokens int
}

// Policy holds package-builder policy (MODULE_SPEC §9).
type Policy struct {
	ContingencyPct    int // percent of budget
	FXBufferPct       int // percent of foreign-currency estimate items
	FXMaxAgeDays      int
	MinStayRating     float64
	BuilderVersion    string
	BookingWindowDays int
}

// Suppliers selects and configures supplier adapters.
type Suppliers struct {
	FlightProvider string // sandbox | duffel
	StayProvider   string // sandbox | liteapi
	DuffelToken    string
	DuffelBaseURL  string
	LiteAPIKey     string
	LiteAPIBaseURL string
	LiteAPIMargin  int
	FlightTTL      time.Duration
	StayTTL        time.Duration
}

// FX configures the exchange-rate provider.
type FX struct {
	Provider string // frankfurter | static
	BaseURL  string
}

// Persona configures the OpenPersona pack.
type Persona struct {
	PackPath      string
	UnitsPerQuery int
	BudgetTokens  int
}

// Memory configures the OpenPersona memory service.
type Memory struct {
	Enabled      bool
	URL          string
	BudgetTokens int
}

// Guardrails configures guard-rails-engine.
type Guardrails struct {
	Mode       string // engine | off (off is for local debugging only)
	PolicyFile string
	ToolsFile  string
	Secret     string
}

// Tracer selects event sinks.
type Tracer struct {
	LabURL   string
	LabToken string
	// Agent Reliability Lab (arl/1 over WebSocket), a development tool:
	// active only when Stage is "dev", URL is set and APP_ENV is local.
	ARL ARL
}

// ARL configures the Agent Reliability Lab SDK. Variable names follow the
// SDK so the Lab's own docs apply unchanged.
type ARL struct {
	Stage    string // STAGE
	URL      string // LAB_WS_URL, e.g. ws://localhost:8000/api/agent-lab/ws
	Token    string // LAB_TOKEN (the Lab's ARL_AUTH_TOKEN)
	Project  string // LAB_PROJECT_ID
	Required bool   // LAB_REQUIRED: strict mode for integration tests
	Service  string // LAB_SERVICE, shown as the service name in the Lab
}

// Enabled reports whether runs should be streamed to the Lab.
func (a ARL) Enabled() bool { return strings.EqualFold(a.Stage, "dev") && a.URL != "" }

// Quota bounds per-user usage (API_SPEC §8).
type Quota struct {
	GuestRunsPerDay        int
	UserRunsPerDay         int
	RequestsPerMinute      int
	MaxActiveRunsPerUser   int
	GuestSessionsPerIPHour int
}

// Email configures outbound mail for magic links and reminders.
type Email struct {
	Provider string // log | smtp
	From     string
	SMTPAddr string
	SMTPUser string
	SMTPPass string
}

// Load reads the environment (after optionally loading .env files) and
// validates required keys.
func Load() (*Config, error) {
	LoadDotEnv(".env", "../.env", "../../.env")
	var errs []string
	req := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(os.Getenv(k)); v != "" {
				return v
			}
		}
		errs = append(errs, strings.Join(keys, " or ")+" is required")
		return ""
	}

	c := &Config{}
	c.App = App{
		Env:          str("APP_ENV", "local"),
		Port:         num("APP_PORT", 8080),
		PublicURL:    str("APP_PUBLIC_URL", "http://localhost:8080"),
		WebOrigin:    str("APP_WEB_ORIGIN", "http://localhost:3000"),
		LogLevel:     str("APP_LOG_LEVEL", "info"),
		SessionKey:   str("APP_SESSION_KEY", ""),
		ServiceToken: str("APP_SERVICE_TOKEN", ""),
		Build:        str("APP_BUILD", "dev"),
		Destinations: list("APP_DESTINATIONS", "OSA,UKY,TYO"),
		Origins:      list("APP_ORIGINS", "JKT,SUB,DPS"),
	}
	c.DB = DB{URL: normalizePGURL(req("DB_URL", "NEON_DB_URL")), MaxConns: int32(num("DB_MAX_CONNS", 10))}
	c.Redis = Redis{URL: str("REDIS_URL", "redis://localhost:6379/0")}
	c.OpenAI = OpenAI{
		APIKey:  req("OPENAI_API_KEY"),
		BaseURL: str("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		Timeout: dur("OPENAI_TIMEOUT", 60*time.Second),
	}
	c.JEV = JEV{
		APIKey:    firstEnv("JEV_API_KEY", "OPENROUTER_API_KEY"),
		BaseURL:   str("JEV_BASE_URL", "https://openrouter.ai/api/alpha"),
		Model:     str("JEV_MODEL", "typesafe/jev-1.13"),
		Timeout:   dur("JEV_TIMEOUT", 15*time.Second),
		Threshold: flt("JEV_THRESHOLD", 0.80),
	}
	c.Models = Models{
		Planner:           str("MODEL_PLANNER", "gpt-5-mini"),
		PlannerFallback:   str("MODEL_PLANNER_FALLBACK", "gpt-4.1-mini"),
		Extractor:         str("MODEL_EXTRACTOR", "gpt-5-nano"),
		ExtractorFallback: str("MODEL_EXTRACTOR_FALLBACK", "gpt-4o-mini"),
		Narrator:          str("MODEL_NARRATOR", "gpt-5-mini"),
		NarratorFallback:  str("MODEL_NARRATOR_FALLBACK", "gpt-4.1-mini"),
		Researcher:        str("MODEL_RESEARCHER", "gpt-5-mini"),
		Summarizer:        str("MODEL_SUMMARIZER", "gpt-5-nano"),
		PlannerEffort:     str("MODEL_PLANNER_EFFORT", "auto"),
		ExtractorEffort:   str("MODEL_EXTRACTOR_EFFORT", "auto"),
		NarratorEffort:    str("MODEL_NARRATOR_EFFORT", "auto"),
		EffortMax:         str("MODEL_EFFORT_MAX", "medium"),
	}
	c.Limits = Limits{
		MaxSteps:        num("LIMIT_MAX_STEPS", 12),
		FlightSearches:  num("LIMIT_FLIGHT_SEARCHES", 6),
		StaySearches:    num("LIMIT_STAY_SEARCHES", 6),
		ActivitySearch:  num("LIMIT_ACTIVITY_SEARCHES", 4),
		WebResearch:     num("LIMIT_WEB_RESEARCH", 3),
		CostUSDMicros:   int64(flt("LIMIT_COST_USD", 0.30) * 1_000_000),
		RunTimeout:      dur("LIMIT_RUN_TIMEOUT", 120*time.Second),
		ToolTimeout:     dur("LIMIT_TOOL_TIMEOUT", 15*time.Second),
		WebTimeout:      dur("LIMIT_WEB_TIMEOUT", 30*time.Second),
		MaxConcurrent:   num("LIMIT_MAX_CONCURRENT_RUNS", 20),
		MaxOutputTokens: num("LIMIT_MAX_OUTPUT_TOKENS", 4000),
	}
	c.Policy = Policy{
		ContingencyPct:    num("POLICY_CONTINGENCY_PCT", 10),
		FXBufferPct:       num("POLICY_FX_BUFFER_PCT", 3),
		FXMaxAgeDays:      num("POLICY_FX_MAX_AGE_DAYS", 4),
		MinStayRating:     flt("POLICY_MIN_STAY_RATING", 7.0),
		BuilderVersion:    str("POLICY_BUILDER_VERSION", "pb-1.0.0"),
		BookingWindowDays: num("POLICY_BOOKING_WINDOW_DAYS", 330),
	}
	c.Suppliers = Suppliers{
		FlightProvider: str("SUPPLIER_FLIGHTS", "sandbox"),
		StayProvider:   str("SUPPLIER_STAYS", "sandbox"),
		DuffelToken:    str("DUFFEL_ACCESS_TOKEN", ""),
		DuffelBaseURL:  str("DUFFEL_BASE_URL", "https://api.duffel.com"),
		LiteAPIKey:     str("LITEAPI_API_KEY", ""),
		LiteAPIBaseURL: str("LITEAPI_BASE_URL", "https://api.liteapi.travel/v3.0"),
		LiteAPIMargin:  num("LITEAPI_MARGIN", 0),
		FlightTTL:      dur("SUPPLIER_FLIGHT_TTL", 30*time.Minute),
		StayTTL:        dur("SUPPLIER_STAY_TTL", 60*time.Minute),
	}
	if c.Suppliers.FlightProvider == "duffel" && c.Suppliers.DuffelToken == "" {
		errs = append(errs, "DUFFEL_ACCESS_TOKEN is required when SUPPLIER_FLIGHTS=duffel")
	}
	if c.Suppliers.StayProvider == "liteapi" && c.Suppliers.LiteAPIKey == "" {
		errs = append(errs, "LITEAPI_API_KEY is required when SUPPLIER_STAYS=liteapi")
	}
	c.FX = FX{Provider: str("FX_PROVIDER", "frankfurter"), BaseURL: str("FX_BASE_URL", "https://api.frankfurter.app")}
	c.Persona = Persona{
		PackPath:      str("PERSONA_PACK", "persona/packs/kora.persona"),
		UnitsPerQuery: num("PERSONA_UNITS_PER_QUERY", 4),
		BudgetTokens:  num("PERSONA_BUDGET_TOKENS", 4000),
	}
	c.Memory = Memory{
		Enabled:      boolean("MEMORY_ENABLED", false),
		URL:          str("MEMORY_URL", "http://localhost:8090"),
		BudgetTokens: num("MEMORY_BUDGET_TOKENS", 600),
	}
	c.Guardrails = Guardrails{
		Mode:       str("GUARDRAILS_MODE", "engine"),
		PolicyFile: str("GUARDRAILS_POLICY", "guardrails/policy.yaml"),
		ToolsFile:  str("GUARDRAILS_TOOLS", "guardrails/tools.yaml"),
		Secret:     str("GUARDRAILS_SECRET", ""),
	}
	c.Tracer = Tracer{LabURL: str("TRACER_LAB_URL", ""), LabToken: str("TRACER_LAB_TOKEN", ""), ARL: ARL{
		Stage:    str("STAGE", ""),
		URL:      str("LAB_WS_URL", ""),
		Token:    str("LAB_TOKEN", ""),
		Project:  str("LAB_PROJECT_ID", ""),
		Required: str("LAB_REQUIRED", "false") == "true",
		Service:  str("LAB_SERVICE", "kora/api"),
	}}
	c.Quota = Quota{
		GuestRunsPerDay:        num("QUOTA_GUEST_RUNS_PER_DAY", 10),
		UserRunsPerDay:         num("QUOTA_USER_RUNS_PER_DAY", 30),
		RequestsPerMinute:      num("QUOTA_REQUESTS_PER_MINUTE", 120),
		MaxActiveRunsPerUser:   num("QUOTA_MAX_ACTIVE_RUNS_PER_USER", 2),
		GuestSessionsPerIPHour: num("QUOTA_GUEST_SESSIONS_PER_IP_HOUR", 20),
	}
	c.Email = Email{
		Provider: str("EMAIL_PROVIDER", "log"),
		From:     str("EMAIL_FROM", "Kora <no-reply@kora.local>"),
		SMTPAddr: str("EMAIL_SMTP_ADDR", ""),
		SMTPUser: str("EMAIL_SMTP_USER", ""),
		SMTPPass: str("EMAIL_SMTP_PASS", ""),
	}

	if c.App.Env != "local" {
		if len(c.App.SessionKey) < 32 {
			errs = append(errs, "APP_SESSION_KEY must be at least 32 characters outside local")
		}
		if len(c.Guardrails.Secret) < 32 {
			errs = append(errs, "GUARDRAILS_SECRET must be at least 32 characters outside local")
		}
		if c.Guardrails.Mode != "engine" {
			errs = append(errs, "GUARDRAILS_MODE=off is only allowed in local")
		}
		if c.Tracer.ARL.Enabled() {
			// Lab traces carry model/tool evidence and masked user text; they are a
			// development tool, never a production sink.
			errs = append(errs, "STAGE=dev with LAB_WS_URL (Agent Reliability Lab) is only allowed in local")
		}
	}
	if c.App.SessionKey == "" {
		c.App.SessionKey = "local-dev-session-key-change-me-0123456789"
	}
	if c.Guardrails.Secret == "" {
		c.Guardrails.Secret = "local-dev-guardrails-secret-change-me-0123"
	}
	if len(errs) > 0 {
		return nil, errors.New("config: " + strings.Join(errs, "; "))
	}
	return c, nil
}

// Secure reports whether cookies should carry the Secure flag.
func (c *Config) Secure() bool { return c.App.Env != "local" }

// normalizePGURL drops parameters pgx does not understand. Neon connection
// strings carry channel_binding=require, which pgx would otherwise send to the
// server as a runtime parameter.
func normalizePGURL(raw string) string {
	raw = strings.Trim(strings.TrimSpace(raw), `'"`)
	if strings.HasPrefix(raw, "psql ") {
		raw = strings.Trim(strings.TrimSpace(strings.TrimPrefix(raw, "psql ")), `'"`)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Del("channel_binding")
	u.RawQuery = q.Encode()
	return u.String()
}

func str(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func num(k string, def int) int {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func flt(k string, def float64) float64 {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return def
}

func boolean(k string, def bool) bool {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func dur(k string, def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func list(k, def string) []string {
	v := str(k, def)
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, strings.ToUpper(p))
		}
	}
	return out
}

// LoadDotEnv sets variables from the first files that exist, without
// overriding variables already set in the environment. Values may be quoted;
// a value of the form `psql '...'` (Neon's copy snippet) is unwrapped.
func LoadDotEnv(files ...string) {
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			line := strings.TrimSpace(strings.TrimSuffix(sc.Text(), "\r"))
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "export ")
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.Trim(strings.TrimSpace(v), `'"`)
			if _, set := os.LookupEnv(k); !set {
				os.Setenv(k, v)
			}
		}
		fh.Close()
	}
}

// Snapshot is the subset of configuration recorded on every run
// (runs.config_snapshot) so evaluations can be reproduced.
func (c *Config) Snapshot() map[string]any {
	return map[string]any{
		"models": map[string]string{
			"planner": c.Models.Planner, "extractor": c.Models.Extractor,
			"narrator": c.Models.Narrator, "researcher": c.Models.Researcher,
			"router": c.JEV.Model, "verifier": c.JEV.Model,
		},
		"limits": map[string]any{
			"max_steps": c.Limits.MaxSteps, "flight_searches": c.Limits.FlightSearches,
			"stay_searches": c.Limits.StaySearches, "web_research": c.Limits.WebResearch,
			"cost_usd_micros": c.Limits.CostUSDMicros, "run_timeout_s": int(c.Limits.RunTimeout.Seconds()),
		},
		"policy": map[string]any{
			"contingency_pct": c.Policy.ContingencyPct, "fx_buffer_pct": c.Policy.FXBufferPct,
			"builder_version": c.Policy.BuilderVersion,
		},
		"suppliers":     map[string]string{"flights": c.Suppliers.FlightProvider, "stays": c.Suppliers.StayProvider},
		"effort":        map[string]string{"planner": c.Models.PlannerEffort, "extractor": c.Models.ExtractorEffort, "narrator": c.Models.NarratorEffort, "max": c.Models.EffortMax},
		"jev_threshold": c.JEV.Threshold,
		"build":         c.App.Build,
	}
}

// String hides secrets when the config is printed.
func (c *Config) String() string {
	return fmt.Sprintf("config{env=%s port=%d flights=%s stays=%s}", c.App.Env, c.App.Port, c.Suppliers.FlightProvider, c.Suppliers.StayProvider)
}
