package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func searchExec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func searchFixture(t *testing.T) (int64, *Thread, *Post) {
	t.Helper()
	// Tests do not run concurrently. Hide unrelated queues without deleting them.
	searchExec(t, `UPDATE search_index_events SET next_attempt_at=now()+interval '1 day'`)
	t.Cleanup(func() { searchExec(t, `UPDATE search_index_events SET next_attempt_at=now()`) })
	uid, _ := setupUsers(t)
	th, p, err := testStore.CreateThread(context.Background(), setupForum(t), uid, "author", "searchtitle", "originalbody", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	return uid, th, p
}

func searchDrain(t *testing.T, pid int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		var n int
		if err := testPool.QueryRow(ctx, `SELECT count(*) FROM search_index_events WHERE post_id=$1`, pid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return
		}
		if err := testStore.ProcessSearchIndex(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSearchPublicationEnqueueRollback(t *testing.T) {
	uid, th, p := searchFixture(t)
	searchDrain(t, p.ID)
	searchExec(t, `CREATE FUNCTION fail_enqueue_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'enqueue failure'; END $$;
CREATE TRIGGER fail_enqueue_test BEFORE INSERT ON search_index_events FOR EACH ROW EXECUTE FUNCTION fail_enqueue_test()`)
	t.Cleanup(func() {
		searchExec(t, `DROP TRIGGER fail_enqueue_test ON search_index_events; DROP FUNCTION fail_enqueue_test()`)
	})
	ctx := context.Background()
	if _, _, err := testStore.CreateThread(ctx, th.ForumID, uid, "author", "rollback", "body", "", false, ""); err == nil {
		t.Fatal("thread enqueue failure ignored")
	}
	if _, _, err := testStore.CreateReply(ctx, th.ID, uid, "author", "rollback", "", false, ""); err == nil {
		t.Fatal("reply enqueue failure ignored")
	}
	if _, _, err := testStore.UpdatePost(ctx, p.ID, p.Version, uid, "", "changed", "changed", ""); err == nil {
		t.Fatal("edit enqueue failure ignored")
	}
	var valid bool
	if err := testPool.QueryRow(ctx, `SELECT
(SELECT count(*) FROM threads WHERE forum_id=$1)=1 AND
(SELECT count(*) FROM posts WHERE thread_id=$2)=1 AND
(SELECT post_count=1 AND floor_seq=1 FROM threads WHERE id=$2) AND
(SELECT post_count=1 FROM users WHERE id=$3) AND
(SELECT post_count=1 AND thread_count=1 FROM forums WHERE id=$1) AND
(SELECT content_md='originalbody' AND version=$5 FROM posts WHERE id=$4) AND
NOT EXISTS(SELECT 1 FROM post_edits WHERE post_id=$4)`, th.ForumID, th.ID, uid, p.ID, p.Version).Scan(&valid); err != nil || !valid {
		t.Fatalf("rollback drift: %v", err)
	}
}

func TestSearchDeleteRestoreAndModeration(t *testing.T) {
	uid, th, p := searchFixture(t)
	ctx := context.Background()
	_, reply, err := testStore.CreateReply(ctx, th.ID, uid, "author", "replykeyword", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	searchDrain(t, reply.ID)
	if _, _, err := testStore.DeletePost(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	searchDrain(t, reply.ID)
	if err := testStore.RestoreThread(ctx, th.ID); err != nil {
		t.Fatal(err)
	}
	searchDrain(t, reply.ID)
	var indexed bool
	if err := testPool.QueryRow(ctx, `SELECT coalesce(search_data @@ to_tsquery('simple','replykeyword'),false) FROM posts WHERE id=$1`, reply.ID).Scan(&indexed); err != nil || !indexed {
		t.Fatal("restored reply not indexed", err)
	}
	if err := testStore.SetPostPendingModeration(ctx, p.ID, "review"); err != nil {
		t.Fatal(err)
	}
	searchDrain(t, p.ID)
	hits, _, err := testStore.Search(ctx, "replykeyword", 1, 10, SearchOpts{})
	if err != nil || len(hits) != 0 {
		t.Fatal("pending thread leaked", err)
	}
	if err := testStore.SetThreadApproved(ctx, th.ID); err != nil {
		t.Fatal(err)
	}
	searchDrain(t, reply.ID)
	hits, _, err = testStore.Search(ctx, "replykeyword", 1, 10, SearchOpts{})
	if err != nil || len(hits) != 1 {
		t.Fatal("approved reply not searchable", err)
	}
}

func TestSearchConcurrentEditingConsumption(t *testing.T) {
	uid, _, p := searchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 40; i++ {
			if err := testStore.ProcessSearchIndex(ctx); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	var editErr error
	for i := 0; i < 20; i++ {
		if _, _, err := testStore.UpdatePost(ctx, p.ID, 0, uid, "", "latesttitle", fmt.Sprintf("versiontoken%d", i), ""); err != nil {
			editErr = err
			break
		}
	}
	workerErr := <-done
	if editErr != nil || workerErr != nil {
		t.Fatalf("concurrent edit=%v worker=%v", editErr, workerErr)
	}
	searchDrain(t, p.ID)
	var exact bool
	if err := testPool.QueryRow(ctx, `SELECT search_data=setweight(to_tsvector('simple','latesttitle'),'A') || setweight(to_tsvector('simple','versiontoken19'),'B') FROM posts WHERE id=$1`, p.ID).Scan(&exact); err != nil || !exact {
		t.Fatal("stale index after concurrent edit", err)
	}
}

func TestSearchSkipsLockedWriter(t *testing.T) {
	_, th, p := searchFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `SELECT id FROM threads WHERE id=$1 FOR UPDATE`, th.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE posts SET content_md='newestbody' WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- testStore.ProcessSearchIndex(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("consumer waited on locked writer instead of skipping")
	}
	if err := queueSearchIndexTx(ctx, tx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	searchDrain(t, p.ID)
	var current bool
	if err := testPool.QueryRow(ctx, `SELECT search_data @@ to_tsquery('simple','newestbody') FROM posts WHERE id=$1`, p.ID).Scan(&current); err != nil || !current {
		t.Fatal("lost latest edit", err)
	}
}
