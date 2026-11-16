package costnorm

import "testing"

// The SQL seed (scripts/build-schema.mjs) and ImportFile must produce the
// same id for a norm, or re-imports would duplicate rows.
func TestStableID_MatchesSeedGenerator(t *testing.T) {
	if got := StableID("OSA:meal.hemat").String(); got != "b7af1bc4-62ec-5b43-a763-10108d2bc790" {
		t.Fatalf("got %s", got)
	}
}
