// SPDX-License-Identifier: AGPL-3.0-or-later

package markdown

import (
	"strings"
	"testing"
)

func TestRenderBasic(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"标题", "# 你好", "<h1 id="},
		{"加粗", "**重要**", "<strong>重要</strong>"},
		{"代码块", "```go\nx := 1\n```", "<pre><code class=\"language-go\">"},
		{"删除线", "~~旧信息~~", "<del>旧信息</del>"},
		{"链接", "[点我](https://example.com)", `<a href="https://example.com"`},
		{"表格", "| a | b |\n| --- | --- |\n| 1 | 2 |", "<table>"},
		{"软换行转br", "第一行\n第二行", "第一行<br>\n第二行"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Render(c.in)
			if !strings.Contains(got, c.want) {
				t.Fatalf("渲染结果缺少 %q:\n%s", c.want, got)
			}
		})
	}
}

func TestRenderSmiley(t *testing.T) {
	if got := Render("开心 :smile:"); !strings.Contains(got, "😄") {
		t.Fatalf("内置 emoji 应渲染为字符: %s", got)
	}
	if got := Render("不存在 :notacode:"); strings.Contains(got, "notacode</") {
		t.Fatalf("未知代码应保持字面量: %s", got)
	}
	// 代码段内不做表情替换
	if got := Render("`代码 :smile: 原样`"); strings.Contains(got, "😄") {
		t.Fatalf("code span 内不应替换表情: %s", got)
	}
}

func TestRenderXSSSafe(t *testing.T) {
	got := Render(`<script>alert(1)</script><img src=x onerror=alert(1)>`)
	if strings.Contains(got, "<script>") || strings.Contains(got, "onerror") {
		t.Fatalf("原始 HTML 必须不被输出: %s", got)
	}
}
