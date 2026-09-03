// SPDX-License-Identifier: AGPL-3.0-or-later
// Package db 负责 PostgreSQL 连接池与 schema 迁移。
package db

import (
	"context"
	"fmt"
	"time"

	"dzforum/assets"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Open 建立连接池。
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("解析 DSN: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = time.Minute
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

// Migrate 执行内嵌 schema。所有语句均幂等
// （CREATE TABLE/INDEX IF NOT EXISTS、ADD COLUMN IF NOT EXISTS），
// 每次启动都执行，存量库自动获得增量列与新增表。
// 多语句用 simple query protocol 执行。
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pgx.Connect(ctx, pool.Config().ConnString())
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, string(assets.SchemaFile())); err != nil {
		return fmt.Errorf("执行 schema: %w", err)
	}
	return nil
}
