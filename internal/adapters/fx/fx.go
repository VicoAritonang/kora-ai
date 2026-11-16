// Package fx adapts exchange-rate providers to ports.FXProvider. Frankfurter
// (ECB reference rates, no key) is the default; Static is the offline
// fallback. Persistence in fx_rates lives in the currency module, because
// adapters do not import the store (rule D5).
package fx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
)

// Frankfurter reads https://api.frankfurter.app. Weekends and holidays
// return the previous business day, which is the documented fallback to
// "the day before" (MODULE_SPEC §16).
type Frankfurter struct {
	BaseURL string
	HTTP    *platform.HTTPClient
	mu      sync.Mutex
	cache   map[string]domain.FXQuote
}

// NewFrankfurter returns the provider.
func NewFrankfurter(baseURL string) *Frankfurter {
	return &Frankfurter{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: platform.NewHTTPClient("fx", 8*time.Second), cache: map[string]domain.FXQuote{}}
}

// Rate returns base→quote on (or before) the given date.
func (f *Frankfurter) Rate(ctx context.Context, base, quote string, on domain.Date) (domain.FXQuote, error) {
	if base == quote {
		return domain.Identity(base, on), nil
	}
	key := base + quote + on.String()
	f.mu.Lock()
	if q, ok := f.cache[key]; ok {
		f.mu.Unlock()
		return q, nil
	}
	f.mu.Unlock()
	url := fmt.Sprintf("%s/%s?from=%s&to=%s", f.BaseURL, on.String(), base, quote)
	out, _, err := f.HTTP.Do(ctx, "GET", url, nil, map[string]string{"Accept": "application/json"})
	if err != nil {
		return domain.FXQuote{}, fmt.Errorf("%w: fx: %v", ports.ErrUnavailable, err)
	}
	var r struct {
		Date  string                     `json:"date"`
		Rates map[string]json.Number     `json:"rates"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return domain.FXQuote{}, fmt.Errorf("fx: decode: %w", err)
	}
	v, ok := r.Rates[quote]
	if !ok {
		return domain.FXQuote{}, fmt.Errorf("%w: no %s→%s rate", ports.ErrNotFound, base, quote)
	}
	rate, err := domain.ParseRateE8(v.String())
	if err != nil {
		return domain.FXQuote{}, err
	}
	d, err := domain.ParseDate(r.Date)
	if err != nil {
		d = on
	}
	q := domain.FXQuote{Base: base, Quote: quote, RateE8: rate, Date: d, Source: "frankfurter (ECB)"}
	f.mu.Lock()
	f.cache[key] = q
	f.mu.Unlock()
	return q, nil
}

// Static returns fixed rates. Used when the network provider is unreachable
// in local development; every quote is labeled so the UI and the FX-age
// invariant treat it accordingly.
type Static struct {
	Rates map[string]string // currency → IDR per unit
	Clock platform.Clock
}

// DefaultStatic carries rough reference rates for offline development only.
func DefaultStatic(clock platform.Clock) *Static {
	return &Static{Clock: clock, Rates: map[string]string{
		"USD": "16450", "JPY": "106.2", "SGD": "12650", "MYR": "3700", "EUR": "17800", "KRW": "11.9", "THB": "470", "HKD": "2110", "AUD": "10700",
	}}
}

// Rate returns the static rate dated today.
func (s *Static) Rate(_ context.Context, base, quote string, on domain.Date) (domain.FXQuote, error) {
	if base == quote {
		return domain.Identity(base, on), nil
	}
	if quote != "IDR" {
		return domain.FXQuote{}, fmt.Errorf("%w: static rates only quote IDR", ports.ErrNotFound)
	}
	v, ok := s.Rates[base]
	if !ok {
		return domain.FXQuote{}, fmt.Errorf("%w: no static rate for %s", ports.ErrNotFound, base)
	}
	r, err := domain.ParseRateE8(v)
	if err != nil {
		return domain.FXQuote{}, err
	}
	return domain.FXQuote{Base: base, Quote: "IDR", RateE8: r, Date: domain.DateOf(s.Clock.Now()), Source: "static-fallback (dev)"}, nil
}

var (
	_ ports.FXProvider = (*Frankfurter)(nil)
	_ ports.FXProvider = (*Static)(nil)
)
