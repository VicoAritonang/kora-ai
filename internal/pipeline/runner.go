// Package pipeline runs one run from a user message to the answer. It is the
// only place that knows the order of the steps (AI_SYSTEM_DESIGN §3):
//
//	guard-in → route → extract → clarify → retrieve → agent loop → build
//	→ verify → narrate (+ guard-out per paragraph)
//
// Every step emits run_events; public events feed the SSE stream.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/agent"
	"github.com/koraai/koraai/internal/config"
	"github.com/koraai/koraai/internal/costnorm"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/priceindex"
	"github.com/koraai/koraai/internal/store/postgres"
	"github.com/koraai/koraai/internal/store/redis"
	"github.com/koraai/koraai/internal/tools"
	"github.com/koraai/koraai/internal/verifier"
)

// JobQueue enqueues background work (River in app/).
type JobQueue interface {
	Enqueue(ctx context.Context, kind string, args map[string]string) error
}

// Deps are the pipeline's collaborators, all behind ports or app modules.
type Deps struct {
	Cfg      *config.Config
	DB       *postgres.DB
	Redis    *redis.Client
	Model    ports.ModelProvider
	Decider  ports.Decider
	Persona  ports.PersonaKnowledge // nil when the pack failed to load
	Memory   ports.UserMemory
	Guard    ports.Guard
	Tracer   ports.Tracer
	Registry *agent.Registry
	ToolDeps *tools.Deps
	Norms    *costnorm.Service
	FX       *costnorm.FX
	Index    *priceindex.Index
	Verifier *verifier.Verifier
	Jobs     JobQueue
	Clock    platform.Clock
	Log      *slog.Logger
}

// RunInput identifies the run to execute (MODULE_SPEC §4).
type RunInput struct {
	RunID, ConversationID, UserID, MessageID uuid.UUID
}

// ErrShuttingDown is returned by Start during shutdown.
var ErrShuttingDown = errors.New("pipeline: shutting down")

// ErrCapacity is returned when the instance runs its maximum concurrent runs.
var ErrCapacity = errors.New("pipeline: capacity exhausted")

// CancelChannel is the Redis channel carrying cancellation requests.
const CancelChannel = "run-cancel"

// Runner executes runs as goroutines on this instance (SYSTEM_ARCHITECTURE §7).
type Runner struct {
	d       Deps
	policy  *agent.EffortPolicy
	sem     chan struct{}
	mu      sync.Mutex
	active  map[uuid.UUID]context.CancelFunc
	wg      sync.WaitGroup
	closing atomic.Bool
}

// NewRunner builds the runner.
func NewRunner(d Deps) *Runner {
	n := d.Cfg.Limits.MaxConcurrent
	if n <= 0 {
		n = 20
	}
	return &Runner{d: d, policy: effortPolicy(d.Cfg), sem: make(chan struct{}, n), active: map[uuid.UUID]context.CancelFunc{}}
}

// Capacity reports whether a new run could start now.
func (r *Runner) Capacity() bool { return len(r.sem) < cap(r.sem) && !r.closing.Load() }

// Start launches the run in the background.
func (r *Runner) Start(_ context.Context, in RunInput) error {
	if r.closing.Load() {
		return ErrShuttingDown
	}
	select {
	case r.sem <- struct{}{}:
	default:
		return ErrCapacity
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.d.Cfg.Limits.RunTimeout)
	ctx = platform.WithRun(ctx, in.RunID, in.ConversationID)
	ctx = platform.WithUser(ctx, in.UserID)
	r.mu.Lock()
	r.active[in.RunID] = cancel
	r.mu.Unlock()
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() { <-r.sem }()
		defer func() {
			r.mu.Lock()
			delete(r.active, in.RunID)
			r.mu.Unlock()
			cancel()
		}()
		r.execute(ctx, in)
	}()
	return nil
}

// Cancel cancels a run here, or asks the owning instance through Redis.
func (r *Runner) Cancel(ctx context.Context, runID uuid.UUID) error {
	if r.cancelLocal(runID) {
		return nil
	}
	if r.d.Redis != nil {
		return r.d.Redis.Publish(ctx, CancelChannel, runID.String())
	}
	return nil
}

func (r *Runner) cancelLocal(runID uuid.UUID) bool {
	r.mu.Lock()
	c, ok := r.active[runID]
	r.mu.Unlock()
	if ok {
		c()
	}
	return ok
}

