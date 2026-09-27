// SPDX-License-Identifier: AGPL-3.0-or-later
// forumd — GoBBS 论坛服务。
//
// 用法：forumd [-seed | -migrate | -enqueue-derived-repair]
// -seed 启动前灌入演示数据。
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"dzforum/internal/api"
	"dzforum/internal/config"
	"dzforum/internal/db"
	"dzforum/internal/live"
	"dzforum/internal/smiley"
	"dzforum/internal/store"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	cfg := config.FromEnv()
	if len(os.Args) == 2 && os.Args[1] == "-version" {
		println(version)
		return
	}
	flagSeed := flag.Bool("seed", false, "灌入演示数据后启动")
	flagCheck := flag.Bool("check-backup", false, "上线检查：DSN 可写、pg_dump 在 PATH、数据目录可写，然后退出")
	flagMigrate := flag.Bool("migrate", false, "迁移数据库后退出，不启动 HTTP 或 Worker")
	flagRepair := flag.Bool("enqueue-derived-repair", false, "排入搜索和版块统计修复任务后退出；由服务 Worker 消费")
	flagMigrationTimeout := flag.Duration("migration-timeout", 5*time.Minute, "迁移和等待迁移锁的总时限")
	flagImport := flag.String("import-smileys", "", "从指定目录导入图片表情包（结构：<包名>/<图片文件>），导入后退出")
	flagCodes := flag.String("codes", "", "可选：表情代码映射 JSON（配合 -import-smileys）")
	flag.Parse()
	if err := validateStartupFlags(*flagSeed, *flagCheck, *flagMigrate, *flagRepair, *flagImport != "", *flagMigrationTimeout); err != nil {
		slog.Error("启动参数无效", "err", err)
		os.Exit(2)
	}

	if *flagCheck {
		os.Exit(runCheckBackup(cfg))
	}

	if *flagImport != "" {
		if err := importSmileys(*flagImport, cfg.SmileyDir, *flagCodes); err != nil {
			slog.New(slog.NewTextHandler(os.Stderr, nil)).Error("导入表情失败", "err", err)
			os.Exit(1)
		}
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.OpenWithPoolSize(ctx, cfg.DSN, cfg.DBMaxConnections, cfg.DBMinConnections)
	if err != nil {
		logger.Error("数据库连接失败", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	migrationCtx, cancelMigration := context.WithTimeout(ctx, *flagMigrationTimeout)
	err = db.Migrate(migrationCtx, pool)
	cancelMigration()
	if err != nil {
		logger.Error("数据库迁移失败", "err", err)
		os.Exit(1)
	}
	if *flagMigrate {
		logger.Info("数据库迁移完成")
		return
	}
	if *flagRepair {
		repairCtx, cancel := context.WithTimeout(ctx, *flagMigrationTimeout)
		result, err := store.New(pool).QueueDerivedRepair(repairCtx)
		cancel()
		if err != nil {
			logger.Error("派生数据修复入队失败", "err", err)
			os.Exit(1)
		}
		logger.Info("派生数据修复已入队", "searchJobs", result.SearchJobs, "forumJobs", result.ForumJobs)
		return
	}
	monitorDone := make(chan struct{})
	go func() { defer close(monitorDone); db.Monitor(ctx, pool, logger) }()
	defer func() { stop(); <-monitorDone }()

	st := store.New(pool)
	if cfg.AsyncForumStats {
		st = store.NewWithAsyncForumStats(pool)
		logger.Warn("异步版块统计已启用", "consistency", "display-fields-eventual")
	}
	st.StartViewCounter(ctx)
	defer st.StopViewCounter()

	// 权限矩阵：空表播种默认值并加载到运行时（后台矩阵页保存后即时生效）
	if err := st.LoadRolePerms(ctx); err != nil {
		logger.Warn("权限矩阵加载失败", "err", err)
	}

	// 首次启动以环境配置初始化站点设置（不覆盖已有值）
	if err := st.EnsureSettingsDefaults(ctx, map[string]string{
		"site_name":        cfg.SiteName,
		"threads_per_page": strconv.Itoa(cfg.ThreadsPerPage),
		"posts_per_page":   strconv.Itoa(cfg.PostsPerPage),
	}); err != nil {
		logger.Warn("初始化站点设置失败", "err", err)
	}

	// 运行时数据目录
	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		logger.Warn("创建上传目录失败", "dir", cfg.UploadDir, "err", err)
	}
	if err := os.MkdirAll(cfg.SmileyDir, 0o755); err != nil {
		logger.Warn("创建表情目录失败", "dir", cfg.SmileyDir, "err", err)
	}
	if err := smiley.LoadCustom(cfg.SmileyDir); err != nil {
		logger.Info("未加载自定义表情包", "dir", cfg.SmileyDir)
	}

	if *flagSeed {
		if err := seed(ctx, pool, st); err != nil {
			logger.Error("灌入演示数据失败", "err", err)
			os.Exit(1)
		}
	}

	hub := live.NewHub()
	srv, err := api.New(cfg, st, hub, logger)
	if err != nil {
		logger.Error("初始化服务失败", "err", err)
		os.Exit(1)
	}

	workersDone := make(chan struct{})
	go func() { defer close(workersDone); srv.RunBackgroundWorkers(ctx) }()
	defer func() { stop(); <-workersDone }()
	// 会话过期清理
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
				st.PurgeSessions(cleanupCtx)
				if err := st.PurgeMFA(cleanupCtx); err != nil {
					slog.Error("MFA cleanup failed", "err", err)
				}
				cleanupCancel()
			}
		}
	}()

	httpSrv := newHTTPServer(cfg.Addr, srv.Handler())
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	logger.Info("论坛服务已启动", "addr", cfg.Addr, "site", cfg.SiteName)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		logger.Info("服务已停止")
	case err := <-errCh:
		logger.Error("服务异常退出", "err", err)
		os.Exit(1)
	}
}
