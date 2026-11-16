package tools

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/koraai/koraai/internal/agent"
	"github.com/koraai/koraai/internal/ports"
)

// PresentToUser is the planner's actuator for the web canvas: it chooses the
// view the traveller should look at and the tier Kora recommends. It changes
// what is shown, never what is priced.
type PresentToUser struct{ d *Deps }

var views = map[string]bool{"brief": true, "flights": true, "stays": true, "itinerary": true, "packages": true}

// Name implements agent.Tool.
func (t *PresentToUser) Name() string { return "present_to_user" }

// Spec implements agent.Tool.
func (t *PresentToUser) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name: "present_to_user",
		Description: "Direct the traveller's screen: choose the canvas view to show and the package tier you recommend, with a " +
			"short reason in Indonesian (no numbers). Call it in the same turn as build_packages. It changes the display only.",
		Parameters: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["view"],
 "properties":{"view":{"type":"string","enum":["brief","flights","stays","itinerary","packages"]},
  "recommend_tier":{"type":"string","enum":["hemat","seimbang","nyaman"]},
  "reason":{"type":"string","maxLength":160}}}`),
	}
}

// Label implements agent.Tool.
func (t *PresentToUser) Label(json.RawMessage, *agent.RunContext) string { return "Menyiapkan tampilan untuk Anda" }

// Execute implements agent.Tool.
func (t *PresentToUser) Execute(_ context.Context, raw json.RawMessage, rc *agent.RunContext) (agent.ToolResult, error) {
	var a struct {
		View   string `json:"view"`
		Tier   string `json:"recommend_tier"`
		Reason string `json:"reason"`
	}
	if err := decode(raw, &a); err != nil || !views[a.View] {
		return agent.Fail("invalid_arguments", "view must be one of brief, flights, stays, itinerary, packages", true), nil
	}
	reason := sanitize(a.Reason, 160)
	// Numbers belong to the package builder, not to model captions (P1).
	if strings.IndexFunc(reason, unicode.IsDigit) >= 0 {
		reason = ""
	}
	rc.Lock()
	rc.Presentation = &agent.Presentation{View: a.View, Tier: a.Tier, Reason: reason}
	rc.Unlock()
	return agent.ToolResult{OK: true, Digest: `{"ok":true}`, Summary: "Tampilan diperbarui"}, nil
}
