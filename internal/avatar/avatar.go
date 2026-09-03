// SPDX-License-Identifier: AGPL-3.0-or-later
// Package avatar 生成确定性字母头像（内联 SVG）：
// 零外部请求、零磁盘 IO，颜色由用户 ID 哈希决定。
package avatar

import (
	"fmt"
	"strings"
)

var palette = []string{
	"#e65100", "#ad1457", "#6a1b9a", "#4527a0", "#1565c0",
	"#00838f", "#2e7d32", "#9e9d24", "#ef6c00", "#c62828",
	"#283593", "#00695c", "#4e342e", "#37474f", "#7b1fa2",
}

// HTML 返回用户名首字符头像的 <svg> 标记（模板里直接以 template.HTML 使用）。
func HTML(uid int64, username string) string {
	r := []rune(strings.TrimSpace(username))
	letter := "U"
	if len(r) > 0 {
		letter = strings.ToUpper(string(r[0]))
	}
	color := palette[int(uid)%len(palette)]
	return fmt.Sprintf(
		`<svg class="avatar" viewBox="0 0 48 48" width="48" height="48" role="img" aria-label="%s">`+
			`<rect width="48" height="48" rx="6" fill="%s"/><text x="24" y="31" text-anchor="middle" `+
			`font-family="system-ui,sans-serif" font-size="22" font-weight="600" fill="#fff">%s</text></svg>`,
		username, color, letter)
}

// Small 是列表页用的小号头像。
func Small(uid int64, username string) string {
	s := HTML(uid, username)
	return strings.Replace(s, `width="48" height="48"`, `width="24" height="24"`, 1)
}
