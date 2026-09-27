package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"dzforum/internal/db"
	"dzforum/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

type faultSpec struct{ table, event, when, rollback string }

// Each pause occurs after a real business write but before its transaction commits.
// The advisory marker distinguishes our injected pause from unrelated SQL sleeps.
const lockedQuery = `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a JOIN pg_locks l ON l.pid=a.pid
 WHERE a.datname=current_database() AND a.usename=current_user AND a.wait_event='PgSleep'
 AND l.locktype='advisory' AND l.classid=0 AND l.objid=86420917 AND l.objsubid=1 AND l.granted)`

var faults = map[string]faultSpec{
	"search": {"search_index_events", "DELETE", "", `
 (SELECT count(*) FROM search_index_events)=2 AND NOT EXISTS(SELECT 1 FROM posts WHERE search_data IS NOT NULL)`},
	"forum": {"forum_stat_events", "DELETE", "", `
 (SELECT count(*) FROM forum_stat_events)=(SELECT forum_events FROM recovery_fixture)
 AND NOT EXISTS(SELECT 1 FROM forums WHERE thread_count<>0 OR post_count<>0)`},
	"growth": {"member_events", "DELETE", "WHEN (OLD.kind='thread' AND OLD.active)", `
 (SELECT count(*) FROM member_events)=(SELECT count(*) FROM recovery_growth_events)
 AND NOT EXISTS(SELECT 1 FROM member_experience) AND NOT EXISTS(SELECT 1 FROM points_ledger)
 AND NOT EXISTS(SELECT 1 FROM member_states WHERE experience<>0)
 AND NOT EXISTS(SELECT 1 FROM points_accounts WHERE balance<>0)`},
	"titles": {"title_logs", "INSERT", "WHEN (NEW.action='grant')", `
 NOT EXISTS(SELECT 1 FROM recovery_title_events original WHERE NOT EXISTS(SELECT 1 FROM title_events e WHERE e.id=original.id))
 AND EXISTS(SELECT 1 FROM title_jobs WHERE status='pending')
 AND NOT EXISTS(SELECT 1 FROM user_titles)
 AND NOT EXISTS(SELECT 1 FROM title_logs WHERE action='grant')`},
	"subscriptions": {"subscription_events", "UPDATE OF completed", "WHEN (NEW.completed)", `
 NOT EXISTS(SELECT 1 FROM subscription_events WHERE completed OR cursor_uid<>0)
 AND (SELECT count(*) FROM subscription_events)=2
 AND NOT EXISTS(SELECT 1 FROM subscription_deliveries)
 AND NOT EXISTS(SELECT 1 FROM notifications WHERE type='subscription')
 AND NOT EXISTS(SELECT 1 FROM email_jobs)`},
	"email": {"email_jobs", "UPDATE OF status", "WHEN (NEW.status='sent')", `
 (SELECT count(*) FROM email_jobs)=1
 AND (SELECT count(*) FROM email_jobs WHERE status='sending' AND attempts=1 AND version=2 AND sent_at IS NULL AND lease_until>now())=1`},
}

