// SPDX-License-Identifier: AGPL-3.0-or-later
package store

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode"
)

// ---- 全文搜索（PostgreSQL tsvector + 中文 bigram 分词，侧表存储）----

// SearchTokens 把文本切分为可被 to_tsvector('simple', ...) 正确处理的词序列：
// 拉丁词原样保留，中文连续段输出全部二元组（末尾落单输出单字）。
func SearchTokens(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	var run []rune  // 中文连续段
	var word []rune // 拉丁/数字连续段
	flushHan := func() {
		for i := 0; i+1 < len(run); i++ { // 多字段只输出 bigram（末尾单字不输出，避免查询端 AND 不中）
			b.WriteRune(run[i])
			b.WriteRune(run[i+1])
			b.WriteByte(' ')
		}
		if len(run) == 1 {
			b.WriteRune(run[0])
			b.WriteByte(' ')
		}
		run = run[:0]
	}
	flushWord := func() {
		for _, r := range word {
			b.WriteRune(r)
		}
		b.WriteByte(' ')
		word = word[:0]
	}
	for _, r := range s {
		if isHan(r) {
			flushWord()
			run = append(run, r)
			continue
		}
		flushHan()
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			word = append(word, r)
			continue
		}
		flushWord()
	}
	flushHan()
	flushWord()
	return b.String()
}

func isHan(r rune) bool {
	return unicode.Is(unicode.Han, r)
}

// SearchQueryTokens 把用户查询转为 tsquery 字面量（多词 AND）。
func SearchQueryTokens(q string) string {
	var parts []string
	for _, f := range strings.Fields(SearchTokens(q)) {
		parts = append(parts, f)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " & ")
}

