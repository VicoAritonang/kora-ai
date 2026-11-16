package agent

import "testing"

func TestEffortPolicy_Auto(t *testing.T) {
	p := EffortPolicy{Max: EffortMedium, Fixed: map[string]Effort{}}
	if d := p.For("extractor", Signals{}); d.Effort != EffortMinimal {
		t.Fatalf("extractor %+v", d)
	}
	if d := p.For("planner", Signals{}); d.Effort != EffortLow {
		t.Fatalf("simple planner %+v", d)
	}
	if d := p.For("planner", Signals{SpecialRequests: 1}); d.Effort != EffortMedium {
		t.Fatalf("constrained planner %+v", d)
	}
	if d := p.For("narrator", Signals{OverBudget: true}); d.Effort != EffortMedium {
		t.Fatalf("over-budget narrator %+v", d)
	}
}

func TestEffortPolicy_CapAndEscalate(t *testing.T) {
	p := EffortPolicy{Max: EffortLow, Fixed: map[string]Effort{}}
	d := p.For("planner", Signals{SpecialRequests: 3})
	if d.Effort != EffortLow {
		t.Fatalf("cap ignored: %+v", d)
	}
	if e := p.EscalateAfter(d, "x"); e.Effort != EffortLow {
		t.Fatalf("escalated past cap: %+v", e)
	}
	p.Max = EffortHigh
	if e := p.EscalateAfter(Decision{Role: "planner", Effort: EffortLow, Mode: "auto"}, "x"); e.Effort != EffortMedium || e.Mode != "escalated" {
		t.Fatalf("%+v", e)
	}
}

func TestEffortPolicy_FixedWins(t *testing.T) {
	p := EffortPolicy{Max: EffortMedium, Fixed: map[string]Effort{"planner": EffortHigh}}
	d := p.For("planner", Signals{})
	if d.Effort != EffortHigh || d.Mode != "fixed" {
		t.Fatalf("%+v", d)
	}
	if e := p.EscalateAfter(d, "x"); e.Effort != EffortHigh {
		t.Fatalf("fixed level changed: %+v", e)
	}
}
