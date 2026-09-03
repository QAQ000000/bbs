// SPDX-License-Identifier: AGPL-3.0-or-later

// 列清单同步测试：校验 store 中的手写 SELECT 列清单（userCols/forumCols/
// threadCols/postCols）与数据库实际列一致。防止“新增列后忘改列清单或 Scan”
// 的回归（历史上发生过两次）。

package store

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// splitTopLevel 按顶层逗号切分列清单（忽略括号内的逗号）。
func splitTopLevel(list string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range list {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, list[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, list[start:])
	return out
}

var identRe = regexp.MustCompile(`[a-zA-Z_][a-zA-Z0-9_]*`)

// sqlKeywords 出现在表达式里但不属于列名的词。
var sqlKeywords = map[string]bool{
	"as": true, "and": true, "or": true, "not": true, "is": true, "null": true,
	"coalesce": true, "count": true, "epoch": true, "timestamptz": true,
	"greatest": true, "now": true, "current_date": true, "true": true, "false": true,
}

// columnIdents 从单个列表达式中提取候选列名：
// 去掉表别名前缀（t./p./u./lu.）、coalesce 外壳、类型转换与 IS NOT NULL 尾巴。
func columnIdents(expr string) []string {
	expr = strings.TrimSpace(strings.ToLower(expr))
	// 剥离表别名前缀（t./p./u./lu.）
	for _, alias := range []string{"t.", "p.", "u.", "lu."} {
		if strings.HasPrefix(expr, alias) {
			expr = expr[len(alias):]
		}
	}
	// 去掉 ::类型转换
	if i := strings.Index(expr, "::"); i >= 0 {
		expr = expr[:i]
	}
	// coalesce(a, b) 保留第一个参数（列）
	if strings.HasPrefix(strings.TrimSpace(strings.ToLower(expr)), "coalesce(") {
		if i := strings.LastIndex(expr, ","); i > 0 {
			expr = expr[:i]
		}
	}
	var idents []string
	for _, m := range identRe.FindAllString(strings.ToLower(expr), -1) {
		if sqlKeywords[m] {
			continue
		}
		idents = append(idents, m)
	}
	return idents
}

// TestColumnParser 解析器纯逻辑单测（捏造列名的事故形态可被提取识别）。
func TestColumnParser(t *testing.T) {
	list := `id, coalesce(last_post_at, 'epoch'::timestamptz), last_post_at IS NOT NULL, coalesce(moderators,'')`
	parts := splitTopLevel(list)
	if len(parts) != 4 {
		t.Fatalf("应切出 4 段: %q", parts)
	}
	idents := columnIdents("coalesce(last_post_at, 'epoch'::timestamptz)")
	if len(idents) != 1 || idents[0] != "last_post_at" {
		t.Fatalf("coalesce 表达式应只提取列名: %q", idents)
	}
	idents = columnIdents("t.pending_reason")
	if len(idents) != 1 || idents[0] != "pending_reason" {
		t.Fatalf("带别名前缀应剥离: %q", idents)
	}
	idents = columnIdents("last_post_at IS NOT NULL")
	if len(idents) != 1 || idents[0] != "last_post_at" {
		t.Fatalf("IS NOT NULL 尾巴应剥离: %q", idents)
	}
	idents = columnIdents("t.not_exist_col")
	if len(idents) != 1 || idents[0] != "not_exist_col" {
		t.Fatalf("捏造列应被原样提取: %q", idents)
	}
}

// TestColumnListsMatchSchema 与 information_schema 逐列比对。
func TestColumnListsMatchSchema(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		list   string
		tables []string // 表达式里的列必须至少存在于其中一张表
	}{
		{"users", userCols, []string{"users"}},
		{"forums", forumCols, []string{"forums"}},
		{"threads", threadCols, []string{"threads", "users"}},
		{"posts", postCols, []string{"posts", "users"}},
	}

	actual := map[string]map[string]bool{}
	for _, tb := range []string{"users", "forums", "threads", "posts"} {
		rows, err := testPool.Query(ctx,
			`SELECT column_name FROM information_schema.columns WHERE table_name=$1`, tb)
		if err != nil {
			t.Fatalf("读取 %s 列失败: %v", tb, err)
		}
		cols := map[string]bool{}
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatal(err)
			}
			cols[c] = true
		}
		rows.Close()
		actual[tb] = cols
	}

	for _, c := range cases {
		for _, expr := range splitTopLevel(c.list) {
			expr = strings.TrimSpace(expr)
			if expr == "" {
				continue
			}
			ok := false
			for _, name := range columnIdents(expr) {
				for _, tb := range c.tables {
					if actual[tb][name] {
						ok = true
					}
				}
			}
			if !ok {
				t.Errorf("%s 列清单中的表达式 %q 找不到对应的数据库列", c.name, expr)
			}
		}
	}
}
