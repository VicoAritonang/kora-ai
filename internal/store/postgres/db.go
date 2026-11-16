// Package postgres is the Postgres store. Queries are explicit SQL over pgx
// (no ORM); every method takes ctx first and never calls the network beyond
// the database. Rows are returned as store types or domain types.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Errors returned by the store.
var (
	ErrNotFound = errors.New("store: not found")
	ErrConflict = errors.New("store: conflict")
)

type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DB owns the pool.
type DB struct {
	Pool *pgxpool.Pool
	q    *Q
}

// Q runs queries against the pool or a transaction.
type Q struct{ db dbtx }

// Open connects and verifies the connection.
func Open(ctx context.Context, url string, maxConns int32) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse db url: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	cfg.ConnConfig.RuntimeParams["application_name"] = "kora"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &DB{Pool: pool, q: &Q{db: pool}}, nil
}

// Close releases the pool.
func (d *DB) Close() { d.Pool.Close() }

// Q returns the pool-backed query set.
func (d *DB) Q() *Q { return d.q }

// WithTx runs fn in a transaction (ENGINEERING_STANDARDS §1.3: short, no
// network calls inside).
func (d *DB) WithTx(ctx context.Context, fn func(q *Q) error) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	if err := fn(&Q{db: tx}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Ping checks the database for /readyz.
func (d *DB) Ping(ctx context.Context) error { return d.Pool.Ping(ctx) }

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// IsUniqueViolation reports a unique constraint violation (23505).
func IsUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func dateOrNil(s string) any {
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return t
}

func strOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}
