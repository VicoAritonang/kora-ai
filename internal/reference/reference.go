// Package reference holds static reference data: supported origins and
// destinations, their airports and aliases. Place normalization (name →
// code) is done here by code, never by the model (MODULE_SPEC §5).
package reference

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/koraai/koraai/internal/domain"
)

// City is a supported origin or destination.
type City struct {
	Code       string   `json:"code"`
	Label      string   `json:"label"`
	Country    string   `json:"country"`
	Airports   []string `json:"airports"`
	TimeZone   string   `json:"time_zone"`
	Aliases    []string `json:"-"`
	Currency   string   `json:"currency"`
	// Areas are the stay areas the agent may search (destinations only).
	Areas []string `json:"areas,omitempty"`
}

// Origins are the departure cities (TECHNICAL_SPEC §2).
var Origins = []City{
	{Code: "JKT", Label: "Jakarta", Country: "ID", Airports: []string{"CGK"}, TimeZone: "Asia/Jakarta", Currency: "IDR",
		Aliases: []string{"jakarta", "jkt", "cgk", "soekarno hatta", "soetta", "jabodetabek", "tangerang", "bekasi", "depok", "bogor"}},
	{Code: "SUB", Label: "Surabaya", Country: "ID", Airports: []string{"SUB"}, TimeZone: "Asia/Jakarta", Currency: "IDR",
		Aliases: []string{"surabaya", "sby", "sub", "juanda", "sidoarjo"}},
	{Code: "DPS", Label: "Denpasar (Bali)", Country: "ID", Airports: []string{"DPS"}, TimeZone: "Asia/Makassar", Currency: "IDR",
		Aliases: []string{"denpasar", "bali", "dps", "ngurah rai", "kuta"}},
}

// Destinations are the supported destination cities.
var Destinations = []City{
	{Code: "OSA", Label: "Osaka", Country: "JP", Airports: []string{"KIX", "ITM"}, TimeZone: "Asia/Tokyo", Currency: "JPY",
		Aliases: []string{"osaka", "oosaka", "kansai", "kix", "namba", "umeda", "dotonbori"},
		Areas:   []string{"Namba", "Umeda", "Shin-Osaka", "Shinsaibashi", "Tennoji", "Kitahama", "Bay Area"}},
	{Code: "UKY", Label: "Kyoto", Country: "JP", Airports: []string{"KIX"}, TimeZone: "Asia/Tokyo", Currency: "JPY",
		Aliases: []string{"kyoto", "kioto", "gion", "arashiyama"},
		Areas:   []string{"Kyoto Station", "Gion", "Kawaramachi", "Karasuma", "Higashiyama", "Arashiyama"}},
	{Code: "TYO", Label: "Tokyo", Country: "JP", Airports: []string{"NRT", "HND"}, TimeZone: "Asia/Tokyo", Currency: "JPY",
		Aliases: []string{"tokyo", "tokio", "shinjuku", "shibuya", "asakusa", "narita", "haneda"},
		Areas:   []string{"Shinjuku", "Shibuya", "Asakusa", "Ueno", "Ginza", "Marunouchi", "Ryogoku"}},
}

// Countries map a country-level mention to its default city (an assumption
// recorded in the trip state).
var Countries = map[string]struct {
	Code, Label, DefaultCity string
}{
	"jepang": {"JP", "Jepang", "OSA"}, "japan": {"JP", "Jepang", "OSA"}, "nihon": {"JP", "Jepang", "OSA"},
}

// Unsupported are places users ask for that Kora does not cover yet; the
// router answers them as out of scope (FR-5) instead of guessing.
var Unsupported = []string{
	"seoul", "korea", "busan", "bangkok", "thailand", "singapura", "singapore", "kuala lumpur", "malaysia", "hong kong",
	"taipei", "taiwan", "beijing", "shanghai", "china", "tiongkok", "vietnam", "hanoi", "ho chi minh", "manila", "filipina",
	"paris", "london", "eropa", "europe", "amerika", "new york", "sydney", "australia", "melbourne", "dubai", "turki", "istanbul",
	"mekkah", "makkah", "madinah", "umroh", "sapporo", "hokkaido", "fukuoka", "okinawa", "nagoya", "hiroshima", "nara only",
	"lombok", "yogyakarta", "jogja", "labuan bajo", "raja ampat", "bandung", "medan", "makassar",
}

// Fold lowercases and strips accents ("Ōsaka" → "osaka").
func Fold(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		out = s
	}
	return strings.ToLower(strings.TrimSpace(out))
}

func match(c City, s string) bool {
	f := Fold(s)
	if strings.EqualFold(c.Code, f) || Fold(c.Label) == f {
		return true
	}
	for _, a := range c.Aliases {
		if a == f {
			return true
		}
	}
	return false
}

// Origin normalizes an origin mention.
func Origin(s string) (domain.Place, bool) {
	for _, c := range Origins {
		if match(c, s) {
			return domain.Place{Code: c.Code, Label: c.Label, Level: "city"}, true
		}
	}
	return domain.Place{}, false
}

// Destination normalizes a destination mention. Country-level mentions
// return the country with level "country".
func Destination(s string) (domain.Place, bool) {
	for _, c := range Destinations {
		if match(c, s) {
			return domain.Place{Code: c.Code, Label: c.Label, Level: "city"}, true
		}
	}
	if c, ok := Countries[Fold(s)]; ok {
		return domain.Place{Code: c.Code, Label: c.Label, Level: "country"}, true
	}
	return domain.Place{}, false
}

// IsUnsupported reports whether s names a known but unsupported place.
func IsUnsupported(s string) bool {
	f := Fold(s)
	for _, u := range Unsupported {
		if f == u || strings.Contains(f, u) {
			return true
		}
	}
	return false
}

// MentionsUnsupported scans free text for an unsupported destination and
// returns it.
func MentionsUnsupported(text string) (string, bool) {
	f := " " + Fold(text) + " "
	for _, u := range Unsupported {
		if strings.Contains(f, " "+u+" ") || strings.Contains(f, " "+u+",") || strings.Contains(f, " "+u+"?") || strings.Contains(f, " "+u+".") {
			return u, true
		}
	}
	return "", false
}

// FindDestination scans free text for a supported destination (city
// names and aliases first, then country names) and returns the name found.
func FindDestination(text string) (string, bool) {
	words := strings.FieldsFunc(Fold(text), func(r rune) bool { return !unicode.IsLetter(r) && r != '-' })
	for _, c := range Destinations {
		for _, w := range words {
			if w == Fold(c.Label) {
				return c.Label, true
			}
			for _, a := range c.Aliases {
				if w == a && len(a) > 3 {
					return c.Label, true
				}
			}
		}
	}
	for _, w := range words {
		if _, ok := Countries[w]; ok {
			return w, true
		}
	}
	return "", false
}

// DestinationCity returns the city record for a code.
func DestinationCity(code string) (City, bool) {
	for _, c := range Destinations {
		if c.Code == code {
			return c, true
		}
	}
	return City{}, false
}

// OriginCity returns the origin record for a code.
func OriginCity(code string) (City, bool) {
	for _, c := range Origins {
		if c.Code == code {
			return c, true
		}
	}
	return City{}, false
}

// CountryDefault returns the default city for a country code.
func CountryDefault(code string) (domain.Place, bool) {
	for _, c := range Countries {
		if c.Code == code {
			city, _ := DestinationCity(c.DefaultCity)
			return domain.Place{Code: city.Code, Label: city.Label, Level: "city"}, true
		}
	}
	return domain.Place{}, false
}
