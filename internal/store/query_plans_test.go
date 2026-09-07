package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Representative data and all DDL are confined to a rolled-back test transaction.
func TestPerformanceQueryPlans(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	uid, _ := setupUsers(t)
	fid := setupForum(t)
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SET LOCAL statement_timeout='20s';
	 ALTER TABLE threads DISABLE TRIGGER USER; ALTER TABLE posts DISABLE TRIGGER USER;`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO threads(forum_id,author_id,title,last_post_at)
	 SELECT $1,$2,'query-plan',current_date-g*interval '2 minutes' FROM generate_series(1,20000) g`, fid, uid); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO posts(thread_id,author_id,floor,content_md,content_html,created_at)
	 SELECT t.id,$2,g,'plan body','',t.last_post_at FROM threads t CROSS JOIN generate_series(1,4) g
	 WHERE t.forum_id=$1`, fid, uid); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `ANALYZE threads; ANALYZE posts`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, sql, index string }{
		{"latest", `SELECT id,last_post_at FROM threads WHERE NOT deleted AND NOT pending ORDER BY last_post_at DESC,id DESC LIMIT 20`, "threads_public_latest_idx"},
		{"recent_posts", `SELECT count(*) FROM posts WHERE NOT deleted AND NOT pending AND created_at>=current_date-interval '1 day'`, "posts_public_created_idx"},
	} {
		var raw []byte
		if err = tx.QueryRow(ctx, `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) `+test.sql).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), test.index) {
			t.Fatalf("%s did not use %s: %s", test.name, test.index, raw)
		}
		var plans []struct {
			ExecutionTime float64        `json:"Execution Time"`
			Plan          map[string]any `json:"Plan"`
		}
		if err = json.Unmarshal(raw, &plans); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: index=%s executionMs=%.3f sharedHitBlocks=%v", test.name, test.index, plans[0].ExecutionTime, plans[0].Plan["Shared Hit Blocks"])
	}
}
