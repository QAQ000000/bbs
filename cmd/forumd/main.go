// SPDX-License-Identifier: AGPL-3.0-or-later
// forumd — GoBBS 论坛服务。
//
// 用法：forumd [-addr 127.0.0.1:8080] [-seed]
// -seed 启动前灌入演示数据。
package main

import (
	"context"
	"flag"
	"strconv"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dzforum/internal/config"
	"dzforum/internal/db"
	"dzforum/internal/live"
	"dzforum/internal/smiley"
	"dzforum/internal/store"
	"dzforum/internal/web"
)

func main() {
	cfg := config.FromEnv()
	flagSeed := flag.Bool("seed", false, "灌入演示数据后启动")
	flagImport := flag.String("import-smileys", "", "从指定目录导入图片表情包（结构：<包名>/<图片文件>），导入后退出")
	flagCodes := flag.String("codes", "", "可选：表情代码映射 JSON（配合 -import-smileys）")
	flag.Parse()

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

	pool, err := db.Open(ctx, cfg.DSN)
	if err != nil {
		logger.Error("数据库连接失败", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		logger.Error("数据库迁移失败", "err", err)
		os.Exit(1)
	}

	st := store.New(pool)
	st.StartViewCounter(ctx)
	defer st.StopViewCounter()

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

	// 存量帖子补齐搜索索引（分词器升级或新装数据）
	if n, err := st.ReindexSearch(ctx); err != nil {
		logger.Warn("搜索索引重建失败", "err", err)
	} else if n > 0 {
		logger.Info("已补齐搜索索引", "posts", n)
	}

	// 版块公开口径统计全量重算（修复历史漂移：删除未回补、测试残留等）
	if err := st.RecomputeAllForumStats(ctx); err != nil {
		logger.Warn("版块统计重算失败", "err", err)
	}

	if *flagSeed {
		if err := seed(ctx, pool, st); err != nil {
			logger.Error("灌入演示数据失败", "err", err)
			os.Exit(1)
		}
	}

	hub := live.NewHub()
	srv, err := web.New(cfg, st, hub, logger)
	if err != nil {
		logger.Error("初始化服务失败", "err", err)
		os.Exit(1)
	}

	// 会话过期清理
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				st.PurgeSessions(context.Background())
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
