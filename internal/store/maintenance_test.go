package store

import (
	"context"
	"testing"
)

func TestDerivedRepairQueuesAtomicallyAndPreservesRetryState(t *testing.T) {
	ctx := t.Context()
	author, _ := setupUsers(t)
	fid := setupForum(t)
	_, post, err := testStore.CreateThread(ctx, fid, author, "author", "repair", "repair body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE search_index_events SET attempts=3,next_attempt_at=now()+interval '1 hour',created_at=now()-interval '1 hour',last_sqlstate='P0001' WHERE post_id=$1`, post.ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := testStore.QueueDerivedRepair(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var valid bool
	if err := testPool.QueryRow(ctx, `SELECT (SELECT count(*) FROM search_index_events WHERE post_id=$1 AND attempts=3 AND next_attempt_at>now() AND created_at<now()-interval '30 minutes' AND last_sqlstate='P0001')=1 AND (SELECT count(*) FROM forum_stat_events WHERE forum_id=$2)=1`, post.ID, fid).Scan(&valid); err != nil || !valid {
		t.Fatalf("repair reset a retry or duplicated a forum job: %v", err)
	}
	if _, err := testPool.Exec(ctx, `DELETE FROM search_index_events WHERE post_id=$1`, post.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `DELETE FROM forum_stat_events WHERE forum_id=$1`, fid); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `CREATE FUNCTION reject_repair_forum() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'injected maintenance failure'; END$$; CREATE TRIGGER reject_repair_forum BEFORE INSERT ON forum_stat_events FOR EACH ROW EXECUTE FUNCTION reject_repair_forum()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DROP TRIGGER IF EXISTS reject_repair_forum ON forum_stat_events; DROP FUNCTION IF EXISTS reject_repair_forum()`)
	})
	if _, err := testStore.QueueDerivedRepair(ctx); err == nil {
		t.Fatal("injected repair failure was ignored")
	}
	if err := testPool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM search_index_events WHERE post_id=$1)`, post.ID).Scan(&valid); err != nil || !valid {
		t.Fatalf("partial repair queue committed: %v", err)
	}
}
