package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInitializeSiteAtomic(t *testing.T) {
	ctx := context.Background()
	schema := fmt.Sprintf("setup_test_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := testPool.Exec(ctx, `CREATE SCHEMA `+quoted); err != nil {
		t.Fatal(err)
	}
	defer testPool.Exec(ctx, `DROP SCHEMA `+quoted+` CASCADE`)
	cfg := testPool.Config().Copy()
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `CREATE TABLE users (LIKE public.users INCLUDING ALL);
		CREATE TABLE settings (LIKE public.settings INCLUDING ALL);
		ALTER TABLE settings ADD CONSTRAINT reject_name CHECK (value <> 'reject')`); err != nil {
		t.Fatal(err)
	}
	st := &Store{pool: pool}
	if _, err := st.InitializeSite(ctx, "rollback", "password123", "", "reject"); err == nil {
		t.Fatal("expected settings write failure")
	}
	if exists, err := st.HasUsers(ctx); err != nil || exists {
		t.Fatalf("failed setup left users: %v, %v", exists, err)
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			_, err := st.InitializeSite(ctx, fmt.Sprintf("owner%d", i), "password123", "", "site")
			results <- err
		}(i)
	}
	winners := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrAlreadyInitialized) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("expected one initializer, got %d", winners)
	}
	var admins int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE group_id=1`).Scan(&admins); err != nil || admins != 1 {
		t.Fatalf("admins=%d, err=%v", admins, err)
	}
}

func TestDeletePostConcurrentCounters(t *testing.T) {
	ctx := context.Background()
	author, replier := setupUsers(t)
	fid := setupForum(t)
	th, first, err := testStore.CreateThread(ctx, fid, author, "author", "delete", "body", "body", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, reply, err := testStore.CreateReply(ctx, th.ID, replier, "replier", "body", "body", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, pending, err := testStore.CreateReply(ctx, th.ID, replier, "replier", "body", "body", true, "test")
	if err != nil {
		t.Fatal(err)
	}
	// A separate public thread must retain the replier's count.
	if _, _, err := testStore.CreateThread(ctx, fid, replier, "replier", "retained", "body", "body", false, ""); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, _, err := testStore.DeletePost(ctx, reply.ID); results <- err }()
	}
	winners := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("delete winners=%d", winners)
	}
	var count int
	if err := testPool.QueryRow(ctx, `SELECT post_count FROM threads WHERE id=$1`, th.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("thread count=%d, err=%v", count, err)
	}
	if _, _, err := testStore.DeletePost(ctx, pending.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := testStore.DeletePost(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT post_count FROM users WHERE id=$1`, replier).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retained user count=%d, err=%v", count, err)
	}
}
