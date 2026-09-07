package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestReportConcurrentResolutionAndDeletedContent(t *testing.T) {
	ctx := context.Background()
	owner, reporter := setupUsers(t)
	fid := setupForum(t)
	if _, err := testPool.Exec(ctx, `UPDATE users SET group_id=1 WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	for _, alreadyDeleted := range []bool{false, true} {
		_, p, err := testStore.CreateThread(ctx, fid, owner, "owner", "report", "body", "", false, "")
		if err != nil {
			t.Fatal(err)
		}
		if err = testStore.CreateReport(ctx, p.ID, reporter, "reason"); err != nil {
			t.Fatal(err)
		}
		var rid int64
		if err = testPool.QueryRow(ctx, `SELECT id FROM reports WHERE post_id=$1`, p.ID).Scan(&rid); err != nil {
			t.Fatal(err)
		}
		if _, _, _, _, err = testStore.HandleReport(ctx, rid, reporter, "delete", ""); !errors.Is(err, ErrReportForbidden) {
			t.Fatal("member handled report", err)
		}
		if alreadyDeleted {
			if _, _, err = testStore.DeletePost(ctx, p.ID); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		results := make(chan error, 2)
		start := make(chan struct{})
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, _, _, _, e := testStore.HandleReport(ctx, rid, owner, "delete", "")
				results <- e
			}()
		}
		close(start)
		wg.Wait()
		close(results)
		success := 0
		for e := range results {
			if e == nil {
				success++
			} else if !errors.Is(e, ErrReportNotFound) {
				t.Fatal(e)
			}
		}
		if success != 1 {
			t.Fatal("report handled twice", success)
		}
		var valid bool
		if err = testPool.QueryRow(ctx, `SELECT r.status='resolved' AND p.deleted AND (SELECT count(*) FROM notifications WHERE uid=$2 AND type='report.resolved' AND payload->>'reportId'=$1::bigint::text)=1 FROM reports r JOIN posts p ON p.id=r.post_id WHERE r.id=$1`, rid, reporter).Scan(&valid); err != nil || !valid {
			t.Fatal("inconsistent report result", valid, err)
		}
	}
}

func TestContentTransitionsRollbackStatisticsAndEvents(t *testing.T) {
	ctx := context.Background()
	uid, replier := setupUsers(t)
	fid := setupForum(t)
	th, p, err := testStore.CreateThread(ctx, fid, uid, "owner", "original", "original body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	_, reply, err := testStore.CreateReply(ctx, th.ID, replier, "replier", "reply", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		var value string
		err := testPool.QueryRow(ctx, `SELECT jsonb_build_object(
 'thread',to_jsonb(t),'forum',to_jsonb(f),
 'posts',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM posts p WHERE thread_id=t.id),
 'users',(SELECT jsonb_agg(jsonb_build_array(id,post_count) ORDER BY id) FROM users WHERE id IN ($2,$3)),
 'edits',(SELECT count(*) FROM post_edits WHERE post_id IN (SELECT id FROM posts WHERE thread_id=t.id)),
 'notifications',(SELECT count(*) FROM notifications WHERE thread_id=t.id),
 'events',(SELECT count(*) FROM member_events WHERE user_id IN ($2,$3)))::text
 FROM threads t JOIN forums f ON f.id=t.forum_id WHERE t.id=$1`, th.ID, uid, replier).Scan(&value)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	if _, err = testPool.Exec(ctx, `CREATE FUNCTION fail_flow_stats() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'stats failure'; END $$; CREATE TRIGGER fail_flow_stats BEFORE UPDATE ON forums FOR EACH ROW EXECUTE FUNCTION fail_flow_stats()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(ctx, `DROP TRIGGER fail_flow_stats ON forums; DROP FUNCTION fail_flow_stats()`); err != nil {
			t.Error(err)
		}
	})
	before := snapshot()
	if _, _, err = testStore.UpdatePostModerated(ctx, p.ID, p.Version, uid, "unreviewed", "changed body", "", true, "review"); err == nil {
		t.Fatal("stats failure ignored")
	}
	if snapshot() != before {
		t.Fatal("edit leaked partial content, counters or events")
	}
	if _, _, err = testStore.DeletePost(ctx, reply.ID); err == nil {
		t.Fatal("delete stats failure ignored")
	}
	if snapshot() != before {
		t.Fatal("delete leaked partial counters or events")
	}
	if _, err = testPool.Exec(ctx, `ALTER TABLE forums DISABLE TRIGGER fail_flow_stats`); err != nil {
		t.Fatal(err)
	}
	if err = testStore.SetPostPendingModeration(ctx, reply.ID, "review"); err != nil {
		t.Fatal(err)
	}
	if _, err = testPool.Exec(ctx, `ALTER TABLE forums ENABLE TRIGGER fail_flow_stats`); err != nil {
		t.Fatal(err)
	}
	before = snapshot()
	if _, _, err = testStore.SetPostApproved(ctx, reply.ID); err == nil {
		t.Fatal("reply approval stats failure ignored")
	}
	if snapshot() != before {
		t.Fatal("approval leaked counters or result notifications")
	}
	if _, err = testPool.Exec(ctx, `ALTER TABLE forums DISABLE TRIGGER fail_flow_stats`); err != nil {
		t.Fatal(err)
	}
	if err = testStore.SetPostPendingModeration(ctx, p.ID, "review"); err != nil {
		t.Fatal(err)
	}
	if _, err = testPool.Exec(ctx, `ALTER TABLE forums ENABLE TRIGGER fail_flow_stats`); err != nil {
		t.Fatal(err)
	}
	before = snapshot()
	if err = testStore.SetThreadApproved(ctx, th.ID); err == nil {
		t.Fatal("thread approval stats failure ignored")
	}
	if snapshot() != before {
		t.Fatal("thread approval leaked counters or events")
	}
}

func TestContentConcurrentPublicationAndModeration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	uid, _ := setupUsers(t)
	fid := setupForum(t)
	th, p, err := testStore.CreateThread(ctx, fid, uid, "owner", "moderate", "body", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 8; i++ {
			if e := testStore.SetPostPendingModeration(ctx, p.ID, "review"); e != nil {
				results <- e
				return
			}
			if e := testStore.SetThreadApproved(ctx, th.ID); e != nil {
				results <- e
				return
			}
		}
		results <- nil
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 8; i++ {
			if _, _, e := testStore.CreateThread(ctx, fid, uid, "owner", "concurrent", "body", "", false, ""); e != nil {
				results <- e
				return
			}
		}
		results <- nil
	}()
	close(start)
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal(e)
		}
	}
	var count, userCount int
	if err = testPool.QueryRow(ctx, `SELECT f.post_count,u.post_count FROM forums f,users u WHERE f.id=$1 AND u.id=$2`, fid, uid).Scan(&count, &userCount); err != nil || count != 9 || userCount != 9 {
		t.Fatal("counter drift", count, userCount, err)
	}
}
