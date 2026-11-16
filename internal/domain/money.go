// Package domain holds the core types and pure rules of the product. It
// imports no other internal package and performs no I/O (rule D1).
package domain

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Money is an amount in the smallest unit of an ISO 4217 currency. Floating
// point is never used for money (ENGINEERING_STANDARDS §1.1).
type Money struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// ErrCurrencyMismatch is returned when two amounts in different currencies
// are combined.
var ErrCurrencyMismatch = errors.New("currency mismatch")

// minorUnits lists the exponent of each supported currency. IDR and JPY have
// no fractional unit in practice.
var minorUnits = map[string]int{
	"IDR": 0, "JPY": 0, "KRW": 0,
	"USD": 2, "SGD": 2, "MYR": 2, "THB": 2, "EUR": 2, "AUD": 2, "HKD": 2, "CNY": 2, "GBP": 2,
}

// SupportedCurrency reports whether c is a currency the system can price.
func SupportedCurrency(c string) bool {
	_, ok := minorUnits[strings.ToUpper(c)]
	return ok
}

// MinorUnits returns the exponent for currency c (2 if unknown).
func MinorUnits(c string) int {
	if e, ok := minorUnits[strings.ToUpper(c)]; ok {
		return e
	}
	return 2
}

// IDR builds an amount in rupiah.
func IDR(amount int64) Money { return Money{Amount: amount, Currency: "IDR"} }

// Add returns m+o, or ErrCurrencyMismatch.
func (m Money) Add(o Money) (Money, error) {
	if m.Currency != o.Currency {
		return Money{}, fmt.Errorf("add %s to %s: %w", o.Currency, m.Currency, ErrCurrencyMismatch)
	}
	return Money{Amount: m.Amount + o.Amount, Currency: m.Currency}, nil
}

// MustAdd adds and panics on mismatch. Only for call sites where a mismatch
// is a programmer error; tests exercise the panic.
func (m Money) MustAdd(o Money) Money {
	r, err := m.Add(o)
	if err != nil {
		panic(err)
	}
	return r
}

// Mul multiplies by an integer quantity.
func (m Money) Mul(q int64) Money { return Money{Amount: m.Amount * q, Currency: m.Currency} }

// IsZero reports whether the amount is zero.
func (m Money) IsZero() bool { return m.Amount == 0 }

// String renders the amount for logs, e.g. "IDR 20000000" or "USD 15.00".
func (m Money) String() string {
	e := MinorUnits(m.Currency)
	if e == 0 {
		return fmt.Sprintf("%s %d", m.Currency, m.Amount)
	}
	div := int64(1)
	for i := 0; i < e; i++ {
		div *= 10
	}
	return fmt.Sprintf("%s %d.%0*d", m.Currency, m.Amount/div, e, abs(m.Amount%div))
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// FXQuote is the rate to convert one major unit of Base into Quote, scaled by
// 1e8 so it can be stored as numeric(18,8) and handled without floats.
type FXQuote struct {
	Base   string `json:"base"`
	Quote  string `json:"quote"`
	RateE8 int64  `json:"rate_e8"`
	Date   Date   `json:"date"`
	Source string `json:"source"`
}

// RateScale is the fixed-point scale of FXQuote.RateE8.
const RateScale = 100_000_000

// Identity returns a 1:1 quote for a currency into itself.
func Identity(c string, on Date) FXQuote {
	return FXQuote{Base: c, Quote: c, RateE8: RateScale, Date: on, Source: "identity"}
}

// RateString renders the rate with 8 decimals for numeric(18,8).
func (q FXQuote) RateString() string {
	return fmt.Sprintf("%d.%08d", q.RateE8/RateScale, q.RateE8%RateScale)
}

// ParseRateE8 parses a decimal rate string ("106.12345678") into RateE8.
func ParseRateE8(s string) (int64, error) {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(s))
	if !ok || r.Sign() <= 0 {
		return 0, fmt.Errorf("bad rate %q", s)
	}
	r.Mul(r, big.NewRat(RateScale, 1))
	q := new(big.Int).Quo(r.Num(), r.Denom())
	return q.Int64(), nil
}

// Convert converts m into the quote currency, rounding UP: overestimating a
// cost is safer for the traveller than underestimating it (MODULE_SPEC §2).
func (q FXQuote) Convert(m Money) (Money, error) {
	if m.Currency != q.Base {
		return Money{}, fmt.Errorf("convert %s with %s/%s quote: %w", m.Currency, q.Base, q.Quote, ErrCurrencyMismatch)
	}
	if q.RateE8 <= 0 {
		return Money{}, fmt.Errorf("convert: non-positive rate %s/%s", q.Base, q.Quote)
	}
	// out = amount * rate * 10^quoteExp / (10^baseExp * 1e8), ceiling.
	num := new(big.Int).Mul(big.NewInt(m.Amount), big.NewInt(q.RateE8))
	num.Mul(num, pow10(MinorUnits(q.Quote)))
	den := new(big.Int).Mul(pow10(MinorUnits(q.Base)), big.NewInt(RateScale))
	out, rem := new(big.Int).QuoRem(num, den, new(big.Int))
	if rem.Sign() > 0 {
		out.Add(out, big.NewInt(1))
	}
	return Money{Amount: out.Int64(), Currency: q.Quote}, nil
}

// AgeDays returns how many whole days old the quote is relative to now.
func (q FXQuote) AgeDays(now time.Time) int {
	return q.Date.DaysUntil(DateOf(now))
}

func pow10(e int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(e)), nil)
}

// FXTable resolves quotes into IDR for a set of currencies.
type FXTable map[string]FXQuote

// ToIDR converts m into rupiah using the table (identity for IDR).
func (t FXTable) ToIDR(m Money) (int64, FXQuote, error) {
	if m.Currency == "IDR" {
		return m.Amount, Identity("IDR", Date{}), nil
	}
	q, ok := t[m.Currency]
	if !ok {
		return 0, FXQuote{}, fmt.Errorf("no FX rate for %s→IDR", m.Currency)
	}
	out, err := q.Convert(m)
	if err != nil {
		return 0, FXQuote{}, err
	}
	return out.Amount, q, nil
}