func seed(ctx context.Context, pool *pgxpool.Pool, s *store.Store) error {
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	author, err := s.CreateUser(ctx, "recovery-author", "unused-test-password", "")
	if err != nil {
		return err
	}
	subscriber, err := s.CreateUser(ctx, "recovery-subscriber", "unused-test-password", "reader@example.test")
	if err != nil {
		return err
	}
	var cid, fid int64
	if err = pool.QueryRow(ctx, `INSERT INTO categories(name) VALUES('recovery') RETURNING id`).Scan(&cid); err != nil {
		return err
	}
	if err = pool.QueryRow(ctx, `INSERT INTO forums(category_id,name) VALUES($1,'recovery') RETURNING id`, cid).Scan(&fid); err != nil {
		return err
	}
	title, err := s.SaveTitle(ctx, store.TitleDefinition{
		Name: "Recovery title", Description: "Create a topic", Status: "draft", Mode: "automatic", Match: "all",
		Badge:      store.LevelBadge{Label: "Title", Icon: "star", Color: "#112233", Background: "#ddeeff"},
		Conditions: []store.TitleCondition{{Metric: "threads_created", Target: 1}},
	}, author.ID)
	if err != nil {
		return err
	}
	title.Status = "active"
	if _, err = s.SaveTitle(ctx, title, author.ID); err != nil {
		return err
	}
	if _, err = s.SaveSubscription(ctx, subscriber.ID, store.Subscription{Kind: "forum", TargetID: fid}, true); err != nil {
		return err
	}
	th, post, err := s.CreateThread(ctx, fid, author.ID, author.Username, "recoveryprobe", "recoveryprobe durable body", "", false, "")
	if err != nil {
		return err
	}
	if _, _, err = s.CreateReply(ctx, th.ID, author.ID, author.Username, "recoveryprobe reply", "", false, ""); err != nil {
		return err
	}
	// Preserve source payloads for explicit redelivery after recovery, including original rule/time.
	_, err = pool.Exec(ctx, `CREATE TABLE recovery_growth_events AS SELECT * FROM member_events;
 CREATE TABLE recovery_title_events AS SELECT * FROM title_events;
 CREATE TABLE recovery_fixture(author_id bigint, subscriber_id bigint, forum_id bigint, post_id bigint, title_id bigint, forum_events bigint)`)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `INSERT INTO recovery_fixture SELECT $1,$2,$3,$4,$5,
 (SELECT count(*) FROM forum_stat_events)`,
		author.ID, subscriber.ID, fid, post.ID, title.ID)
	return err
}

func inject(ctx context.Context, pool *pgxpool.Pool, f faultSpec) error {
	_, err := pool.Exec(ctx, `CREATE FUNCTION recovery_pause() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
 PERFORM pg_advisory_xact_lock(86420917::bigint);
 PERFORM pg_sleep(30);
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
 END $$; CREATE TRIGGER recovery_pause BEFORE `+f.event+` ON `+f.table+` FOR EACH ROW `+f.when+` EXECUTE FUNCTION recovery_pause()`)
	return err
}

func assertRollback(ctx context.Context, pool *pgxpool.Pool, f faultSpec) error {
	var ok bool
	if err := pool.QueryRow(ctx, "SELECT "+f.rollback).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("partial commit or lost work at %s fault", f.table)
	}
	fmt.Println("rollback PASS", f.table)
	return nil
}

func release(ctx context.Context, pool *pgxpool.Pool, f faultSpec) error {
	if _, err := pool.Exec(ctx, `DROP TRIGGER recovery_pause ON `+f.table+`; DROP FUNCTION recovery_pause()`); err != nil {
		return err
	}
	if f.table == "email_jobs" {
		// Test-only clock advance; production's two-minute lease is unchanged.
		tag, err := pool.Exec(ctx, `UPDATE email_jobs SET lease_until=now()-interval '1 second' WHERE status='sending'`)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("expected exactly one abandoned SMTP lease")
		}
		fmt.Println("advanced one test email lease to simulate two-minute expiry")
	}
	return nil
}

