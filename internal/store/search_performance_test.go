// Test-only reference to the pre-optimization query at abe34e4. A stable post-ID
// tie-break is shared with the new query so fixtures can compare equal ranks.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func (s *Store) searchBeforePageOptimization(ctx context.Context, q string, page, size int, opts SearchOpts) ([]*SearchHit, int, error) {
	tq := SearchQueryTokens(q)
	if tq == "" {
		return nil, 0, nil
	}
	tokens := strings.Fields(tq)
	offset := (page - 1) * size
	// 取足够多的命中做主题级去重分页（小型论坛数据量下足够）
	// 摘要不用 ts_headline：simple 配置对原文分词为单字，与 bigram 词素不匹配，永远无法高亮
	rows, err := s.pool.Query(ctx,
		searchLegacyPlanSQL(ctx), tq, opts.ForumID, opts.Author)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	seen := map[int64]bool{}
	var hits []*SearchHit
	for rows.Next() {
		var h SearchHit
		var raw string
		if err := rows.Scan(&h.ThreadID, &h.Title, &h.ForumID, &h.ForumName, &h.AuthorName,
			&h.CreatedAt, &raw, &h.Rank); err != nil {
			return nil, 0, err
		}
		h.Excerpt = buildExcerpt(raw, tokens)
		if seen[h.ThreadID] {
			continue
		}
		seen[h.ThreadID] = true
		hits = append(hits, &h)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	total := len(hits)
	lo, hi := offset, offset+size
	if lo > total {
		lo = total
	}
	if hi > total {
		hi = total
	}
	return hits[lo:hi], total, nil
}

func searchLegacyPlanSQL(ctx context.Context) string {
	return `SELECT t.id, t.title, t.forum_id, f.name, u.username, t.created_at,
			left(p.content_md, 800),
			ts_rank(p.search_data, q) AS score
		 FROM posts p
		 JOIN threads t ON t.id = p.thread_id AND NOT t.deleted AND NOT t.pending
		 JOIN users u ON u.id = t.author_id
		 JOIN forums f ON f.id = t.forum_id
		 CROSS JOIN (SELECT to_tsquery('simple', $1) AS q) qq
		 WHERE p.search_data @@ q AND NOT p.deleted AND NOT p.pending
		   AND ($2::bigint = 0 OR t.forum_id = $2)
		   AND ($3::text = '' OR u.username = $3)` + forumFilter(ctx, "t.forum_id") + `
		 ORDER BY score DESC, p.id DESC LIMIT 400`
}

