// SPDX-License-Identifier: AGPL-3.0-or-later
// check.go：上线检查单（ROADMAP 5.4）。forumd -check-backup 执行后退出，
// 任一项失败进程退出码为 1，可直接用于部署脚本的断言。
package main

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"dzforum/internal/config"
	"dzforum/internal/db"
)

func runCheckBackup(cfg config.Config) int {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	failed := false
	check := func(name string, err error) {
		if err != nil {
			logger.Error("✗ "+name, "err", err)
			failed = true
			return
		}
		logger.Info("✓ " + name)
	}

	// 1. DSN 可达且可写（临时表探测，不触碰业务数据）
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, cfg.DSN)
	if err != nil {
		check("数据库连接", err)
		return 1
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, "CREATE TEMP TABLE _backup_probe (id int)")
	check("数据库可写（临时表探测）", err)

	// 2. schema 迁移版本
	var v int
	if err := pool.QueryRow(ctx,
		`SELECT coalesce(max(version),0) FROM schema_migrations`).Scan(&v); err != nil {
		check("schema_migrations 表", err)
	} else {
		logger.Info("✓ schema 迁移版本", "version", v)
	}

	// 3. pg_dump 在 PATH（备份脚本依赖）
	_, err = exec.LookPath("pg_dump")
	check("pg_dump 在 PATH", err)

	// 4. 运行时数据目录可写
	for _, dir := range []string{cfg.UploadDir, cfg.SmileyDir} {
		err = os.MkdirAll(dir, 0o755)
		if err == nil {
			probe := filepath.Join(dir, ".backup-probe")
			if err = os.WriteFile(probe, []byte("probe"), 0o644); err == nil {
				_ = os.Remove(probe)
			}
		}
		check("目录可写 "+dir, err)
	}

	if failed {
		logger.Error("上线检查未通过，请先处理以上 ✗ 项")
		return 1
	}
	logger.Info("上线检查全部通过")
	return 0
}
