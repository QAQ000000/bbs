// SPDX-License-Identifier: AGPL-3.0-or-later
// Package db 负责 PostgreSQL 连接池与 schema 迁移。
package db

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Open 建立连接池。
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return OpenWithPoolSize(ctx, dsn, 20, 2)
}

func OpenWithPoolSize(ctx context.Context, dsn string, maxConns, minConns int) (*pgxpool.Pool, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("FORUM_DSN is required; implicit PostgreSQL connection defaults are disabled")
	}
	if maxConns < 1 || maxConns > 1000 || minConns < 0 || minConns > maxConns {
		return nil, fmt.Errorf("invalid database pool size: require 1 <= max <= 1000 and 0 <= min <= max")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("解析 DSN: %w", err)
	}
	cfg.MaxConns = int32(maxConns)
	cfg.MinConns = int32(minConns)
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = time.Minute
	cfg.ConnConfig.Tracer = &performanceTracer{logger: slog.Default()}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("连接数据库: %w", err)
	}
	return pool, nil
}
