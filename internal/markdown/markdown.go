// SPDX-License-Identifier: AGPL-3.0-or-later
// Package markdown 基于 goldmark 的渲染器：
//   - GFM 扩展（表格/删除线/自动链接/任务列表）
//   - 软换行渲染为 <br>，贴合论坛发帖习惯
//   - 自定义表情内联扩展：短代码表情（如 :lol、:weixiao:、{:3_41:}）渲染为图片
//
// 原始 HTML 默认不输出（goldmark 安全默认值），杜绝存储型 XSS。
package markdown

import (
	stdhtml "html"
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"dzforum/internal/smiley"
)

// ---- 表情内联扩展 ----

type smileyExt struct{}

func (e *smileyExt) Extend(m goldmark.Markdown) {
	// 优先级 150：先于链接(200)/自动链接(299)执行，避免 URL 里的冒号被误解析；
	// 晚于代码段(100)，`code` 内的表情保持字面量。
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(&smileyInlineParser{}, 150),
	))
}

type smileyInlineParser struct{}

func (p *smileyInlineParser) Trigger() []byte { return []byte(":{") }

func (p *smileyInlineParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) == 0 {
		return nil
	}
	code, n, ok := smiley.Match(util.BytesToReadOnlyString(line))
	if !ok {
		return nil
	}
	block.Advance(n)

	// 内置 emoji：直接输出 Unicode 字符本身（零图片资产）
	if code.Unicode != "" {
		txt := ast.NewString([]byte(code.Unicode))
		txt.SetRaw(true)
		return txt
	}
	link := ast.NewLink()
	link.Destination = []byte(smiley.URL(code))
	link.Title = []byte(code.Code)
	img := ast.NewImage(link)
	img.SetAttributeString("class", []byte("smiley"))
	img.AppendChild(img, ast.NewString([]byte(code.Code)))
	return img
}

// ---- 引擎 ----

var engine = goldmark.New(
	goldmark.WithExtensions(extension.GFM, &smileyExt{}),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(html.WithHardWraps()),
)

// Render 把 Markdown 源文渲染为安全 HTML。
func Render(src string) string {
	var buf bytes.Buffer
	if err := engine.Convert([]byte(src), &buf); err != nil {
		return "<p>" + stdhtml.EscapeString(src) + "</p>"
	}
	return buf.String()
}
