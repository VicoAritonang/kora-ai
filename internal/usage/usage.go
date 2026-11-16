// Package usage enforces quotas and rate limits: runs per day (reset at
// midnight Asia/Jakarta), requests per minute, and generic keyed limits. When
// Redis is down it falls back to conservative in-memory limits per instance
// (MODULE_SPEC §14).
package usage

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/config"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/store/redis"
)

// Limiter is the quota service.
type Limiter struct {
	R     *redis.Client
	Q     config.Quota
	Clock platform.Clock
	mu    sync.Mutex
	mem   map[string]memCount
}

type memCount struct {
	n     int64
	until time.Time
}

// New returns a limiter.
func New(r *redis.Client, q config.Quota, clock platform.Clock) *Limiter {
	return &Limiter{R: r, Q: q, Clock: clock, mem: map[string]memCount{}}
}

// DailyRuns returns the run quota for the user type.
func (l *Limiter) DailyRuns(guest bool) int {
	if guest {
		return l.Q.GuestRunsPerDay
	}
	return l.Q.UserRunsPerDay
}

func (l *Limiter) dayKey(userID uuid.UUID) (string, time.Duration) {
	now := l.Clock.Now().In(platform.Jakarta)
	end := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, platform.Jakarta)
	return fmt.Sprintf("quota:runs:%s:%s", userID, now.Format("20060102")), end.Sub(now) + time.Minute
}

// RunsUsed returns runs used today.
func (l *Limiter) RunsUsed(ctx context.Context, userID uuid.UUID) int64 {
	key, _ := l.dayKey(userID)
	if l.R != nil {
		if n, err := l.R.GetInt(ctx, key); err == nil {
			return n
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.mem[key].n
}

// RunsRemaining returns today's remaining runs.
func (l *Limiter) RunsRemaining(ctx context.Context, userID uuid.UUID, guest bool) int {
	r := int64(l.DailyRuns(guest)) - l.RunsUsed(ctx, userID)
	if r < 0 {
		return 0
	}
	return int(r)
}

// ConsumeRun counts one run; false when the daily quota is exhausted.
func (l *Limiter) ConsumeRun(ctx context.Context, userID uuid.UUID, guest bool) (bool, int) {
	key, ttl := l.dayKey(userID)
	limit := int64(l.DailyRuns(guest))
	n, err := l.incr(ctx, key, ttl, true)
	if err != nil {
		limit /= 2 // conservative while Redis is unavailable
	}
	if n > limit {
		return false, 0
	}
	return true, int(limit - n)
}

// Allow is a fixed-window limiter: at most limit events per window for key.
func (l *Limiter) Allow(ctx context.Context, key string, limit int, window time.Duration) bool {
	bucket := l.Clock.Now().Truncate(window).Unix()
	k := fmt.Sprintf("rl:%s:%d", key, bucket)
	n, err := l.incr(ctx, k, window+time.Second, false)
	if err != nil {
		limit = max(1, limit/2)
	}
	return n <= int64(limit)
}

func (l *Limiter) incr(ctx context.Context, key string, ttl time.Duration, _ bool) (int64, error) {
	if l.R != nil {
		n, err := l.R.Incr(ctx, key, ttl)
		if err == nil {
			return n, nil
		}
		l.memIncr(key, ttl)
		return l.memGet(key), err
	}
	return l.memIncr(key, ttl), nil
}

func (l *Limiter) memIncr(key string, ttl time.Duration) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Clock.Now()
	c := l.mem[key]
	if now.After(c.until) {
		c = memCount{until: now.Add(ttl)}
	}
	c.n++
	l.mem[key] = c
	if len(l.mem) > 50_000 {
		for k, v := range l.mem {
			if now.After(v.until) {
				delete(l.mem, k)
			}
		}
	}
	return c.n
}

func (l *Limiter) memGet(key string) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.mem[key].n
}
