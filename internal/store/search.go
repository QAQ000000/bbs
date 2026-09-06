// SPDX-License-Identifier: AGPL-3.0-or-later
// store/search.go：tsvector 全文搜索（中文 bigram 分词）。
package store

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
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
	if err != nil {
		slog.Warn("搜索索引更新失败（启动时会自动补齐）", "post", postID, "err", err)
	}
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
	CreatedAt  time.Time
	Excerpt    string
	Rank       float64
}

// mdLinkRe 摘要剥离：[文本](链接) → 文本（含图片前缀 !）。
var mdLinkRe = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)

// mdMarksRe 摘要剥离：标题井号、围栏代码、强调记号、引用符。
var mdMarksRe = regexp.MustCompile("(^|\\n)[ \\t]*#{1,6} |```|[*_`~>|]")

func stripMarkdown(s string) string {
	s = mdLinkRe.ReplaceAllString(s, "$1")
	s = mdMarksRe.ReplaceAllString(s, "$1")
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

// buildExcerpt 返回命中位置附近的纯文本摘要；高亮由前端决定。
func buildExcerpt(md string, tokens []string) string {
	plain := stripMarkdown(md)
	runes := []rune(plain)
	lower := strings.ToLower(plain)

	pos := -1
	for _, tk := range tokens {
		if tk == "" {
			continue
		}
		if i := strings.Index(lower, tk); i >= 0 {
			r := len([]rune(lower[:i]))
			if pos < 0 || r < pos {
				pos = r
			}
		}
	}
	const window = 70
	start, end := 0, len(runes)
	if pos >= 0 {
		start = pos - 30
		if start < 0 {
			start = 0
		}
		end = pos + window
		if end > len(runes) {
			end = len(runes)
		}
	} else {
		if end > 120 {
			end = 120
		}
	}
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}

// Search 全文搜索：命中楼层聚合到主题，按 ts_rank 降序分页。
// SearchOpts 搜索过滤条件（零值 = 全站）。
type SearchOpts struct {
	ForumID int64  // 限定版块
	Author  string // 限定作者用户名（精确）
}

func (s *Store) Search(ctx context.Context, q string, page, size int, opts SearchOpts) ([]*SearchHit, int, error) {
	tq := SearchQueryTokens(q)
	if tq == "" {
		return nil, 0, nil
	}
	tokens := strings.Fields(tq)
	offset := (page - 1) * size
	// 取足够多的命中做主题级去重分页（小型论坛数据量下足够）
	// 摘要不用 ts_headline：simple 配置对原文分词为单字，与 bigram 词素不匹配，永远无法高亮
	rows, err := s.pool.Query(ctx,
		`SELECT t.id, t.title, t.forum_id, f.name, u.username, t.created_at,
			left(p.content_md, 800),
			ts_rank(p.search_data, q) AS score
		 FROM posts p
		 JOIN threads t ON t.id = p.thread_id AND NOT t.deleted AND NOT t.pending
		 JOIN users u ON u.id = t.author_id
		 JOIN forums f ON f.id = t.forum_id
		 CROSS JOIN (SELECT to_tsquery('simple', $1) AS q) qq
		 WHERE p.search_data @@ q AND NOT p.deleted AND NOT p.pending
		   AND ($2::bigint = 0 OR t.forum_id = $2)
		   AND ($3::text = '' OR u.username = $3)`+forumFilter(ctx, "t.forum_id")+`
		 ORDER BY score DESC LIMIT 400`, tq, opts.ForumID, opts.Author)
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
