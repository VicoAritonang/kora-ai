// Package jobs defines the River jobs run by the worker
// (SYSTEM_ARCHITECTURE §3): price index refresh, summaries, user memory,
// reminders, retention, hanging-run sweeper, event partitions.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/koraai/koraai/internal/agent/prompts"
	"github.com/koraai/koraai/internal/config"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/priceindex"
	"github.com/koraai/koraai/internal/reminder"
	"github.com/koraai/koraai/internal/store/postgres"
)

// --- job args -------------------------------------------------------------

// SummarizeArgs summarizes old turns of a conversation.
type SummarizeArgs struct {
	ConversationID string `json:"conversation_id" river:"unique"`
}

// Kind implements river.JobArgs.
func (SummarizeArgs) Kind() string { return "summarize" }

// MemoryWriteArgs sends a run's turn to user memory.
type MemoryWriteArgs struct {
	ConversationID string `json:"conversation_id"`
	RunID          string `json:"run_id" river:"unique"`
}

// Kind implements river.JobArgs.
func (MemoryWriteArgs) Kind() string { return "memory_write" }

// RefreshIndexArgs refreshes the Destination Cost Index.
type RefreshIndexArgs struct{}

// Kind implements river.JobArgs.
func (RefreshIndexArgs) Kind() string { return "refresh_price_index" }

// SendRemindersArgs sends due reminders.
type SendRemindersArgs struct{}

// Kind implements river.JobArgs.
func (SendRemindersArgs) Kind() string { return "send_reminders" }

// RetentionArgs applies data retention.
type RetentionArgs struct{}

// Kind implements river.JobArgs.
func (RetentionArgs) Kind() string { return "retention" }

// SweepRunsArgs fails hanging runs.
type SweepRunsArgs struct{}

// Kind implements river.JobArgs.
func (SweepRunsArgs) Kind() string { return "sweep_runs" }

// PartitionArgs creates upcoming run_events partitions.
type PartitionArgs struct{}

// Kind implements river.JobArgs.
func (PartitionArgs) Kind() string { return "partition_maintenance" }

// --- dependencies ---------------------------------------------------------

// Deps are what the workers need.
type Deps struct {
	Cfg       *config.Config
	DB        *postgres.DB
	Model     ports.ModelProvider
	Guard     ports.Guard
	Memory    ports.UserMemory
	Index     *priceindex.Index
	Reminders *reminder.Service
	Clock     platform.Clock
	Log       *slog.Logger
}

// Workers registers every worker.
func Workers(d *Deps) *river.Workers {
	w := river.NewWorkers()
	river.AddWorker(w, &summarizeWorker{d: d})
	river.AddWorker(w, &memoryWorker{d: d})
	river.AddWorker(w, &indexWorker{d: d})
	river.AddWorker(w, &remindersWorker{d: d})
	river.AddWorker(w, &retentionWorker{d: d})
	river.AddWorker(w, &sweepWorker{d: d})
	river.AddWorker(w, &partitionWorker{d: d})
	return w
}

// Periodic returns the schedule.
func Periodic() []*river.PeriodicJob {
	every := func(d time.Duration, args river.JobArgs, onStart bool) *river.PeriodicJob {
		return river.NewPeriodicJob(river.PeriodicInterval(d), func() (river.JobArgs, *river.InsertOpts) { return args, nil }, &river.PeriodicJobOpts{RunOnStart: onStart})
	}
	return []*river.PeriodicJob{
		every(time.Minute, SweepRunsArgs{}, true),
		every(5*time.Minute, SendRemindersArgs{}, false),
		every(24*time.Hour, RetentionArgs{}, true),
		every(24*time.Hour, PartitionArgs{}, true),
		every(7*24*time.Hour, RefreshIndexArgs{}, true),
	}
}

// --- workers --------------------------------------------------------------

type summarizeWorker struct {
	river.WorkerDefaults[SummarizeArgs]
	d *Deps
}

// window turns kept verbatim (AI_SYSTEM_DESIGN §7 layer 5) and the backlog
// beyond it that triggers a summary.
const keepTurns, summaryThreshold = 12, 6

func (w *summarizeWorker) Work(ctx context.Context, job *river.Job[SummarizeArgs]) error {
	id, err := uuid.Parse(job.Args.ConversationID)
	if err != nil {
		return nil
	}
	q := w.d.DB.Q()
	conv, err := q.GetConversationByID(ctx, id)
	if err != nil {
		return nil // deleted
	}
	msgs, err := q.ListMessages(ctx, id, conv.SummaryUptoSeq, 500)
	if err != nil {
		return err
	}
	if len(msgs) <= keepTurns+summaryThreshold {
		return nil
	}
	old := msgs[:len(msgs)-keepTurns]
	var b strings.Builder
	if conv.Summary != nil {
		b.WriteString("<previous_summary>\n" + *conv.Summary + "\n</previous_summary>\n")
	}
	for _, m := range old {
		fmt.Fprintf(&b, "%s: %s\n", m.Role, truncate(m.Content, 1200))
	}
	res, err := w.d.Model.Generate(ctx, ports.GenerateRequest{
		Role: "summarizer", Model: w.d.Cfg.Models.Summarizer, FallbackModel: w.d.Cfg.Models.ExtractorFallback,
		Instructions: prompts.Summarizer(), Input: []ports.InputItem{ports.Message(ports.RoleUser, b.String())},
		MaxOutputTokens: 800, ReasoningEffort: "minimal",
	})
	if err != nil {
		return err
	}
	return q.SaveSummary(ctx, id, strings.TrimSpace(res.Text), old[len(old)-1].Seq)
}

