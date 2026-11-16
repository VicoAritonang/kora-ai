// Package tracerpg writes run events to Postgres (run_events, the source of
// truth for SSE replay and AI Lab) and publishes the sequence number on the
// Redis channel run:{id} for cross-instance SSE fan-out.
package tracerpg

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"

	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/store/postgres"
	"github.com/koraai/koraai/internal/store/redis"
)

// Tracer is the Postgres + Redis sink.
//
// It imports the store, which rule D5 reserves for application modules; the
// tracer is the one adapter whose whole job is persistence, so it is wired
// as infrastructure in app/. depguard allows this one exception.
type Tracer struct {
	DB    *postgres.DB
	Redis *redis.Client
	Log   *slog.Logger
}

// Channel returns the Redis channel of a run.
func Channel(runID string) string { return "run:" + runID }

// Emit stores the event, then notifies subscribers. A Redis failure is
// tolerated: SSE handlers fall back to polling Postgres.
func (t *Tracer) Emit(ctx context.Context, ev ports.RunEvent) error {
	payload, err := json.Marshal(ev.Payload)
	if err != nil {
		payload = []byte(`{"error":"unserializable payload"}`)
	}
	vis := "internal"
	if ev.Public {
		vis = "public"
	}
	var step, model *string
	if ev.Step != "" {
		step = &ev.Step
	}
	if ev.Model != "" {
		model = &ev.Model
	}
	err = t.DB.Q().InsertEvent(context.WithoutCancel(ctx), postgres.Event{
		RunID: ev.RunID, Seq: ev.Seq, Type: ev.Type, Visibility: vis, Step: step, Payload: payload, Model: model,
		InputTokens: ev.InputTokens, CachedTokens: ev.CachedTokens, OutputTokens: ev.OutputTokens, CostMicros: ev.CostMicros,
		LatencyMs: ev.LatencyMs, CreatedAt: ev.At,
	})
	if err != nil {
		return err
	}
	if ev.Public && t.Redis != nil {
		if perr := t.Redis.Publish(context.WithoutCancel(ctx), Channel(ev.RunID.String()), strconv.Itoa(ev.Seq)); perr != nil && t.Log != nil {
			t.Log.Warn("publish run event failed", "err", perr)
		}
	}
	return nil
}
