package store

import (
	"context"
	"testing"
)

func TestSearchIndexFailureRetryAndRecovery(t *testing.T) {
	if testPool == nil {
		t.Skip("requires isolated database")
	}
	ctx := context.Background()
	_, _, post := searchFixture(t)
	pid := post.ID
	searchExec(t, `CREATE OR REPLACE FUNCTION fail_search_index() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'search failure'; END $$`)
	searchExec(t, `DROP TRIGGER IF EXISTS fail_search_index ON posts; CREATE TRIGGER fail_search_index BEFORE UPDATE OF search_data ON posts FOR EACH ROW EXECUTE FUNCTION fail_search_index()`)
	t.Cleanup(func() {
		searchExec(t, `DROP TRIGGER IF EXISTS fail_search_index ON posts; DROP FUNCTION IF EXISTS fail_search_index()`)
	})
	if _, err := testPool.Exec(ctx, `INSERT INTO search_index_events(post_id) VALUES($1) ON CONFLICT(post_id) DO UPDATE SET next_attempt_at=now()`, pid); err != nil {
		t.Fatal(err)
	}
	if err := testStore.ProcessSearchIndex(ctx); err == nil {
		t.Fatal("expected injected failure")
	}
	// Recreating the Store verifies persisted state; process restart is exercised
	// separately by scripts/verify-worker-recovery.sh.
	if _, err := testPool.Exec(ctx, `UPDATE search_index_events SET next_attempt_at=now() WHERE post_id=$1`, pid); err != nil {
		t.Fatal(err)
	}
	if err := New(testPool).ProcessSearchIndex(ctx); err == nil {
		t.Fatal("expected failure after worker restart")
	}
	var attempts int
	if err := testPool.QueryRow(ctx, `SELECT attempts FROM search_index_events WHERE post_id=$1`, pid).Scan(&attempts); err != nil || attempts < 2 {
		t.Fatalf("retry state missing: %d %v", attempts, err)
	}
	searchExec(t, `DROP TRIGGER fail_search_index ON posts`)
	if _, err := testPool.Exec(ctx, `UPDATE search_index_events SET next_attempt_at=now() WHERE post_id=$1`, pid); err != nil {
		t.Fatal(err)
	}
	if err := testStore.ProcessSearchIndex(ctx); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM search_index_events WHERE post_id=$1`, pid).Scan(&left); err != nil || left != 0 {
		t.Fatalf("event not drained: %d %v", left, err)
	}
	var correct bool
	if err := testPool.QueryRow(ctx, `SELECT search_data @@ to_tsquery('simple','originalbody') FROM posts WHERE id=$1`, pid).Scan(&correct); err != nil || !correct {
		t.Fatal("recovered index incorrect", err)
	}
}
