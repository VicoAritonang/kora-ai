package domain

import (
	"errors"
	"testing"
	"testing/quick"
)

func TestMoneyAdd_MixedCurrency_ReturnsError(t *testing.T) {
	_, err := IDR(100).Add(Money{Amount: 1, Currency: "JPY"})
	if !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("want ErrCurrencyMismatch, got %v", err)
	}
}

func TestMoneyMustAdd_MixedCurrency_Panics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	IDR(1).MustAdd(Money{Amount: 1, Currency: "USD"})
}

func TestConvert_RoundsUp(t *testing.T) {
	// 1 JPY = 106.5 IDR; ¥1.201 → 127.906,5 → rounds up to 127.907.
	q := FXQuote{Base: "JPY", Quote: "IDR", RateE8: 10_650_000_000}
	got, err := q.Convert(Money{Amount: 1201, Currency: "JPY"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Amount != 127907 || got.Currency != "IDR" {
		t.Fatalf("got %v", got)
	}
}

func TestConvert_USDCentsToIDR(t *testing.T) {
	// USD 15.00 at 16 500,25 → 247 503,75 → 247 504.
	q := FXQuote{Base: "USD", Quote: "IDR", RateE8: 1_650_025_000_000}
	got, err := q.Convert(Money{Amount: 1500, Currency: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Amount != 247504 {
		t.Fatalf("got %d", got.Amount)
	}
}

func TestConvert_WrongBase_Fails(t *testing.T) {
	q := FXQuote{Base: "USD", Quote: "IDR", RateE8: RateScale}
	if _, err := q.Convert(Money{Amount: 1, Currency: "JPY"}); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("got %v", err)
	}
}

func TestConvert_NeverBelowExact_Property(t *testing.T) {
	f := func(amount uint32, rate uint32) bool {
		if rate == 0 {
			return true
		}
		q := FXQuote{Base: "JPY", Quote: "IDR", RateE8: int64(rate)}
		got, err := q.Convert(Money{Amount: int64(amount), Currency: "JPY"})
		if err != nil {
			return false
		}
		// got*1e8 >= amount*rate and (got-1)*1e8 < amount*rate
		exact := int64(amount) * int64(rate)
		return got.Amount*RateScale >= exact && (got.Amount-1)*RateScale < exact
	}
	if err := quick.Check(f, nil); err != nil {
		t.Fatal(err)
	}
}

func TestParseRateE8(t *testing.T) {
	got, err := ParseRateE8("106.12345678")
	if err != nil || got != 10612345678 {
		t.Fatalf("got %d %v", got, err)
	}
	if _, err := ParseRateE8("-1"); err == nil {
		t.Fatal("negative rate accepted")
	}
}

func TestPaxValidate(t *testing.T) {
	cases := []struct {
		p  Pax
		ok bool
	}{
		{Pax{1, 0}, true}, {Pax{9, 0}, true}, {Pax{0, 1}, false},
		{Pax{5, 5}, false}, {Pax{2, 8}, false}, {Pax{1, 8}, true},
	}
	for _, c := range cases {
		if err := c.p.Validate(); (err == nil) != c.ok {
			t.Errorf("%+v: err=%v", c.p, err)
		}
	}
}

func TestNextSeasonYear_AutumnFromMidNovember_IsNextYear(t *testing.T) {
	// "musim gugur nanti" said on 15 Nov 2026 means autumn 2027 (PLAN §1).
	if y := NextSeasonYear(SeasonAutumn, MustDate("2026-11-15")); y != 2027 {
		t.Fatalf("got %d", y)
	}
	if y := NextSeasonYear(SeasonAutumn, MustDate("2026-08-01")); y != 2026 {
		t.Fatalf("got %d", y)
	}
}

func TestDateWindowRange(t *testing.T) {
	s, e, err := DateWindow{Month: 12, Year: 2026}.Range()
	if err != nil || s.String() != "2026-12-01" || e.String() != "2026-12-31" {
		t.Fatalf("got %s %s %v", s, e, err)
	}
	s, e, _ = DateWindow{Season: SeasonWinter, Year: 2026}.Range()
	if s.String() != "2026-12-01" || e.String() != "2027-02-28" {
		t.Fatalf("winter %s %s", s, e)
	}
	s, e, _ = DateWindow{Season: SeasonYearEnd, Year: 2026}.Range()
	if s.String() != "2026-12-15" || e.String() != "2027-01-05" {
		t.Fatalf("year end %s %s", s, e)
	}
}

func TestSuggestDeparture_StaysInsideLiveWindow(t *testing.T) {
	today := MustDate("2026-11-15")
	start, end, _ := SeasonRange(SeasonAutumn, 2027)
	live := today.AddDays(330) // 2027-10-11
	d := SuggestDeparture(start, end, 6, today, live)
	if d.AddDays(6).After(live) {
		t.Fatalf("return %s after live window %s", d.AddDays(6), live)
	}
	if d.Before(start) {
		t.Fatalf("departure %s before window", d)
	}
}

func TestTripStateApply_UserSlotReplacesAssumption(t *testing.T) {
	s := TripState{}
	s.Normalize()
	n := 5
	s.Nights = &n
	s.SetAssumption(Assumption{Slot: "nights", Value: 5, Reason: "default"})
	six := 6
	if err := s.Apply(TripStatePatch{Nights: &six}); err != nil {
		t.Fatal(err)
	}
	if *s.Nights != 6 || s.HasAssumption("nights") {
		t.Fatalf("state %+v", s)
	}
}

func TestTripStateApply_RejectsBadNights(t *testing.T) {
	s := TripState{}
	bad := 0
	if err := s.Apply(TripStatePatch{Nights: &bad}); err == nil {
		t.Fatal("accepted 0 nights")
	}
}

func TestDateJSONRoundTrip(t *testing.T) {
	var d Date
	if err := d.UnmarshalJSON([]byte(`"2027-10-10"`)); err != nil || d.String() != "2027-10-10" {
		t.Fatalf("%v %v", d, err)
	}
	b, _ := Date{}.MarshalJSON()
	if string(b) != "null" {
		t.Fatalf("zero date: %s", b)
	}
}
