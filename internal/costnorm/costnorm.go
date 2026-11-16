// Package costnorm serves structured reference costs (cost_norms) to the
// package builder, and the daily FX table they are converted with. Norm
// numbers are curated, never produced by an LLM (AI_SYSTEM_DESIGN §4.1).
package costnorm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/packagebuilder"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/store/postgres"
)

// ErrNoNorms is returned when a destination has no norms at all.
var ErrNoNorms = errors.New("costnorm: no norms for destination")

// Service reads norms with a short in-process cache.
type Service struct {
	DB    *postgres.DB
	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	norms []domain.Norm
	at    time.Time
}

// New returns the service.
func New(db *postgres.DB) *Service { return &Service{DB: db, cache: map[string]cached{}} }

func (s *Service) all(ctx context.Context, dest string) ([]domain.Norm, error) {
	s.mu.Lock()
	if c, ok := s.cache[dest]; ok && time.Since(c.at) < 5*time.Minute {
		s.mu.Unlock()
		return c.norms, nil
	}
	s.mu.Unlock()
	norms, err := s.DB.Q().ListNorms(ctx, dest)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[dest] = cached{norms, time.Now()}
	s.mu.Unlock()
	return norms, nil
}

// ForTrip returns the norms valid on the trip date, shaped for the builder.
// Missing categories are left empty; the builder records them as gaps rather
// than inventing defaults (MODULE_SPEC §13a).
func (s *Service) ForTrip(ctx context.Context, dest string, on domain.Date) (packagebuilder.CostNorms, []domain.Norm, error) {
	all, err := s.all(ctx, dest)
	if err != nil {
		return packagebuilder.CostNorms{}, nil, err
	}
	if len(all) == 0 {
		return packagebuilder.CostNorms{}, nil, fmt.Errorf("%w: %s", ErrNoNorms, dest)
	}
	out := packagebuilder.CostNorms{Meals: map[string]domain.Norm{}, LocalDaily: map[string]domain.Norm{}, Attractions: map[uuid.UUID]domain.Norm{}}
	var valid []domain.Norm
	for _, n := range all {
		if !n.ValidOn(on) {
			continue
		}
		valid = append(valid, n)
		switch n.Category {
		case "meal":
			out.Meals[n.Tier] = n
		case "local_transport_daily":
			out.LocalDaily[n.Tier] = n
		case "transport_link":
			if at, _ := n.Details["airport_transfer"].(bool); at && out.AirportTransfer == nil {
				nn := n
				out.AirportTransfer = &nn
			}
		case "visa":
			if out.Visa == nil {
				nn := n
				out.Visa = &nn
			}
		case "attraction":
			out.Attractions[n.ID] = n
		}
	}
	return out, valid, nil
}

// Attractions returns attractions matching any of the tags (all when empty).
func (s *Service) Attractions(ctx context.Context, dest string, tags []string, on domain.Date) ([]domain.Norm, error) {
	all, err := s.all(ctx, dest)
	if err != nil {
		return nil, err
	}
	var out []domain.Norm
	for _, n := range all {
		if n.Category != "attraction" || !n.ValidOn(on) {
			continue
		}
		if len(tags) == 0 {
			out = append(out, n)
			continue
		}
		for _, t := range n.Tags() {
			for _, want := range tags {
				if strings.EqualFold(t, want) {
					out = append(out, n)
					goto next
				}
			}
		}
	next:
	}
	return out, nil
}

// Currencies lists the currencies the norms of a destination use.
func Currencies(norms []domain.Norm) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range norms {
		if !seen[n.Amount.Currency] {
			seen[n.Amount.Currency] = true
			out = append(out, n.Amount.Currency)
		}
	}
	return out
}

