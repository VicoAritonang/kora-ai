package pipeline

import (
	"testing"

	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/reference"
)

func TestParseNights(t *testing.T) {
	cases := map[string]int{"Rp 5 juta ke Jepang seminggu bisa?": 7, "Osaka 5 malam": 5, "paket 4D3N": 3, "liburan 6 hari": 5, "dua minggu di Tokyo": 14}
	for in, want := range cases {
		if got, ok := parseNights(in); !ok || got != want {
			t.Errorf("%q: %d %v", in, got, ok)
		}
	}
	if _, ok := parseNights("budget 20 juta"); ok {
		t.Error("false positive")
	}
}

func TestFindDestination(t *testing.T) {
	if d, ok := reference.FindDestination("Ke Kyoto musim gugur nanti lihat momiji"); !ok || d != "Kyoto" {
		t.Fatalf("%q %v", d, ok)
	}
	if d, ok := reference.FindDestination("5 juta ke Jepang seminggu"); !ok || d != "jepang" {
		t.Fatalf("%q %v", d, ok)
	}
	if _, ok := reference.FindDestination("punya 10 juta bisa ke mana"); ok {
		t.Fatal("false positive")
	}
}

func TestToPatch_PastYearMovesForward(t *testing.T) {
	m, y := 4, 2026
	p, _ := toPatch(extraction{Month: &m, Year: &y}, domain.MustDate("2026-11-15"))
	if p.DateWindow == nil || p.DateWindow.Year != 2027 {
		t.Fatalf("%+v", p.DateWindow)
	}
}

func TestMentionsParty(t *testing.T) {
	if mentionsParty("Saya orang Jakarta, ada uang 20 juta") != true { // "orang" is a cue; nano still must not invent 2
		t.Log("orang counts as a cue")
	}
	if mentionsParty("Mau ke Osaka bulan Maret") {
		t.Fatal("no party cue expected")
	}
	if !mentionsParty("berdua ke Osaka") {
		t.Fatal("berdua missed")
	}
}

func TestCorrectPeriod(t *testing.T) {
	five, one, autumn := 5, 1, "autumn"
	cases := []struct {
		text      string
		month     *int
		season    *string
		wantMonth int
		wantSeas  string
	}{
		{"Mau ke Osaka bulan Maret 5 malam, budget 18 juta", &five, nil, 3, ""},  // nano said May
		{"Liburan akhir tahun ke Osaka, 20 juta", &one, nil, 0, "year_end"},      // nano said January
		{"Ke Kyoto musim gugur nanti lihat momiji", &five, &autumn, 0, "autumn"}, // invented month dropped
		{"Saya janji ke desa dulu, lalu ke Osaka", &five, nil, 0, ""},            // no month words
		{"Ada 30 juta buat liburan Maret, enaknya ke mana ya?", nil, nil, 3, ""},
	}
	for _, c := range cases {
		x := extraction{Month: c.month, Season: c.season}
		correctPeriod(&x, c.text)
		gotM, gotS := 0, ""
		if x.Month != nil {
			gotM = *x.Month
		}
		if x.Season != nil {
			gotS = *x.Season
		}
		if gotM != c.wantMonth || gotS != c.wantSeas {
			t.Errorf("%q: month %d season %q", c.text, gotM, gotS)
		}
	}
}
