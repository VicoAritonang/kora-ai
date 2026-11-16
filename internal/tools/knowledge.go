package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/koraai/koraai/internal/agent"
	"github.com/koraai/koraai/internal/ports"
)

// SearchActivities lists priced attractions (cost norms) and, when an
// activity supplier exists, bookable activities.
type SearchActivities struct{ d *Deps }

type activityArgs struct {
	City      string   `json:"city"`
	Interests []string `json:"interests,omitempty"`
}

// Name implements agent.Tool.
func (t *SearchActivities) Name() string { return "search_activities" }

// Spec implements agent.Tool.
func (t *SearchActivities) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name: "search_activities",
		Description: "List attractions with norm_ids, entry cost per person (IDR, estimate), tags, area and visit length, " +
			"optionally filtered by interest tags (festival, temple, food, autumn_foliage, family, theme_park, view, museum, onsen...). " +
			"Use the norm_ids in propose_day_plan. Do not use it for flights or hotels.",
		Parameters: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["city"],
 "properties":{"city":{"type":"string"},"interests":{"type":"array","items":{"type":"string"},"maxItems":6}}}`),
	}
}

// Label implements agent.Tool.
func (t *SearchActivities) Label(json.RawMessage, *agent.RunContext) string { return "Mencari atraksi dan aktivitas" }

// Execute implements agent.Tool.
func (t *SearchActivities) Execute(ctx context.Context, raw json.RawMessage, rc *agent.RunContext) (agent.ToolResult, error) {
	var a activityArgs
	if err := decode(raw, &a); err != nil {
		return agent.Fail("invalid_arguments", err.Error(), true), nil
	}
	req := rc.Request
	if req.Destination == nil || !strings.EqualFold(a.City, req.Destination.Code) {
		return agent.Fail("invalid_arguments", "city must be the trip request destination "+destCode(req), false), nil
	}
	rc.Lock()
	if rc.Count.Activities >= t.d.Limits.ActivitySearch {
		rc.Unlock()
		return agent.Fail("quota_exhausted", "Activity search quota used up.", false), nil
	}
	rc.Count.Activities++
	rc.Unlock()
	norms, err := t.d.Attractions.Attractions(ctx, req.Destination.Code, a.Interests, req.DepartDate)
	if err != nil {
		return agent.Fail("unavailable", "Attraction data unavailable.", true), nil
	}
	if len(norms) == 0 && len(a.Interests) > 0 {
		norms, _ = t.d.Attractions.Attractions(ctx, req.Destination.Code, nil, req.DepartDate)
	}
	type row struct {
		NormID       string   `json:"norm_id"`
		Label        string   `json:"label"`
		PerPersonIDR int64    `json:"entry_per_person_idr"`
		Tags         []string `json:"tags"`
		Area         string   `json:"area,omitempty"`
		Minutes      any      `json:"minutes,omitempty"`
		Months       any      `json:"season_months,omitempty"`
		Note         any      `json:"note,omitempty"`
	}
	var rows []row
	for _, n := range norms {
		idr, _, err := t.d.FX.Convert(ctx, n.Amount)
		if err != nil {
			continue
		}
		area, _ := n.Details["area"].(string)
		rows = append(rows, row{n.ID.String(), n.Label, idr, n.Tags(), area, n.Details["minutes"], n.Details["season_months"], n.Details["note"]})
	}
	if len(rows) > 20 {
		rows = rows[:20]
	}
	res := map[string]any{"ok": len(rows) > 0, "kind": "estimate", "attractions": rows}
	dig := digest(res)
	art := t.d.artifact(ctx, rc, t.Name(), json.RawMessage(raw), norms, dig)
	return agent.ToolResult{OK: len(rows) > 0, Digest: dig, ArtifactID: art, Summary: fmt.Sprintf("%d atraksi", len(rows))}, nil
}

// KGQuery retrieves travel-agent knowledge during the loop ("on loop").
type KGQuery struct{ d *Deps }

type kgArgs struct {
	Question string `json:"question"`
	Purpose  string `json:"purpose,omitempty"`
}

// Name implements agent.Tool.
func (t *KGQuery) Name() string { return "kg_query" }

// Spec implements agent.Tool.
func (t *KGQuery) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name: "kg_query",
		Description: "Ask the travel-agent knowledge base (reviewed persona pack) a question in Indonesian: areas, seasons, festivals, " +
			"durations, visa procedures, practical rules. Returns knowledge units with refs, and gaps when the base does not know. " +
			"Prices in knowledge units are context only; never use them as package prices.",
		Parameters: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["question"],
 "properties":{"question":{"type":"string","maxLength":400},"purpose":{"type":"string","maxLength":120}}}`),
	}
}

