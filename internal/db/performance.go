package db

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type queryTraceKey struct{}
type acquireTraceKey struct{}
type queryTiming struct {
	start time.Time
	sql   string
}
type performanceTracer struct{ logger *slog.Logger }

func (t *performanceTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryTraceKey{}, queryTiming{time.Now(), d.SQL})
}

func (t *performanceTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	v, ok := ctx.Value(queryTraceKey{}).(queryTiming)
	if !ok || time.Since(v.start) < 500*time.Millisecond {
		return
	}
	// SQL literals, arguments and error details may contain account secrets.
	fingerprint := sha256.Sum256([]byte(v.sql))
	state := ""
	var pgErr *pgconn.PgError
	if errors.As(d.Err, &pgErr) {
		state = pgErr.Code
	}
	t.logger.Warn("db slow query", "queryHash", fmt.Sprintf("%x", fingerprint[:8]),
		"durationMs", time.Since(v.start).Milliseconds(), "failed", d.Err != nil, "sqlState", state)
}

func (t *performanceTracer) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	return context.WithValue(ctx, acquireTraceKey{}, time.Now())
}

func (t *performanceTracer) TraceAcquireEnd(ctx context.Context, pool *pgxpool.Pool, d pgxpool.TraceAcquireEndData) {
	start, ok := ctx.Value(acquireTraceKey{}).(time.Time)
	if !ok || time.Since(start) < 100*time.Millisecond {
		return
	}
	s := pool.Stat()
	t.logger.Warn("db connection wait", "durationMs", time.Since(start).Milliseconds(),
		"failed", d.Err != nil, "acquired", s.AcquiredConns(), "max", s.MaxConns())
}

type PoolStats struct {
	Max                  int32 `json:"max"`
	Total                int32 `json:"total"`
	Acquired             int32 `json:"acquired"`
	Idle                 int32 `json:"idle"`
	AcquireCount         int64 `json:"acquireCount"`
	AcquireDurationMS    int64 `json:"acquireDurationMs"`
	EmptyAcquireCount    int64 `json:"emptyAcquireCount"`
	CanceledAcquireCount int64 `json:"canceledAcquireCount"`
}

func Snapshot(pool *pgxpool.Pool) PoolStats {
	s := pool.Stat()
	return PoolStats{s.MaxConns(), s.TotalConns(), s.AcquiredConns(), s.IdleConns(),
		s.AcquireCount(), s.AcquireDuration().Milliseconds(), s.EmptyAcquireCount(), s.CanceledAcquireCount()}
}

// Monitor samples the application pool and current database lock waits without logging query text.
func Monitor(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			logger.Info("db pool", "stats", Snapshot(pool))
			probe, cancel := context.WithTimeout(ctx, 2*time.Second)
			var waits int
			err := pool.QueryRow(probe, `SELECT count(*) FROM pg_stat_activity
			 WHERE datname=current_database() AND usename=current_user AND wait_event_type='Lock'`).Scan(&waits)
			cancel()
			if err != nil {
				logger.Warn("db lock sample unavailable")
			} else if waits > 0 {
				logger.Warn("db lock waits", "sessions", waits)
			}
		}
	}
}
