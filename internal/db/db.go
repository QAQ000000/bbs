// SPDX-License-Identifier: AGPL-3.0-or-later
// Package db 负责 PostgreSQL 连接池与 schema 迁移。
package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"dzforum/assets"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Open 建立连接池。
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return OpenWithPoolSize(ctx, dsn, 20, 2)
}

func OpenWithPoolSize(ctx context.Context, dsn string, maxConns, minConns int) (*pgxpool.Pool, error) {
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

// Migrate 执行 schema 初始化与编号迁移（ROADMAP 5.2）：
//
//  1. schema.sql 整体幂等重放（CREATE ... IF NOT EXISTS），保持「当前完整形态」，
//     全新库一步到位，存量库的非破坏性增量（加表/加列）也由此获得；
//  2. db/migrations/NNN_*.sql 中版本号大于 schema_migrations.max(version) 的迁移
//     按序执行并登记版本号——破坏性变更（改约束/改类型）写在这里，只执行一次。
//
// 迁移文件为内嵌可信资产，经 simple protocol 多语句执行（与 schema.sql 同路径）；
// 约定迁移必须幂等：若执行失败未登记版本，下次启动将重试。
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pgx.Connect(ctx, pool.Config().ConnString())
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, string(assets.SchemaFile())); err != nil {
		return fmt.Errorf("执行 schema: %w", err)
	}

	var cur int
	if err := conn.QueryRow(ctx,
		`SELECT coalesce(max(version),0) FROM schema_migrations`).Scan(&cur); err != nil {
		return fmt.Errorf("读取迁移版本: %w", err)
	}
	for _, m := range assets.Migrations() {
		if m.Version <= cur {
			continue
		}
		if _, err := conn.Exec(ctx, string(assets.MigrationSQL(m.Version))); err != nil {
			return fmt.Errorf("执行迁移 %03d (%s) 失败: %w", m.Version, m.Name, err)
		}
		if _, err := conn.Exec(ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)", m.Version); err != nil {
			return fmt.Errorf("登记迁移版本 %03d 失败: %w", m.Version, err)
		}
		slog.Info("已应用编号迁移", "version", m.Version, "name", m.Name)
	}
	return nil
}
