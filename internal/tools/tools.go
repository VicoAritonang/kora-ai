// Package tools implements the planner's tools (AI_SYSTEM_DESIGN §6). Each
// tool validates arguments, calls a port, stores the full result as an
// artifact and returns a compact digest. No tool accepts a price argument.
package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/agent"
	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/packagebuilder"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
)

// Store persists offers and artifacts (implemented over Postgres in app/).
type Store interface {
	SaveFlightOffer(ctx context.Context, o domain.FlightOffer) error
	SaveStayOffer(ctx context.Context, o domain.StayOffer) error
	InsertArtifact(ctx context.Context, id, runID uuid.UUID, tool string, args, result any, digest string) error
	CachedFlights(ctx context.Context, searchKey string, now time.Time) ([]domain.FlightOffer, error)
	CachedStays(ctx context.Context, searchKey string, now time.Time) ([]domain.StayOffer, error)
}

// Converter converts money into rupiah with the day's rate.
type Converter interface {
	Convert(ctx context.Context, m domain.Money) (int64, domain.FXQuote, error)
}

// PriceIndex provides labeled estimates (Destination Cost Index).
type PriceIndex interface {
	FlightEstimate(ctx context.Context, origin, dest string, month domain.Date) (domain.Estimate, error)
	StayEstimate(ctx context.Context, dest string, month domain.Date, tier domain.Tier) (domain.Estimate, error)
}

// Attractions lists attraction norms.
type Attractions interface {
	Attractions(ctx context.Context, dest string, tags []string, on domain.Date) ([]domain.Norm, error)
}

// Cache is a JSON cache (Redis).
type Cache interface {
	GetJSON(ctx context.Context, key string, out any) error
	SetJSON(ctx context.Context, key string, v any, ttl time.Duration) error
}

// Deps are the dependencies shared by all tools.
type Deps struct {
	Store       Store
	Flights     ports.FlightSupplier
	Stays       ports.StaySupplier
	Activities  ports.ActivitySupplier // optional (Phase 2)
	FX          Converter
	Index       PriceIndex
	Attractions Attractions
	Persona     ports.PersonaKnowledge
	Memory      ports.UserMemory
	MemoryOn    bool
	Researcher  ports.ModelProvider
	ResearchModel string
	Cache       Cache
	Clock       platform.Clock
	Limits      agent.Limits
	Policy      packagebuilder.Policy
	Window      int // supplier sales window in days
	Areas       func(dest string) []string
}

// cacheMargin is the validity a cached offer must still have when served, so
// it does not expire between the search and the package build (MODULE_SPEC
// §9: offers expiring mid-run).
const cacheMargin = 10 * time.Minute

// All returns every tool in a stable order (stable prompt prefix).
func All(d *Deps) []agent.Tool {
	return []agent.Tool{
		&SearchFlights{d}, &SearchStays{d}, &SearchActivities{d}, &KGQuery{d},
		&GetUserMemory{d}, &WebResearch{d}, &ProposeDayPlan{d}, &BuildPackages{d}, &PresentToUser{d},
	}
}

// decode parses args strictly: unknown fields (a price smuggled in) fail.
func decode(args json.RawMessage, v any) error {
	dec := json.NewDecoder(strings.NewReader(string(args)))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func searchKey(parts ...any) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%v|", p)
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func digest(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// sanitize strips control and markup characters from untrusted supplier text
// and caps its length (P4: content from outside is data).
func sanitize(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '<' || r == '>' || r == '`' || r == '[' || r == ']' || r == '{' || r == '}':
			return -1
		case unicode.IsControl(r):
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) > n {
		s = string([]rune(s)[:n]) + "…"
	}
	return s
}

func shortDate(d domain.Date) string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%d %s", d.Day, []string{"", "Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}[d.Month])
}

func (d *Deps) artifact(ctx context.Context, rc *agent.RunContext, tool string, args, result any, dig string) uuid.UUID {
	id := platform.NewID()
	if d.Store != nil {
		if err := d.Store.InsertArtifact(ctx, id, rc.RunID, tool, args, result, dig); err != nil {
			return uuid.Nil
		}
	}
	return id
}

// dateInWindow accepts departures inside the trip window, widened by the
// flexibility days (default ±7 for fuzzy windows).
func dateInWindow(req domain.TripRequest, d domain.Date) bool {
	flex := req.Window.FlexDays
	if flex == 0 {
		flex = 7
	}
	start, end := req.WindowStart.AddDays(-flex), req.WindowEnd.AddDays(flex)
	if req.WindowStart.IsZero() {
		return true
	}
	return !d.Before(start) && !d.After(end)
}