type memoryWorker struct {
	river.WorkerDefaults[MemoryWriteArgs]
	d *Deps
}

// Work sends the run's turn to memory after masking PII with the same
// inbound guard (AI_SYSTEM_DESIGN §7: only redacted text is remembered).
func (w *memoryWorker) Work(ctx context.Context, job *river.Job[MemoryWriteArgs]) error {
	if !w.d.Cfg.Memory.Enabled || w.d.Memory == nil {
		return nil
	}
	conv, err := uuid.Parse(job.Args.ConversationID)
	if err != nil {
		return nil
	}
	c, err := w.d.DB.Q().GetConversationByID(ctx, conv)
	if err != nil {
		return nil
	}
	u, err := w.d.DB.Q().GetUser(ctx, c.UserID)
	if err != nil || u.IsGuest || !u.MemoryEnabled {
		return nil
	}
	msgs, err := w.d.DB.Q().LastMessages(ctx, conv, 2)
	if err != nil {
		return err
	}
	var out []ports.MemoryMessage
	for _, m := range msgs {
		res, err := w.d.Guard.Inbound(ctx, ports.GuardInbound{Messages: []ports.GuardMessage{{Role: "user", Content: m.Content}}})
		if err != nil || len(res.Messages) == 0 {
			continue // never store what the guard would not pass
		}
		out = append(out, ports.MemoryMessage{Role: m.Role, Content: res.Messages[0].Content, At: m.CreatedAt})
	}
	if len(out) == 0 {
		return nil
	}
	return w.d.Memory.Remember(ctx, u.ID, out)
}

type indexWorker struct {
	river.WorkerDefaults[RefreshIndexArgs]
	d *Deps
}

func (w *indexWorker) Timeout(*river.Job[RefreshIndexArgs]) time.Duration { return 30 * time.Minute }

func (w *indexWorker) Work(ctx context.Context, _ *river.Job[RefreshIndexArgs]) error {
	n, err := w.d.Index.Refresh(ctx, w.d.Cfg.App.Origins, 12)
	w.d.Log.Info("price index refreshed", "rows", n, "err", err)
	return err
}

type remindersWorker struct {
	river.WorkerDefaults[SendRemindersArgs]
	d *Deps
}

func (w *remindersWorker) Work(ctx context.Context, _ *river.Job[SendRemindersArgs]) error {
	n, err := w.d.Reminders.SendDue(ctx)
	if n > 0 {
		w.d.Log.Info("reminders sent", "count", n)
	}
	return err
}

type retentionWorker struct {
	river.WorkerDefaults[RetentionArgs]
	d *Deps
}

func (w *retentionWorker) Work(ctx context.Context, _ *river.Job[RetentionArgs]) error {
	q := w.d.DB.Q()
	now := w.d.Clock.Now()
	// Forget memory before rows are deleted (DATABASE_SCHEMA §7).
	if ids, err := q.DeletedUsersDue(ctx, now); err == nil && w.d.Memory != nil {
		for _, id := range ids {
			if err := w.d.Memory.Forget(ctx, id); err != nil {
				w.d.Log.Warn("memory forget failed", "err", err)
			}
		}
	}
	res, err := q.RunRetention(ctx, now)
	if n, e := q.MarkExpiredPackagesStale(ctx, now); e == nil {
		res["packages_stale"] = n
	}
	w.d.Log.Info("retention pass", "result", res, "err", err)
	return err
}

type sweepWorker struct {
	river.WorkerDefaults[SweepRunsArgs]
	d *Deps
}

func (w *sweepWorker) Work(ctx context.Context, _ *river.Job[SweepRunsArgs]) error {
	ids, err := w.d.DB.Q().FailStaleRuns(ctx, w.d.Cfg.Limits.RunTimeout+time.Minute, "timeout")
	if len(ids) > 0 {
		w.d.Log.Warn("swept hanging runs", "count", len(ids))
	}
	return err
}

type partitionWorker struct {
	river.WorkerDefaults[PartitionArgs]
	d *Deps
}

func (w *partitionWorker) Work(ctx context.Context, _ *river.Job[PartitionArgs]) error {
	now := w.d.Clock.Now()
	for i := 0; i < 4; i++ {
		if err := w.d.DB.Q().EnsureEventPartition(ctx, now.AddDate(0, i, 0)); err != nil {
			return err
		}
	}
	return nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// --- queue (insert side, used by the api) ---------------------------------

// Queue inserts jobs through an insert-only River client.
type Queue struct {
	Client *river.Client[pgx.Tx]
	Log    *slog.Logger
}

// Enqueue implements pipeline.JobQueue.
func (q *Queue) Enqueue(ctx context.Context, kind string, args map[string]string) error {
	if q == nil || q.Client == nil {
		return nil
	}
	var a river.JobArgs
	opts := &river.InsertOpts{}
	switch kind {
	case "summarize":
		a = SummarizeArgs{ConversationID: args["conversation_id"]}
		opts.UniqueOpts = river.UniqueOpts{ByArgs: true, ByPeriod: 5 * time.Minute}
	case "memory_write":
		a = MemoryWriteArgs{ConversationID: args["conversation_id"], RunID: args["run_id"]}
		opts.UniqueOpts = river.UniqueOpts{ByArgs: true}
	default:
		return fmt.Errorf("unknown job kind %q", kind)
	}
	_, err := q.Client.Insert(ctx, a, opts)
	if err != nil && q.Log != nil {
		q.Log.Warn("enqueue job failed", "kind", kind, "err", err)
	}
	return err
}