// IndexPost 写入/更新楼层的搜索向量：首楼包含标题（权重 A），正文权重 B。
// 需在写帖子的事务外或同事务内执行。
func (s *Store) IndexPost(ctx context.Context, postID int64, title, body string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE posts SET search_data =
			setweight(to_tsvector('simple', $2), 'A') ||
			setweight(to_tsvector('simple', $3), 'B')
		 WHERE id=$1`, postID, SearchTokens(title), SearchTokens(body))
	return err
}

// ReindexSearch 全量重建搜索索引（渲染器/分词器升级或存量数据补齐时调用）。
func (s *Store) ReindexSearch(ctx context.Context) (int64, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT p.id, t.title, p.content_md FROM posts p
		 JOIN threads t ON t.id = p.thread_id
		 WHERE p.search_data IS NULL AND NOT p.deleted`)
	if err != nil {
		return 0, err
	}
	type item struct {
		id    int64
		title string
		body  string
	}
	var batch []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.title, &it.body); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	var n int64
	for _, it := range batch {
		if err := s.IndexPost(ctx, it.id, it.title, it.body); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// SearchHit 搜索结果条目（按主题去重后的最佳命中）。
type SearchHit struct {
	ThreadID   int64
	Title      string
	ForumID    int64
	ForumName  string
	AuthorName string
	CreatedAt  string
	Excerpt    string
	Rank       float64
}

// Search 全文搜索：命中楼层聚合到主题，按 ts_rank 降序分页。
func (s *Store) Search(ctx context.Context, q string, page, size int) ([]*SearchHit, int, error) {
	tq := SearchQueryTokens(q)
	if tq == "" {
		return nil, 0, nil
	}
	offset := (page - 1) * size
	// 取足够多的命中做主题级去重分页（小型论坛数据量下足够）
	rows, err := s.pool.Query(ctx,
		`SELECT t.id, t.title, t.forum_id, f.name, u.username, t.created_at::text,
			ts_headline('simple', p.content_md, q, 'MaxWords=30, MinWords=12, StartSel=<mark>, StopSel=</mark>, MaxFragments=1'),
			ts_rank(p.search_data, q) AS score
		 FROM posts p
		 JOIN threads t ON t.id = p.thread_id AND NOT t.deleted AND NOT t.pending
		 JOIN users u ON u.id = t.author_id
		 JOIN forums f ON f.id = t.forum_id
		 CROSS JOIN (SELECT to_tsquery('simple', $1) AS q) qq
		 WHERE p.search_data @@ q AND NOT p.deleted AND NOT p.pending
		 ORDER BY score DESC LIMIT 400`, tq)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	seen := map[int64]bool{}
	var hits []*SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ThreadID, &h.Title, &h.ForumID, &h.ForumName, &h.AuthorName,
			&h.CreatedAt, &h.Excerpt, &h.Rank); err != nil {
			return nil, 0, err
		}
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

// ---- 阅读追踪与信任等级 ----

// TL1 升级门槛。
const (
	tl1DaysVisited = 3
	tl1PostsRead   = 20
)

var (
	visitMu    sync.Mutex
	visitBumped = map[int64]string{} // uid -> 已打过点的日期（内存去重，省每请求一次 DB）
)

// TouchVisit 记录当日访问（每天每用户只落库一次；已升级判断由调用方按需触发）。
func (s *Store) TouchVisit(ctx context.Context, uid int64) {
	today := todayString()
	visitMu.Lock()
	if visitBumped[uid] == today {
		visitMu.Unlock()
		return
	}
	visitBumped[uid] = today
	if len(visitBumped) > 10000 {
		visitBumped = map[int64]string{}
	}
	visitMu.Unlock()

	_, err := s.pool.Exec(ctx, `
		UPDATE users SET
			days_visited = days_visited + CASE WHEN last_visit_date = current_date THEN 0 ELSE 1 END,
			last_visit_date = current_date
		WHERE id=$1`, uid)
	_ = err
}

// RecordRead 阅读打点：记录该用户读到主题的第几楼，返回新增阅读楼层数。
func (s *Store) RecordRead(ctx context.Context, uid, tid int64, maxFloor int) int {
	if maxFloor <= 0 {
		return 0
	}
	// 先读旧进度再 upsert（INSERT 分支的 RETURNING 无法引用“旧值”）
	var prev int
	_ = s.pool.QueryRow(ctx,
		`SELECT last_floor FROM thread_reads WHERE user_id=$1 AND thread_id=$2`, uid, tid).Scan(&prev)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO thread_reads (user_id, thread_id, last_floor, updated_at)
		VALUES ($1,$2,$3,now())
		ON CONFLICT (user_id, thread_id) DO UPDATE SET
			last_floor = GREATEST(thread_reads.last_floor, EXCLUDED.last_floor),
			updated_at = now()`, uid, tid, maxFloor); err != nil {
		return 0
	}
	added := maxFloor - prev
	if added < 0 {
		added = 0
	}
	if added > 0 {
		_, _ = s.pool.Exec(ctx,
			`UPDATE users SET posts_read = posts_read + $2 WHERE id=$1`, uid, added)
	}
	return added
}

// MaybeUpgradeTrust 信任等级自动升级（0→1：访问天数与读帖数达标）。
func (s *Store) MaybeUpgradeTrust(ctx context.Context, uid int64) {
	var cur int
	var days, reads int64
	err := s.pool.QueryRow(ctx,
		`SELECT trust_level, days_visited, posts_read FROM users WHERE id=$1`, uid).
		Scan(&cur, &days, &reads)
	if err != nil || cur != 0 {
		return
	}
	if days >= tl1DaysVisited && reads >= tl1PostsRead {
		_, _ = s.pool.Exec(ctx,
			`UPDATE users SET trust_level=1 WHERE id=$1 AND trust_level=0`, uid)
	}
}

// TrustLevelName 等级名称。
func TrustLevelName(t int) string {
	switch t {
	case 1:
		return "正式成员"
	case 2:
		return "资深成员"
	default:
		return "新用户"
	}
}

func todayString() string {
	return time.Now().Format("2006-01-02")
}
