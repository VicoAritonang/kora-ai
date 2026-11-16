package config

import (
	"strings"
	"testing"
)

func base(t *testing.T) {
	t.Helper()
	t.Setenv("DB_URL", "postgres://kora:kora@localhost:5432/kora")
	t.Setenv("OPENAI_API_KEY", "test")
	t.Setenv("STAGE", "")
	t.Setenv("LAB_WS_URL", "")
}

func TestLabEnabledOnlyWithDevStageAndURL(t *testing.T) {
	base(t)
	t.Setenv("APP_ENV", "local")
	t.Setenv("STAGE", "dev")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Tracer.ARL.Enabled() {
		t.Error("STAGE=dev without LAB_WS_URL must stay off")
	}
	t.Setenv("LAB_WS_URL", "ws://localhost:8000/api/agent-lab/ws")
	if c, err = Load(); err != nil || !c.Tracer.ARL.Enabled() || c.Tracer.ARL.Service != "kora/api" {
		t.Fatalf("expected lab on in local: %v %+v", err, c.Tracer.ARL)
	}
}

func TestLabRejectedOutsideLocal(t *testing.T) {
	base(t)
	t.Setenv("APP_ENV", "staging")
	t.Setenv("APP_SESSION_KEY", strings.Repeat("s", 32))
	t.Setenv("GUARDRAILS_SECRET", strings.Repeat("g", 32))
	t.Setenv("STAGE", "dev")
	t.Setenv("LAB_WS_URL", "ws://lab:8000/api/agent-lab/ws")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "Agent Reliability Lab") {
		t.Fatalf("lab streaming must be refused outside local, got %v", err)
	}
	t.Setenv("STAGE", "")
	if _, err := Load(); err != nil {
		t.Fatalf("staging without STAGE=dev should load: %v", err)
	}
}
