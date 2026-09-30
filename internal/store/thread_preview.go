package store

import (
	"context"
	"regexp"
	"strings"
)

// ThreadPreview 列表用的摘要与封面投影（纯文本、长度受控）。
type ThreadPreview struct {
	Excerpt  string `json:"excerpt"`
	CoverURL string `json:"coverUrl"`
}

// mdImageRe 匹配 Markdown 图片，用于挑选站内封面。
var mdImageRe = regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)\)`)

var coverExtRe = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp)(\?|$)`)

// PlainExcerpt 复用搜索的 Markdown 剥离，再按 rune 截断成长度受控的纯文本。
func PlainExcerpt(markdown string, limit int) string {
	text := stripMarkdown(markdown)
	if limit > 0 {
		runes := []rune(text)
		if len(runes) > limit {
			text = strings.TrimSpace(string(runes[:limit])) + "…"
		}
	}
	return text
}

// FirstCoverURL 返回正文中第一张站内 /uploads/ 图片地址。
// 只接受本站受控媒体，避免把外链当作封面泄露访问者信息。
func FirstCoverURL(markdown string) string {
	for _, m := range mdImageRe.FindAllStringSubmatch(markdown, -1) {
		url := strings.TrimSpace(m[1])
		if strings.HasPrefix(url, "/uploads/") && coverExtRe.MatchString(url) {
			return url
		}
	}
	return ""
}

// ThreadPreviews 批量投影列表摘要与封面：一次查询取每页可见首楼的正文前缀，
// 在 SQL 内过滤主题 / 楼层的删除与待审，天然满足正文权限且无缓存过期问题。
func (s *Store) ThreadPreviews(ctx context.Context, tids []int64) (map[int64]ThreadPreview, error) {
	out := map[int64]ThreadPreview{}
	if len(tids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT t.id, left(p.content_md, 2000) FROM threads t JOIN posts p ON p.id=t.first_post_id
 WHERE t.id=ANY($1) AND NOT t.deleted AND NOT t.pending AND NOT p.deleted AND NOT p.pending`, tids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var body string
		if err := rows.Scan(&id, &body); err != nil {
			return nil, err
		}
		out[id] = ThreadPreview{Excerpt: PlainExcerpt(body, 160), CoverURL: FirstCoverURL(body)}
	}
	return out, rows.Err()
}