// Seed only an isolated test database; restore fixture triggers before timing.
func searchPerformanceFixture(tb testing.TB, threads int) (int64, int64) {
	tb.Helper()
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		tb.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var cid, fid, uid int64
	if err := tx.QueryRow(ctx, "INSERT INTO categories(name) VALUES('search performance') RETURNING id").Scan(&cid); err != nil {
		tb.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO forums(category_id,name) VALUES($1,'search performance') RETURNING id`, cid).Scan(&fid); err != nil {
		tb.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO users(username,password_hash) VALUES($1,'unusable') RETURNING id`, fmt.Sprintf("search-performance-%d", fid)).Scan(&uid); err != nil {
		tb.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `ALTER TABLE threads DISABLE TRIGGER USER; ALTER TABLE posts DISABLE TRIGGER USER`); err != nil {
		tb.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO threads(forum_id,author_id,title,post_count,floor_seq)
        SELECT $1,$2,'search topic '||g,5,5 FROM generate_series(1,$3::int) g`, fid, uid, threads); err != nil {
		tb.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO posts(thread_id,author_id,floor,content_md,content_html,search_data)
        SELECT t.id,$2,g,repeat('load content 中文搜索 ',40)||CASE WHEN t.title='search topic 1' THEN 'rareword' ELSE '' END,'',
        to_tsvector('simple',repeat('load content 中文 文搜 搜索 ',40)||CASE WHEN t.title='search topic 1' THEN 'rareword' ELSE '' END)
        FROM threads t CROSS JOIN generate_series(1,5) g WHERE t.forum_id=$1`, fid, uid); err != nil {
		tb.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE threads t SET first_post_id=p.id FROM posts p WHERE p.thread_id=t.id AND p.floor=1 AND t.forum_id=$1`, fid); err != nil {
		tb.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `ALTER TABLE threads ENABLE TRIGGER USER; ALTER TABLE posts ENABLE TRIGGER USER; ANALYZE posts; ANALYZE threads`); err != nil {
		tb.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		tb.Fatal(err)
	}
	return fid, uid
}
func BenchmarkSearchPage(b *testing.B) {
	fid, _ := searchPerformanceFixture(b, 10000)
	ctx := WithVisibleForums(context.Background(), []int64{fid})
	opts := SearchOpts{ForumID: fid}
	b.Log("fixture: 10000 topics, 50000 indexed posts, mixed Chinese/English, warm cache")
	for _, q := range []string{"content", "rareword", "中文搜索"} {
		for _, implementation := range []struct {
			name string
			run  func(context.Context, string, int, int, SearchOpts) ([]*SearchHit, int, error)
		}{{"before", testStore.searchBeforePageOptimization}, {"after", testStore.Search}} {
			b.Run(q+"/"+implementation.name, func(b *testing.B) {
				if _, _, err := implementation.run(ctx, q, 1, 20, opts); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					hits, total, err := implementation.run(ctx, q, 1, 20, opts)
					if err != nil || len(hits) == 0 || total == 0 {
						b.Fatal(total, err)
					}
				}
			})
		}
	}
	for _, variant := range []string{"before", "after"} {
		sql := searchPageSQL(ctx)
		args := []any{SearchQueryTokens("content"), fid, "", 20, 0}
		if variant == "before" {
			sql = searchLegacyPlanSQL(ctx)
			args = args[:3]
		}
		var plan json.RawMessage
		if err := testPool.QueryRow(ctx, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+sql, args...).Scan(&plan); err != nil {
			b.Fatal(err)
		}
		b.Logf("SEARCH_PLAN %s %s", variant, plan)
	}
}

func TestSearchPagePreservesCandidatesAndVisibility(t *testing.T) {
	fid, uid := searchPerformanceFixture(t, 420)
	hiddenFid, otherUID := searchPerformanceFixture(t, 12)
	ctx := context.Background()
	// Non-uniform relevance, duplicate hits, another reply author, and hidden rows.
	for _, query := range []string{
		`UPDATE posts SET search_data=setweight(search_data,'A'),content_md='best content 中文搜索' WHERE thread_id IN (SELECT id FROM threads WHERE forum_id=$1 ORDER BY id LIMIT 20) AND floor=2`,
		`UPDATE threads SET pending=true WHERE id IN (SELECT id FROM threads WHERE forum_id=$1 ORDER BY id LIMIT 2)`,
		`UPDATE threads SET deleted=true WHERE id IN (SELECT id FROM threads WHERE forum_id=$1 ORDER BY id OFFSET 2 LIMIT 2)`,
		`UPDATE posts SET pending=true WHERE floor=5 AND thread_id IN (SELECT id FROM threads WHERE forum_id=$1 ORDER BY id LIMIT 30)`,
		`UPDATE posts SET deleted=true WHERE floor=4 AND thread_id IN (SELECT id FROM threads WHERE forum_id=$1 ORDER BY id LIMIT 30)`,
	} {
		if _, err := testPool.Exec(ctx, query, fid); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := testPool.Exec(ctx, `UPDATE posts SET author_id=$2 WHERE floor=3 AND thread_id IN(SELECT id FROM threads WHERE forum_id=$1)`, fid, otherUID); err != nil {
		t.Fatal(err)
	}
	var author string
	if err := testPool.QueryRow(ctx, `SELECT username FROM users WHERE id=$1`, uid).Scan(&author); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		ctx  context.Context
		opts SearchOpts
		q    string
	}{
		{"all", ctx, SearchOpts{}, "content"},
		{"visible", WithVisibleForums(ctx, []int64{fid}), SearchOpts{}, "content"},
		{"empty_scope", WithVisibleForums(ctx, []int64{}), SearchOpts{}, "content"},
		{"forum", ctx, SearchOpts{ForumID: fid}, "content"},
		{"denied_forum", WithVisibleForums(ctx, []int64{fid}), SearchOpts{ForumID: hiddenFid}, "content"},
		{"author", ctx, SearchOpts{Author: author}, "content"},
		{"missing_author", ctx, SearchOpts{Author: "nonexistent-search-author"}, "content"},
		{"chinese", ctx, SearchOpts{}, "中文搜索"},
		{"no_hit", ctx, SearchOpts{}, "absentword"},
		{"empty", ctx, SearchOpts{}, "!!!"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, page := range []int{1, 2, 20, 999} {
				want, wt, err := testStore.searchBeforePageOptimization(tc.ctx, tc.q, page, 20, tc.opts)
				if err != nil {
					t.Fatal(err)
				}
				got, gt, err := testStore.Search(tc.ctx, tc.q, page, 20, tc.opts)
				if err != nil || gt != wt || len(got) != len(want) {
					t.Fatalf("page=%d total=%d/%d rows=%d/%d err=%v", page, gt, wt, len(got), len(want), err)
				}
				for i := range got {
					if !reflect.DeepEqual(got[i], want[i]) {
						t.Fatalf("page=%d row=%d got=%+v want=%+v", page, i, got[i], want[i])
					}
				}
			}
		})
	}
}