func waitCheck(ctx context.Context, pool *pgxpool.Pool, name, query string) error {
	for {
		var ok bool
		if err := pool.QueryRow(ctx, query).Scan(&ok); err != nil {
			return err
		}
		if ok {
			fmt.Println(name, "PASS")
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %w", name, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Checks are positive, exact fixture assertions; empty tables cannot pass by vacuity.
const finalChecks = `SELECT jsonb_build_object(
 'source',(SELECT count(*) FROM users)=2 AND (SELECT count(*) FROM threads)=1 AND (SELECT count(*) FROM posts)=2,
 'search',NOT EXISTS(SELECT 1 FROM search_index_events)
  AND (SELECT count(*) FROM posts WHERE search_data @@ to_tsquery('simple','recoveryprobe'))=2,
 'forum',NOT EXISTS(SELECT 1 FROM forum_stat_events)
  AND (SELECT count(*) FROM forums WHERE thread_count=1 AND post_count=2)=1,
 'growth',NOT EXISTS(SELECT 1 FROM member_events)
  AND (SELECT experience FROM member_states WHERE user_id=(SELECT author_id FROM recovery_fixture))=7
  AND (SELECT count(*) FROM member_experience)=2 AND (SELECT sum(delta) FROM member_experience)=7
  AND (SELECT count(*) FROM member_experience WHERE user_id=(SELECT author_id FROM recovery_fixture)
   AND ((kind='thread' AND delta=5) OR (kind='reply' AND delta=2)) AND source LIKE 'post:%')=2,
 'points',(SELECT balance FROM points_accounts WHERE user_id=(SELECT author_id FROM recovery_fixture))=2
  AND (SELECT count(*) FROM points_ledger)=2 AND (SELECT sum(delta) FROM points_ledger)=2
  AND (SELECT count(*) FROM points_ledger WHERE user_id=(SELECT author_id FROM recovery_fixture)
   AND kind IN ('thread','reply') AND delta=1 AND source LIKE 'post:%')=2,
 'titles',NOT EXISTS(SELECT 1 FROM title_events) AND NOT EXISTS(SELECT 1 FROM title_jobs WHERE status='pending')
  AND (SELECT count(*) FROM title_jobs WHERE status='complete')>=1
  AND (SELECT count(*) FROM user_titles)=1
  AND (SELECT count(*) FROM user_titles WHERE user_id=(SELECT author_id FROM recovery_fixture)
   AND title_id=(SELECT title_id FROM recovery_fixture) AND status='earned' AND source='automatic')=1
  AND (SELECT count(*) FROM title_logs WHERE action='grant')=1,
 'subscriptions',(SELECT count(*) FROM subscription_events WHERE completed)=2
  AND (SELECT count(*) FROM subscription_deliveries)=1
  AND (SELECT count(*) FROM subscription_deliveries WHERE uid=(SELECT subscriber_id FROM recovery_fixture)
   AND post_id=(SELECT post_id FROM recovery_fixture))=1
  AND (SELECT count(*) FROM notifications WHERE type='subscription')=1
  AND (SELECT count(*) FROM notifications WHERE type='subscription'
   AND uid=(SELECT subscriber_id FROM recovery_fixture) AND post_id=(SELECT post_id FROM recovery_fixture))=1,
 'email',(SELECT count(*) FROM email_jobs)=1 AND (SELECT count(*) FROM email_jobs WHERE status='sent'
  AND kind='subscription' AND uid=(SELECT subscriber_id FROM recovery_fixture)
  AND post_id=(SELECT post_id FROM recovery_fixture) AND sent_at IS NOT NULL AND lease_until IS NULL)=1
)`

func verify(ctx context.Context, pool *pgxpool.Pool, target string) error {
	var failed []string
	for {
		var raw []byte
		if err := pool.QueryRow(ctx, finalChecks).Scan(&raw); err != nil {
			return err
		}
		var checks map[string]bool
		if err := json.Unmarshal(raw, &checks); err != nil {
			return err
		}
		failed = nil
		for name, ok := range checks {
			if !ok {
				failed = append(failed, name)
			}
		}
		if len(failed) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("recovery checks failed: %s: %w", strings.Join(failed, ","), ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	if err := verifySMTP(ctx, pool, target); err != nil {
		return err
	}
	fmt.Println("verify PASS", target, "search/forum/growth/points/titles/subscriptions/email; no duplicate business awards")
	return nil
}

func replay(ctx context.Context, pool *pgxpool.Pool) error {
	// At-least-once source events must not duplicate awards or fanout. Original event
	// timestamps are retained; a completed email remains sent and is not requeued.
	_, err := pool.Exec(ctx, `INSERT INTO member_events(user_id,kind,source,active,rule,rule_version,created_at,points_rule,points_version)
 SELECT user_id,kind,source,active,rule,rule_version,created_at,points_rule,points_version FROM recovery_growth_events;
 INSERT INTO title_events(user_id) SELECT author_id FROM recovery_fixture;
 UPDATE subscription_events SET completed=false,cursor_uid=0`)
	return err
}
