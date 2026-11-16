// Package prompts holds one versioned prompt per role (AI_SYSTEM_DESIGN §8).
// Prompts are code: change them through review and the golden set; the
// version of each is recorded in runs.config_snapshot.
package prompts

import (
	_ "embed"
	"strings"
)

// Versions of each prompt file. Bump when the text changes.
const (
	PlannerVersion    = "planner-v5"
	NarratorVersion   = "narrator-v1"
	InfoVersion       = "info-v1"
	ChitchatVersion   = "chitchat-v1"
	ExtractorVersion  = "extractor-v3"
	RouterVersion     = "router-fallback-v1"
	ResearcherVersion = "researcher-v1"
	SummarizerVersion = "summarizer-v1"
)

var (
	//go:embed planner.md
	planner string
	//go:embed narrator.md
	narrator string
	//go:embed info.md
	info string
	//go:embed chitchat.md
	chitchat string
	//go:embed extractor.md
	extractor string
	//go:embed router.md
	router string
	//go:embed researcher.md
	researcher string
	//go:embed summarizer.md
	summarizer string
)

// Planner returns the planner prompt with the persona core in the stable prefix.
func Planner(personaCore string) string { return withPersona(planner, personaCore) }

// Narrator returns the narrator prompt.
func Narrator(personaCore string) string { return withPersona(narrator, personaCore) }

// Info returns the travel-information prompt.
func Info(personaCore string) string { return withPersona(info, personaCore) }

// Chitchat returns the small-talk prompt.
func Chitchat() string { return chitchat }

// Extractor returns the slot-extraction prompt; today is "YYYY-MM-DD".
func Extractor(today string) string { return strings.ReplaceAll(extractor, "{{TODAY}}", today) }

// Router returns the fallback router prompt.
func Router() string { return router }

// Researcher returns the isolated web-research prompt.
func Researcher(today string) string { return strings.ReplaceAll(researcher, "{{TODAY}}", today) }

// Summarizer returns the conversation summarizer prompt.
func Summarizer() string { return summarizer }

func withPersona(p, core string) string {
	if core == "" {
		return p
	}
	return "<persona>\n" + core + "\n</persona>\n\n" + p
}

// Versions returns every prompt version for the config snapshot.
func Versions() map[string]string {
	return map[string]string{
		"planner": PlannerVersion, "narrator": NarratorVersion, "info": InfoVersion, "chitchat": ChitchatVersion,
		"extractor": ExtractorVersion, "router_fallback": RouterVersion, "researcher": ResearcherVersion, "summarizer": SummarizerVersion,
	}
}
