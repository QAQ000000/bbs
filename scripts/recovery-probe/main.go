// recovery-probe exercises real workers against a disposable local database.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"dzforum/internal/api"
	"dzforum/internal/config"
	"dzforum/internal/db"
	"dzforum/internal/live"
	"dzforum/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func validateDSN(dsn string) error {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("invalid test DSN")
	}
	if cfg.ConnConfig.Database != "gobbs_test_worker" || cfg.ConnConfig.User != "gobbs_test_worker" ||
		!strings.HasPrefix(cfg.ConnConfig.Host, "/tmp/gobbs-recovery-cluster.") || len(cfg.ConnConfig.Fallbacks) != 0 {
		return fmt.Errorf("requires script-owned gobbs_test_worker database on private recovery socket")
	}
	return nil
}

func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: recovery-probe seed|worker|inject|wait-locked|rollback|release|verify|replay TARGET, or smtp DIRECTORY")
	}
	mode, target := os.Args[1], os.Args[2]
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if mode == "smtp" {
		return serveSMTP(ctx, target)
	}
	fault, ok := faults[target]
	if !ok {
		return fmt.Errorf("unknown recovery target %q", target)
	}
	dsn := os.Getenv("FORUM_TEST_DSN")
	if err := validateDSN(dsn); err != nil {
		return err
	}
	if mode != "worker" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
	}
	pool, err := db.OpenWithPoolSize(ctx, dsn, 20, 0)
	if err != nil {
		return err
	}
	defer pool.Close()
	s := store.NewWithAsyncForumStats(pool)
	switch mode {
	case "seed":
		return seed(ctx, pool, s)
	case "worker":
		endpoint, err := readSMTPEndpoint(os.Getenv("RECOVERY_SMTP_DIR"))
		if err != nil {
			return err
		}
		// Explicit test configuration: never inherit deployment SMTP credentials.
		server, err := api.New(config.Config{
			SiteName: "Recovery test", SiteURL: "http://recovery.example.test", SMTPHost: "127.0.0.1", SMTPPort: endpoint,
			SMTPFrom: "noreply@example.test", MailKey: strings.Repeat("ab", 32), AsyncForumStats: true,
		}, s, live.NewHub(), slog.Default())
		if err != nil {
			return err
		}
		server.RunBackgroundWorkers(ctx)
		return nil
	case "inject":
		return inject(ctx, pool, fault)
	case "wait-locked":
		return waitCheck(ctx, pool, "fault in flight", lockedQuery)
	case "rollback":
		return assertRollback(ctx, pool, fault)
	case "release":
		return release(ctx, pool, fault)
	case "verify":
		return verify(ctx, pool, target)
	case "replay":
		return replay(ctx, pool)
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
}
