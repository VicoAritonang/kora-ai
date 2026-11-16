package agent

import "time"

// StopReason explains why the loop ended.
type StopReason string

// Stop reasons (MODULE_SPEC §7).
const (
	StopDone      StopReason = "done"
	StopSteps     StopReason = "step_limit"
	StopCost      StopReason = "cost_limit"
	StopTime      StopReason = "time_limit"
	StopModelFail StopReason = "model_failed"
)

// Limits bound one loop (AI_SYSTEM_DESIGN §5).
type Limits struct {
	MaxSteps       int
	FlightSearches int
	StaySearches   int
	ActivitySearch int
	WebResearch    int
	CostMicros     int64
	Deadline       time.Time
	ToolTimeout    time.Duration
	WebTimeout     time.Duration
	MaxOutput      int
}
