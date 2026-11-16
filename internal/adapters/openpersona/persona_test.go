package openpersona

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/koraai/koraai/internal/ports"
)

const pack = "../../../persona/packs/kora.persona"

func open(t *testing.T) *Persona {
	t.Helper()
	if _, err := os.Stat(pack); err != nil {
		t.Skip("persona pack not trained (scripts/train-persona.sh)")
	}
	p, err := Open(pack, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

// T5-1: the pipeline queries produce a knowledge pack for Osaka in autumn.
func TestRetrieve_OsakaAutumn(t *testing.T) {
	p := open(t)
	if !strings.HasPrefix(p.Version(), "pack-") || p.Core() == "" {
		t.Fatalf("version %q core %d", p.Version(), len(p.Core()))
	}
	k, err := p.Retrieve(context.Background(), []ports.KnowledgeQuery{
		{Purpose: "destination_period", Text: "Osaka musim gugur 2027"},
		{Purpose: "special_requests", Units: 6, Text: "festival acara matsuri festival perayaan parade Osaka musim gugur 2027"},
		{Purpose: "documents", Text: "visa Jepang paspor Indonesia"},
	}, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if len(k.Units) < 4 {
		t.Fatalf("only %d units", len(k.Units))
	}
	joined := ""
	for _, u := range k.Units {
		joined += strings.ToLower(u.Title + " " + u.Body)
		if !strings.HasPrefix(u.Ref, p.Version()+":") {
			t.Fatalf("ref %q", u.Ref)
		}
	}
	for _, want := range []string{"danjiri", "visa"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in retrieved units", want)
		}
	}
	if k.Tokens > 4000 {
		t.Errorf("budget exceeded: %d", k.Tokens)
	}
}

// T5-1: a question outside the pack is reported as a gap.
func TestRetrieve_GapOutsidePack(t *testing.T) {
	p := open(t)
	k, err := p.Retrieve(context.Background(), []ports.KnowledgeQuery{{Purpose: "outside", Text: "kriptografi kuantum blockchain"}}, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(k.Gaps) != 1 {
		t.Fatalf("gaps %v units %d", k.Gaps, len(k.Units))
	}
}
