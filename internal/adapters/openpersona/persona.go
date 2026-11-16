// Package openpersona adapts OpenPersona: the persona pack runs in process
// (read-only), the user memory is an HTTP client to `openpersona serve`.
// OpenPersona types never leave this package (rule D6).
package openpersona

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"sync"

	"github.com/openpersona/openpersona/persona"

	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
)

// Persona serves a trained pack.
type Persona struct {
	p        *persona.Persona
	version  string
	defUnits int
}

// Open loads a pack read-only. The version is a short hash of the file, so
// every knowledge reference carries the pack it came from (AI_SYSTEM_DESIGN
// §4.2 "Rujukan").
func Open(path string, unitsPerQuery int) (*Persona, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("persona pack: %w", err)
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	f.Close()
	if err != nil {
		return nil, err
	}
	p, err := persona.Open(path)
	if err != nil {
		return nil, err
	}
	if unitsPerQuery <= 0 {
		unitsPerQuery = 4
	}
	return &Persona{p: p, version: "pack-" + hex.EncodeToString(h.Sum(nil))[:12], defUnits: unitsPerQuery}, nil
}

// Close releases the pack.
func (p *Persona) Close() error { return p.p.Close() }

// Core is the identity block for the stable prompt prefix.
func (p *Persona) Core() string { return p.p.Core }

// Version is the pack hash.
func (p *Persona) Version() string { return p.version }

// Name is the persona's name from its spec.
func (p *Persona) Name() string { return p.p.Spec.Name }

// Retrieve runs the queries in parallel (no LLM, milliseconds each), merges
// units by node id, keeps the best score, and trims to the token budget. A
// query whose results came only from the entity-graph channel, or none at
// all, is reported as a gap: the pack does not really know.
func (p *Persona) Retrieve(ctx context.Context, queries []ports.KnowledgeQuery, budgetTokens int) (ports.KnowledgePack, error) {
	type result struct {
		q     ports.KnowledgeQuery
		units []ports.KnowledgeUnit
		gap   bool
		err   error
	}
	results := make([]result, len(queries))
	var wg sync.WaitGroup
	for i, q := range queries {
		wg.Add(1)
		go func(i int, q ports.KnowledgeQuery) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					results[i] = result{q: q, err: fmt.Errorf("persona retrieve panic: %v", r)}
				}
			}()
			n := q.Units
			if n <= 0 {
				n = p.defUnits
			}
			k, err := p.p.Retrieve(ctx, q.Text, persona.RetrieveOptions{Units: n, Sources: -1})
			if err != nil {
				results[i] = result{q: q, err: err}
				return
			}
			res := result{q: q, gap: true}
			for _, it := range k.Units {
				for _, ch := range it.Channels {
					if ch == "units" {
						res.gap = false
					}
				}
				res.units = append(res.units, ports.KnowledgeUnit{
					Ref: p.version + ":" + strconv.FormatInt(it.Node.ID, 10), Kind: string(it.Node.Kind),
					Title: it.Node.Title, Body: it.Node.Body, Score: it.Score, Purpose: q.Purpose,
				})
			}
			results[i] = res
		}(i, q)
	}
	wg.Wait()

	pack := ports.KnowledgePack{Units: []ports.KnowledgeUnit{}, Gaps: []string{}}
	best := map[string]int{}
	for _, r := range results {
		if r.err != nil {
			return pack, fmt.Errorf("%w: %v", ports.ErrUnavailable, r.err)
		}
		if r.gap {
			pack.Gaps = append(pack.Gaps, r.q.Purpose)
		}
		for _, u := range r.units {
			if i, ok := best[u.Ref]; ok {
				if u.Score > pack.Units[i].Score {
					pack.Units[i].Score = u.Score
				}
				continue
			}
			best[u.Ref] = len(pack.Units)
			pack.Units = append(pack.Units, u)
		}
	}
	sort.SliceStable(pack.Units, func(i, j int) bool { return pack.Units[i].Score > pack.Units[j].Score })
	if budgetTokens > 0 {
		kept := pack.Units[:0]
		for _, u := range pack.Units {
			t := platform.EstimateTokens(u.Title + "\n" + u.Body)
			if pack.Tokens+t > budgetTokens {
				continue
			}
			pack.Tokens += t
			kept = append(kept, u)
		}
		pack.Units = kept
	}
	return pack, nil
}

var _ ports.PersonaKnowledge = (*Persona)(nil)
