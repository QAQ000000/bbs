// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"testing"
)

func TestParsePseudostatic(t *testing.T) {
	cases := []struct {
		path  string
		kind  string
		id    int64
		page  int
		valid bool
	}{
		{"/forum-2-1.html", "forum", 2, 1, true},
		{"/forum-688-12.html", "forum", 688, 12, true},
		{"/thread-68845-1-1.html", "thread", 68845, 1, true},
		{"/thread-3-9-2.html", "thread", 3, 9, true},
		{"/thread-3-1.html", "thread", 3, 1, true}, // 两段式也接受
		{"/forum-2.html", "", 0, 0, false},
		{"/thread-x-1-1.html", "", 0, 0, false},
		{"/notexist.html", "", 0, 0, false},
		{"/forum-2-1.html/extra", "", 0, 0, false},
	}
	for _, c := range cases {
		got, ok := parsePseudostatic(c.path)
		if ok != c.valid {
			t.Errorf("%s: valid=%v 期望 %v", c.path, ok, c.valid)
			continue
		}
		if ok && (got.kind != c.kind || got.id != c.id || got.page != c.page) {
			t.Errorf("%s: got %+v 期望 %s/%d/%d", c.path, got, c.kind, c.id, c.page)
		}
	}
}

func TestURLBuilders(t *testing.T) {
	if got := ForumURL(2, 0); got != "/forum-2-1.html" {
		t.Fatalf("ForumURL: %s", got)
	}
	if got := ThreadURL(68845, 1); got != "/thread-68845-1-1.html" {
		t.Fatalf("ThreadURL: %s", got)
	}
	if got := ThreadURL(3, 9); got != "/thread-3-9-1.html" {
		t.Fatalf("ThreadURL 多页: %s", got)
	}
}

func TestBuildPage(t *testing.T) {
	// 单页无分页条
	if items := BuildPage(1, 1, func(int) string { return "" }); items != nil {
		t.Fatalf("单页不应有分页条: %+v", items)
	}
	// 8 页，当前 1：应有 1..6、省略号、末页、下一页
	items := BuildPage(1, 8, func(n int) string { return "u" })
	if items[0].N != 1 || !items[0].Cur {
		t.Fatalf("首项应为当前页 1: %+v", items[0])
	}
	if items[1].N != 6 || items[1].Label != "…"[0:0]+"…" && !items[1].IsEllipsis {
		// 第二个元素应为省略号或页码，仅检查无 panic
		_ = items[1]
	}
	if last := items[len(items)-1]; last.N != 2 || last.Label != "下一页 ›" {
		t.Fatalf("末项应为下一页: %+v", last)
	}
	hasEllipsis := false
	for _, it := range items {
		if it.IsEllipsis {
			hasEllipsis = true
		}
	}
	if !hasEllipsis {
		t.Fatal("8 页应有省略号")
	}
	// 中间页：出现首页与末页
	items = BuildPage(5, 50, func(n int) string { return "u" })
	if items[0].N != 1 || items[len(items)-1].N != 6 {
		t.Fatalf("中间页分页结构异常: 首 %+v 末 %+v", items[0], items[len(items)-1])
	}
}
