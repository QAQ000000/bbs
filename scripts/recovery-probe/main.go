// recovery-probe exercises real workers against a disposable local database.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"dzforum/internal/db"
	"dzforum/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: recovery-probe seed|worker|verify|wait-locked")
	}
	dsn := os.Getenv("FORUM_TEST_DSN")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "gobbs_test_") {
		return fmt.Errorf("requires disposable gobbs_test_ database")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := db.OpenWithPoolSize(ctx, dsn, 5, 0)
	if err != nil {
		return err
	}
	defer pool.Close()
	s := store.NewWithAsyncForumStats(pool)
	switch os.Args[1] {
	case "seed":
		if err := db.Migrate(ctx, pool); err != nil {
			return err
		}
		u, err := s.CreateUser(ctx, fmt.Sprintf("recovery-%d", time.Now().UnixNano()), "unused-test-password", "")
		if err != nil {
			return err
		}
		var cid, fid int64
		if err := pool.QueryRow(ctx, `INSERT INTO categories(name) VALUES('recovery') RETURNING id`).Scan(&cid); err != nil {
			return err
		}
		if err := pool.QueryRow(ctx, `INSERT INTO forums(category_id,name) VALUES($1,'recovery') RETURNING id`, cid).Scan(&fid); err != nil {
			return err
		}
		th, _, err := s.CreateThread(ctx, fid, u.ID, u.Username, "recoveryprobe", "durable body", "", false, "")
		if err != nil {
			return err
		}
		_, _, err = s.CreateReply(ctx, th.ID, u.ID, u.Username, "recoveryprobe reply", "", false, "")
		return err
	case "worker":
		var wg sync.WaitGroup
		for _, work := range []func(context.Context, *slog.Logger){s.RunSearchIndex, s.RunForumStats} {
			wg.Add(1)
			go func(work func(context.Context, *slog.Logger)) { defer wg.Done(); work(ctx, slog.Default()) }(work)
		}
		wg.Wait()
		return nil
	case "verify", "wait-locked":
		deadline, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		for {
			var ok bool
			query := `SELECT NOT EXISTS(SELECT 1 FROM search_index_events) AND NOT EXISTS(SELECT 1 FROM forum_stat_events)
AND NOT EXISTS(SELECT 1 FROM posts WHERE search_data IS NULL OR NOT (search_data @@ to_tsquery('simple','recoveryprobe')))
AND NOT EXISTS(SELECT 1 FROM forums f WHERE f.post_count<>(SELECT count(*) FROM posts p JOIN threads t ON t.id=p.thread_id WHERE t.forum_id=f.id) OR f.thread_count<>(SELECT count(*) FROM threads t WHERE t.forum_id=f.id))`
			if os.Args[1] == "wait-locked" {
				query = `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND usename=current_user AND wait_event='PgSleep' AND query LIKE 'UPDATE posts SET search_data%')`
			}
			if err := pool.QueryRow(deadline, query).Scan(&ok); err != nil {
				return err
			}
			if ok {
				fmt.Println(os.Args[1], "PASS")
				return nil
			}
			select {
			case <-deadline.Done():
				return fmt.Errorf("%s timed out", os.Args[1])
			case <-time.After(100 * time.Millisecond):
			}
		}
	default:
		return fmt.Errorf("unknown mode")
	}
}
