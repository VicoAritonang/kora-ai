// Package tracerlab forwards run events to AI Lab. The event contract is
// still a proposal (AI_SYSTEM_DESIGN §11); mapping to AI Lab's format belongs
// here. Delivery is best effort: events are already in Postgres and can be
// re-read through /internal/v1 (outbox semantics, ENGINEERING_STANDARDS §2).
package tracerlab

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
)

// Tracer posts events in small batches to AI Lab.
type Tracer struct {
	URL   string
	Token string
	HTTP  *platform.HTTPClient
	Log   *slog.Logger
	ch    chan ports.RunEvent
}

// New starts the background sender.
func New(url, token string, log *slog.Logger) *Tracer {
	t := &Tracer{URL: url, Token: token, HTTP: platform.NewHTTPClient("ailab", 5*time.Second), Log: log, ch: make(chan ports.RunEvent, 2048)}
	platform.Go(log, "tracerlab", t.loop)
	return t
}

// Emit enqueues the event; a full queue drops it (it stays in Postgres).
func (t *Tracer) Emit(_ context.Context, ev ports.RunEvent) error {
	select {
	case t.ch <- ev:
	default:
		t.Log.Warn("ai lab queue full; event kept in postgres only", "type", ev.Type)
	}
	return nil
}

func (t *Tracer) loop() {
	batch := make([]ports.RunEvent, 0, 64)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	flush := func() {
		if len(batch) == 0 {
			return
		}
		b, _ := json.Marshal(map[string]any{"events": batch})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _, err := t.HTTP.Do(ctx, "POST", t.URL, b, map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + t.Token})
		cancel()
		if err != nil {
			t.Log.Warn("ai lab delivery failed", "events", len(batch), "err", err)
		}
		batch = batch[:0]
	}
	for {
		select {
		case ev := <-t.ch:
			batch = append(batch, ev)
			if len(batch) >= 64 {
				flush()
			}
		case <-tick.C:
			flush()
		}
	}
}

// Multi fans events out to several tracers. The first is authoritative: its
// error is returned; the others are best effort.
type Multi []ports.Tracer

// Emit sends to every tracer.
func (m Multi) Emit(ctx context.Context, ev ports.RunEvent) error {
	var first error
	for i, t := range m {
		err := t.Emit(ctx, ev)
		if i == 0 {
			first = err
		}
	}
	return first
}