// ListenCancels follows the Redis cancel channel until ctx ends.
func (r *Runner) ListenCancels(ctx context.Context) {
	if r.d.Redis == nil {
		return
	}
	sub := r.d.Redis.Subscribe(ctx, CancelChannel)
	defer sub.Close()
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-ch:
			if !ok {
				return
			}
			if id, err := uuid.Parse(m.Payload); err == nil {
				r.cancelLocal(id)
			}
		}
	}
}

// Shutdown stops accepting runs, waits for active ones until ctx ends and
// marks the rest failed with error_code=interrupted (NFR-14).
func (r *Runner) Shutdown(ctx context.Context) {
	r.closing.Store(true)
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
		return
	case <-ctx.Done():
	}
	r.mu.Lock()
	ids := make([]uuid.UUID, 0, len(r.active))
	for id, c := range r.active {
		ids = append(ids, id)
		c()
	}
	r.mu.Unlock()
	code := "interrupted"
	for _, id := range ids {
		_ = r.d.DB.Q().FinishRun(context.Background(), id, "failed", &code, postgres.RunTotals{})
	}
}

// RunSync executes a run in the caller's goroutine (eval runner, tests).
func (r *Runner) RunSync(ctx context.Context, in RunInput) {
	ctx, cancel := context.WithTimeout(ctx, r.d.Cfg.Limits.RunTimeout)
	defer cancel()
	r.execute(ctx, in)
}

// runError ends a run with a code shown to the user.
type runError struct {
	Code      string
	Status    string // failed | blocked | cancelled
	Retryable bool
	Err       error
}

func (e *runError) Error() string { return fmt.Sprintf("%s: %v", e.Code, e.Err) }

func fail(code string, retryable bool, err error) error {
	return &runError{Code: code, Status: "failed", Retryable: retryable, Err: err}
}

func (r *Runner) execute(ctx context.Context, in RunInput) {
	st := &runState{in: in, d: &r.d, started: time.Now()}
	st.onEvent = func(ctx context.Context, typ string, payload any) { r.wsOnEvent(ctx, st, typ, payload) }
	defer func() {
		if p := recover(); p != nil {
			r.d.Log.Error("run panic", "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
			r.finishFailed(context.WithoutCancel(ctx), st, &runError{Code: "internal_error", Status: "failed", Retryable: true, Err: fmt.Errorf("panic: %v", p)})
		}
	}()
	err := r.run(ctx, st)
	if err == nil {
		return
	}
	var re *runError
	switch {
	case errors.As(err, &re):
	case errors.Is(ctx.Err(), context.Canceled):
		re = &runError{Code: "cancelled", Status: "cancelled", Err: err}
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		re = &runError{Code: "timeout", Status: "failed", Retryable: true, Err: err}
	default:
		re = &runError{Code: "internal_error", Status: "failed", Retryable: true, Err: err}
	}
	if errors.Is(ctx.Err(), context.Canceled) && re.Code != "cancelled" {
		re = &runError{Code: "cancelled", Status: "cancelled", Err: err}
	}
	r.finishFailed(context.WithoutCancel(ctx), st, re)
}

func (r *Runner) finishFailed(ctx context.Context, st *runState, re *runError) {
	if re.Code == "internal_error" || re.Code == "timeout" {
		r.d.Log.Error("run failed", "code", re.Code, "err", re.Err)
	} else {
		r.d.Log.Warn("run ended", "code", re.Code, "err", re.Err)
	}
	msg := userMessage(re.Code, st.lang())
	if re.Status == "blocked" {
		// A blocked run still answers with a safe message (MODULE_SPEC §4).
		if _, err := r.d.DB.Q().InsertMessage(ctx, platform.NewID(), st.in.ConversationID, "assistant", msg, map[string]any{"blocked": true}, &st.in.RunID); err != nil {
			r.d.Log.Error("save blocked message failed", "err", err)
		}
	}
	st.emit(ctx, "run.finished", false, "finish", map[string]any{"status": re.Status, "code": re.Code, "error": re.Err.Error(), "flags": st.flags,
		"usage": st.usage, "duration_ms": time.Since(st.started).Milliseconds()}, nil)
	st.emit(ctx, "run.failed", true, "finish", map[string]any{"run_id": st.in.RunID, "code": re.Code, "message": msg, "retryable": re.Retryable}, nil)
	code := re.Code
	if err := r.d.DB.Q().FinishRun(ctx, st.in.RunID, re.Status, &code, st.totals()); err != nil {
		r.d.Log.Error("finish run failed", "err", err)
	}
	if st.guardSession != nil {
		_ = r.d.DB.Q().SaveGuardSession(ctx, st.in.ConversationID, st.guardSession)
	}
}