// ImportFile loads a curated norms file (data/norms/*.json) into the table.
func (s *Service) ImportFile(ctx context.Context, path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var doc struct {
		Destination string `json:"destination_code"`
		Status      string `json:"status"`
		SourceLabel string `json:"source_label"`
		VerifiedAt  string `json:"verified_at"`
		Norms       []struct {
			Key         string         `json:"key"`
			Category    string         `json:"category"`
			Tier        string         `json:"tier"`
			Label       string         `json:"label"`
			Unit        string         `json:"unit"`
			Amount      int64          `json:"amount"`
			Currency    string         `json:"currency"`
			ChildFactor *float64       `json:"child_factor"`
			Details     map[string]any `json:"details"`
			SourceLabel string         `json:"source_label"`
			SourceURL   string         `json:"source_url"`
			VerifiedAt  string         `json:"verified_at"`
		} `json:"norms"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return 0, err
	}
	n := 0
	for _, x := range doc.Norms {
		ver := x.VerifiedAt
		if ver == "" {
			ver = doc.VerifiedAt
		}
		vd, err := domain.ParseDate(ver)
		if err != nil {
			return n, fmt.Errorf("%s: %w", x.Key, err)
		}
		det := x.Details
		if det == nil {
			det = map[string]any{}
		}
		det["key"], det["status"] = x.Key, doc.Status
		src := x.SourceLabel
		if src == "" {
			src = doc.SourceLabel
		}
		norm := domain.Norm{
			// Same stable id as scripts/build-schema.mjs (UUIDv5-style SHA-1).
			ID:          StableID(doc.Destination + ":" + x.Key),
			Destination: doc.Destination, Category: x.Category, Tier: x.Tier, Label: x.Label, Unit: x.Unit,
			Amount: domain.Money{Amount: x.Amount, Currency: x.Currency}, ChildFactor: x.ChildFactor, Details: det,
			SourceLabel: src, SourceURL: x.SourceURL, VerifiedAt: vd,
		}
		if err := s.DB.Q().UpsertNorm(ctx, norm); err != nil {
			return n, fmt.Errorf("%s: %w", x.Key, err)
		}
		n++
	}
	s.mu.Lock()
	delete(s.cache, doc.Destination)
	s.mu.Unlock()
	return n, nil
}

// StableID derives the norm id from "<destination>:<key>" exactly like the
// seed generator, so SQL seeds and Go imports agree.
func StableID(s string) uuid.UUID {
	return uuid.NewSHA1(uuid.Nil, []byte(s))
}

// ------------------------------------------------------------------- FX --

// FX resolves daily rates into IDR with persistence and fallbacks: today's
// stored rate → provider → newest stored rate → static development rates.
type FX struct {
	DB       *postgres.DB
	Provider ports.FXProvider
	Fallback ports.FXProvider
	Clock    platform.Clock
	Log      *slog.Logger
	mu       sync.Mutex
	mem      map[string]domain.FXQuote
}

// NewFX returns the FX service.
func NewFX(db *postgres.DB, p, fallback ports.FXProvider, clock platform.Clock, log *slog.Logger) *FX {
	return &FX{DB: db, Provider: p, Fallback: fallback, Clock: clock, Log: log, mem: map[string]domain.FXQuote{}}
}

// ToIDR returns the quote currency→IDR for today.
func (f *FX) ToIDR(ctx context.Context, currency string) (domain.FXQuote, error) {
	today := domain.DateOf(f.Clock.Now())
	if currency == "IDR" {
		return domain.Identity("IDR", today), nil
	}
	key := currency + today.String()
	f.mu.Lock()
	if q, ok := f.mem[key]; ok {
		f.mu.Unlock()
		return q, nil
	}
	f.mu.Unlock()
	remember := func(q domain.FXQuote) domain.FXQuote {
		f.mu.Lock()
		f.mem[key] = q
		f.mu.Unlock()
		return q
	}
	// 1. A rate already stored for today.
	if q, err := f.DB.Q().LatestFXRate(ctx, currency, "IDR", today); err == nil && q.Date == today {
		return remember(q), nil
	}
	// 2. The provider (stored for the other instances and the worker).
	if f.Provider != nil {
		q, err := f.Provider.Rate(ctx, currency, "IDR", today)
		if err == nil {
			if serr := f.DB.Q().SaveFXRate(ctx, q); serr != nil && f.Log != nil {
				f.Log.Warn("save fx rate failed", "err", serr)
			}
			return remember(q), nil
		}
		if f.Log != nil {
			f.Log.Warn("fx provider failed; falling back", "currency", currency, "err", err)
		}
	}
	// 3. The newest stored rate; invariant 8 flags it once it is too old.
	if q, err := f.DB.Q().LatestFXRate(ctx, currency, "IDR", today); err == nil {
		return remember(q), nil
	}
	// 4. Static development rates, labeled as such.
	if f.Fallback != nil {
		q, err := f.Fallback.Rate(ctx, currency, "IDR", today)
		if err == nil {
			return remember(q), nil
		}
	}
	return domain.FXQuote{}, fmt.Errorf("%w: no FX rate for %s", ports.ErrUnavailable, currency)
}

// Table builds an FX table for several currencies.
func (f *FX) Table(ctx context.Context, currencies ...string) (domain.FXTable, error) {
	t := domain.FXTable{}
	for _, c := range currencies {
		if c == "IDR" || c == "" {
			continue
		}
		if _, ok := t[c]; ok {
			continue
		}
		q, err := f.ToIDR(ctx, c)
		if err != nil {
			return t, err
		}
		t[c] = q
	}
	return t, nil
}

// Convert converts an amount to IDR, returning the IDR amount and the quote.
func (f *FX) Convert(ctx context.Context, m domain.Money) (int64, domain.FXQuote, error) {
	q, err := f.ToIDR(ctx, m.Currency)
	if err != nil {
		return 0, domain.FXQuote{}, err
	}
	if m.Currency == "IDR" {
		return m.Amount, q, nil
	}
	out, err := q.Convert(m)
	return out.Amount, q, err
}
