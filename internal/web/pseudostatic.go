// SPDX-License-Identifier: AGPL-3.0-or-later
// 伪静态 URL：与与经典论坛的使用习惯一致
//   版块页  forum-{fid}-{page}.html          如 forum-2-1.html
//   帖子页  thread-{tid}-{page}-{prev}.html  如 thread-68845-1-1.html
// 生成与解析都在这里，全站唯一出口。
package web

import (
	"fmt"
	"regexp"
	"strconv"
)

var (
	forumRe  = regexp.MustCompile(`^/forum-(\d+)-(\d+)\.html$`)
	threadRe = regexp.MustCompile(`^/thread-(\d+)-(\d+)(?:-(\d+))?\.html$`)
)

// ForumURL 生成版块页伪静态地址。
func ForumURL(fid int64, page int) string {
	if page <= 1 {
		page = 1
	}
	return fmt.Sprintf("/forum-%d-%d.html", fid, page)
}

// ThreadURL 生成帖子页伪静态地址（第三段与旧版一致，固定为 1）。
func ThreadURL(tid int64, page int) string {
	if page <= 1 {
		page = 1
	}
	return fmt.Sprintf("/thread-%d-%d-1.html", tid, page)
}

// UserURL 用户空间。
func UserURL(uid int64) string { return fmt.Sprintf("/user/%d", uid) }

type pseudostaticTarget struct {
	kind string // "forum" | "thread"
	id   int64
	page int
}

// parsePseudostatic 解析伪静态路径；非伪静态路径返回 ok=false。
func parsePseudostatic(path string) (pseudostaticTarget, bool) {
	if m := forumRe.FindStringSubmatch(path); m != nil {
		return pseudostaticTarget{
			kind: "forum",
			id:   mustInt(m[1]),
			page: int(mustInt(m[2])),
		}, true
	}
	if m := threadRe.FindStringSubmatch(path); m != nil {
		return pseudostaticTarget{
			kind: "thread",
			id:   mustInt(m[1]),
			page: int(mustInt(m[2])),
		}, true
	}
	return pseudostaticTarget{}, false
}

func mustInt(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// clampPage 修正非法页码。
func clampPage(p int) int {
	if p < 1 {
		return 1
	}
	if p > 10000 {
		return 10000
	}
	return p
}