// Label implements agent.Tool.
func (t *KGQuery) Label(json.RawMessage, *agent.RunContext) string { return "Membuka pengetahuan travel agent" }

// Execute implements agent.Tool.
func (t *KGQuery) Execute(ctx context.Context, raw json.RawMessage, rc *agent.RunContext) (agent.ToolResult, error) {
	var a kgArgs
	if err := decode(raw, &a); err != nil || strings.TrimSpace(a.Question) == "" {
		return agent.Fail("invalid_arguments", "question is required", true), nil
	}
	if t.d.Persona == nil {
		return agent.Fail("kg_unavailable", "Knowledge base unavailable.", false), nil
	}
	purpose := a.Purpose
	if purpose == "" {
		purpose = "loop"
	}
	pack, err := t.d.Persona.Retrieve(ctx, []ports.KnowledgeQuery{{Purpose: purpose, Text: a.Question, Units: 5}}, 1500)
	if err != nil {
		return agent.Fail("kg_unavailable", "Knowledge base error.", true), nil
	}
	rc.Lock()
	rc.Knowledge = append(rc.Knowledge, pack.Units...)
	rc.Unlock()
	type unit struct {
		Ref   string `json:"ref"`
		Kind  string `json:"kind"`
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	var units []unit
	for _, u := range pack.Units {
		units = append(units, unit{u.Ref, u.Kind, u.Title, clip(u.Body, 500)})
	}
	res := map[string]any{"ok": true, "units": units, "gaps": pack.Gaps}
	if len(pack.Gaps) > 0 {
		res["hint"] = "The knowledge base has a gap here. If the fact is time-bound, use web_research."
	}
	dig := digest(res)
	art := t.d.artifact(ctx, rc, t.Name(), json.RawMessage(raw), pack, dig)
	return agent.ToolResult{OK: true, Digest: dig, ArtifactID: art, Summary: fmt.Sprintf("%d butir pengetahuan", len(units))}, nil
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// GetUserMemory recalls the user's durable facts (only when enabled).
type GetUserMemory struct{ d *Deps }

// Name implements agent.Tool.
func (t *GetUserMemory) Name() string { return "get_user_memory" }

// Spec implements agent.Tool.
func (t *GetUserMemory) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "get_user_memory",
		Description: "Recall durable preferences the traveller agreed to have remembered (home city, travel style, dietary needs). Returns nothing when memory is disabled.",
		Parameters:  json.RawMessage(`{"type":"object","additionalProperties":false,"required":["topic"],"properties":{"topic":{"type":"string","maxLength":200}}}`),
	}
}

// Label implements agent.Tool.
func (t *GetUserMemory) Label(json.RawMessage, *agent.RunContext) string { return "Mengingat preferensi Anda" }

// Execute implements agent.Tool.
func (t *GetUserMemory) Execute(ctx context.Context, raw json.RawMessage, rc *agent.RunContext) (agent.ToolResult, error) {
	var a struct {
		Topic string `json:"topic"`
	}
	if err := decode(raw, &a); err != nil {
		return agent.Fail("invalid_arguments", err.Error(), true), nil
	}
	if !t.d.MemoryOn || t.d.Memory == nil {
		return agent.ToolResult{OK: true, Digest: `{"ok":true,"memory":"","note":"memory is disabled for this user"}`, Summary: "Memori tidak aktif"}, nil
	}
	b, err := t.d.Memory.Recall(ctx, rc.UserID, a.Topic, 600)
	if err != nil {
		return agent.Fail("memory_unavailable", "Memory service unavailable; continue without it.", false), nil
	}
	return agent.ToolResult{OK: true, Digest: digest(map[string]any{"ok": true, "memory": b.Text}), Summary: "Preferensi dimuat"}, nil
}
