package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// Date is a civil date without a time zone, serialized as "2006-01-02".
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// DateOf returns the civil date of t in t's location.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{y, m, d}
}

// ParseDate parses "2006-01-02".
func ParseDate(s string) (Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return Date{}, fmt.Errorf("bad date %q", s)
	}
	return DateOf(t), nil
}

// MustDate parses or panics; for tests and constant tables.
func MustDate(s string) Date {
	d, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

// IsZero reports whether the date is unset.
func (d Date) IsZero() bool { return d.Year == 0 }

// Time returns midnight UTC of the date.
func (d Date) Time() time.Time { return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC) }

// AddDays returns the date n days later.
func (d Date) AddDays(n int) Date { return DateOf(d.Time().AddDate(0, 0, n)) }

// DaysUntil returns the number of days from d to o (negative if o is earlier).
func (d Date) DaysUntil(o Date) int {
	return int(o.Time().Sub(d.Time()).Hours() / 24)
}

// Before reports whether d is strictly before o.
func (d Date) Before(o Date) bool { return d.Time().Before(o.Time()) }

// After reports whether d is strictly after o.
func (d Date) After(o Date) bool { return d.Time().After(o.Time()) }

// FirstOfMonth returns the first day of d's month.
func (d Date) FirstOfMonth() Date { return Date{d.Year, d.Month, 1} }

// String renders "2006-01-02".
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

// MarshalJSON renders the date as a string, or null when unset.
func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(d.String())
}

// UnmarshalJSON accepts "2006-01-02" or null.
func (d *Date) UnmarshalJSON(b []byte) error {
	if string(b) == "null" || string(b) == `""` {
		*d = Date{}
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	p, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = p
	return nil
}
