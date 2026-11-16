package conversation

import (
	"fmt"
	"strings"
	"testing"
)

func TestActionPhrasesFollowProfileLocale(t *testing.T) {
	en := phrasesFor("en")
	if got := fmt.Sprintf(en.change, fmt.Sprintf(en.nights, 6)); got != "Change the plan: 6 nights. Please rebuild the packages." {
		t.Errorf("en change: %q", got)
	}
	if got := fmt.Sprintf(en.areaRequest, "Namba"); got != "Stay in the Namba area" {
		t.Errorf("en area request: %q", got)
	}
	id := phrasesFor("id")
	if got := fmt.Sprintf(id.change, fmt.Sprintf(id.nights, 6)); got != "Ubah rencana: 6 malam. Tolong susun ulang paketnya." {
		t.Errorf("id change: %q", got)
	}
	if phrasesFor("fr").cheaper != id.cheaper || phrasesFor("").cheaper != id.cheaper {
		t.Error("unknown locales fall back to Indonesian")
	}
	// Every phrase exists in both languages.
	for lang, p := range phrases {
		for i, v := range []string{p.nights, p.adults, p.children, p.budget, p.destination, p.change, p.shift, p.area, p.areaRequest, p.station, p.stationRequest, p.comfier, p.cheaper} {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s phrase %d is empty", lang, i)
			}
		}
	}
}
