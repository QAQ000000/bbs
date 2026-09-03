// SPDX-License-Identifier: AGPL-3.0-or-later
// seed.go：演示数据灌入（-seed；仅库为空时执行）。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"dzforum/internal/markdown"
	"dzforum/internal/store"
)

// seed 灌入演示数据（仅在库为空时执行）。
func seed(ctx context.Context, pool *pgxpool.Pool, st *store.Store) error {
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		slog.Info("数据库已有数据，跳过演示数据灌入")
		return nil
	}
	slog.Info("开始灌入演示数据…")

	// ---- 用户 ----
	type demoUser struct {
		name, pass string
		admin      bool
	}
	users := []demoUser{
		{"admin", "admin123456", true},
		{"码农老张", "demo123456", false},
		{"前端小美", "demo123456", false},
		{"DBA老王", "demo123456", false},
		{"潜水员", "demo123456", false},
	}
	ids := make(map[string]int64)
	for _, u := range users {
		created, err := st.CreateUser(ctx, u.name, u.pass, "")
		if err != nil {
			return fmt.Errorf("创建用户 %s: %w", u.name, err)
		}
		if u.admin {
			if _, err := pool.Exec(ctx, `UPDATE users SET group_id=1 WHERE id=$1`, created.ID); err != nil {
				return err
			}
		}
		ids[u.name] = created.ID
	}

	// ---- 分类与版块 ----
	type demoForum struct{ name, desc string }
	cats := []struct {
		name   string
		forums []demoForum
	}{
		{"站务管理", []demoForum{
			{"站务公告", "论坛公告、规则与重要通知"},
			{"意见反馈", "对论坛的建议与问题反馈"},
		}},
		{"技术交流", []demoForum{
			{"Go 语言", "Go 语言开发、性能优化与工程实践"},
			{"PostgreSQL", "数据库设计、SQL 与性能调优"},
			{"前端开发", "HTML / CSS / JavaScript 与框架"},
		}},
		{"休闲娱乐", []demoForum{
			{"灌水乐园", "轻松一刻，畅所欲言 :)"},
		}},
	}
	fids := make(map[string]int64)
	for _, c := range cats {
		var cid int
		if err := pool.QueryRow(ctx,
			`INSERT INTO categories (name) VALUES ($1) RETURNING id`, c.name).Scan(&cid); err != nil {
			return err
		}
		for i, f := range c.forums {
			var fid int64
			if err := pool.QueryRow(ctx,
				`INSERT INTO forums (category_id, name, description, displayorder) VALUES ($1,$2,$3,$4) RETURNING id`,
				cid, f.name, f.desc, i).Scan(&fid); err != nil {
				return err
			}
			fids[f.name] = fid
		}
	}

	// ---- 主题与回复 ----
	type reply struct {
		user  string
		md    string
		after time.Duration // 相对主题创建时间的偏移
	}
	type thread struct {
		forum, author, title, md string
		sticky                   int
		digest                   bool
		created                  time.Duration
		replies                  []reply
	}

	threads := []thread{
		{
			forum: "站务公告", author: "admin", title: "欢迎使用 GoBBS —— 新版社区上线公告",
			sticky: 1, created: 21 * 24 * time.Hour,
			md: "各位老友新朋：\n\n全新的 **GoBBS** 社区今天正式上线！本站使用 Go 语言从零重写，与旧版相比：\n\n- 发帖全面采用 **Markdown** 语法，支持表格、代码块、删除线等\n- 帖子页面与版块页面支持**实时局部刷新**，别人编辑了内容你能立刻看到\n- 保留经典版块排版，沿用熟悉的伪静态地址（`forum-2-1.html`、`thread-68845-1-1.html`）\n- 原有表情素材完整迁移，发帖时点击表情面板即可插入\n\n发个帖试试 Markdown 效果：\n\n```go\npackage main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello, GoBBS!\")\n}\n```\n\n祝大家玩得开心 :)",
			replies: []reply{
				{"前端小美", "新界面很好看！表情面板也找回来了 :lol", 2 * time.Hour},
				{"码农老张", "Markdown 发帖舒服多了，代码块好评。\n\n| 对比项 | 旧版 | 新版 |\n| --- | --- | --- |\n| 编辑器 | BBCode | Markdown |\n| 刷新 | 手动 | 实时推送 |", 5 * time.Hour},
			},
		},
		{
			forum: "站务公告", author: "admin", title: "社区行为规范（发帖前必读）",
			sticky: 2, created: 21*24*time.Hour + 1*time.Hour,
			md: "为维护社区氛围，请遵守以下规范：\n\n1. 禁止发布违法违规内容\n2. 尊重他人，理性讨论，不人身攻击\n3. 技术提问请描述清楚上下文与报错信息\n4. 广告与灌水内容请发到「灌水乐园」\n\n违规内容将被管理员删除，多次违规会被禁言。",
		},
		{
			forum: "Go 语言", author: "码农老张", title: "Go 1.25 实战：用泛型重构了 800 行重复代码",
			digest: true, created: 9 * 24 * time.Hour,
			md: "接手了一个老项目，里面把同一套逻辑为 `int64`、`float64`、`string` 复制了三遍。用泛型重构后：\n\n## 重构思路\n\n1. 先定义类型约束\n2. 抽取公共逻辑\n3. 逐个替换调用点\n\n```go\ntype Number interface {\n\t~int64 | ~float64\n}\n\nfunc Sum[T Number](xs []T) T {\n\tvar total T\n\tfor _, x := range xs {\n\t\ttotal += x\n\t}\n\treturn total\n}\n```\n\n重构后单测全绿，代码量少了 800 行，再也没有\"改了一处忘改另一处\"的事故了 :D",
			replies: []reply{
				{"DBA老王", "泛型约束写起来还是有点绕，不过确实能省很多代码。", 3 * time.Hour},
				{"码农老张", "分享一个心得：先定义好类型约束，再抽公共逻辑，一步步来不容易翻车。", 1 * time.Hour},
			},
		},
		{
			forum: "PostgreSQL", author: "DBA老王", title: "PostgreSQL 18 升级踩坑记录与性能对比",
			created: 6 * 24 * time.Hour,
			md: "把生产库从 15 升到 **PostgreSQL 18**，记录几个要点：\n\n### 升级步骤\n\n1. `pg_upgrade --check` 预检\n2. 停写窗口内原地升级\n3. `ANALYZE` 刷新统计信息\n\n### 性能对比\n\n| 场景 | PG15 | PG18 |\n| --- | --- | --- |\n| 大表顺序扫描 | 4.2s | 1.9s |\n| 复杂聚合 | 880ms | 520ms |\n\n异步 I/O 的提升比预期还大，读密集业务直接起飞 :D",
			replies: []reply{
				{"码农老张", "异步 I/O 那块提升确实明显，我们读密集场景快了 30%。", 8 * time.Hour},
				{"潜水员", "收藏了，正好下周要升级。", 26 * time.Hour},
			},
		},
		{
			forum: "Go 语言", author: "前端小美", title: "请教：SSE 和 WebSocket 该怎么选？",
			created: 3 * 24 * time.Hour,
			md: "想给管理后台加实时通知，查了一下有两派方案：\n\n- **SSE**：单向推送，HTTP 协议，自带断线重连\n- **WebSocket**：双向，需要额外的心跳与重连逻辑\n\n我的场景是纯服务端推送，是不是 SSE 就够了？大家怎么选的？",
			replies: []reply{
				{"码农老张", "服务端单向推送选 SSE 就够了，自带断线重连；双向交互才需要 WebSocket。", 40 * time.Minute},
				{"DBA老王", "本站的实时刷新就是用 SSE 实现的，开两个窗口试试就知道效果了 :)", 20 * time.Minute},
			},
		},
		{
			forum: "前端开发", author: "前端小美", title: "原生 JS 写了个轻量 Markdown 编辑器，求拍砖",
			created: 30 * time.Hour,
			md: "不想引入重型依赖，用原生 JS 实现了论坛的发帖编辑器：\n\n- 工具栏：加粗 / 斜体 / 引用 / 代码块 / 链接 / 图片\n- 表情面板：复用老社区的表情素材\n- 实时预览：服务端统一渲染，保证所见即所得\n- 草稿：自动保存到 `localStorage`\n\n代码不到 200 行，有兴趣的可以看看本站的发帖页 :P",
			replies: []reply{
				{"潜水员", "表情面板好评，QQ 表情包爷青回 :weixiao:", 4 * time.Hour},
			},
		},
		{
			forum: "灌水乐园", author: "潜水员", title: "今天你摸鱼了吗？（每日打卡）",
			created: 10 * time.Hour,
			md: "如题，评论区打卡 :)",
			replies: []reply{
				{"前端小美", "摸了，顺手把表情包全试了一遍 :kiss:", 2 * time.Hour},
				{"码农老张", "楼里全是表情，哈哈哈哈 :lol", 30 * time.Minute},
			},
		},
	}

	threadsAdded, postsAdded := 0, 0
	for _, t := range threads {
		authorID := ids[t.author]
		html := markdown.Render(t.md)
		th, p, err := st.CreateThread(ctx, fids[t.forum], authorID, t.author, t.title, t.md, html, false, "")
		if err != nil {
			return fmt.Errorf("创建主题 %s: %w", t.title, err)
		}
		threadsAdded++
		postsAdded++
		if t.sticky > 0 || t.digest {
			digest, closed := t.digest, false
			if err := st.SetThreadProperties(ctx, th.ID, t.sticky, &digest, &closed); err != nil {
				return err
			}
		}
		if _, err := pool.Exec(ctx,
			`UPDATE threads SET created_at = now() - $2::interval, last_post_at = now() - $2::interval WHERE id=$1`,
			th.ID, interval(t.created)); err != nil {
			return err
		}
		if _, err := pool.Exec(ctx,
			`UPDATE posts SET created_at = now() - $2::interval WHERE id=$1`, p.ID, interval(t.created)); err != nil {
			return err
		}
		for _, rp := range t.replies {
			html := markdown.Render(rp.md)
			rth, rp2, err := st.CreateReply(ctx, th.ID, ids[rp.user], rp.user, rp.md, html, false, "")
			if err != nil {
				return fmt.Errorf("回复主题 %s: %w", t.title, err)
			}
			postsAdded++
			if _, err := pool.Exec(ctx,
				`UPDATE posts SET created_at = now() - $2::interval WHERE id=$1`, rp2.ID, interval(rp.after)); err != nil {
				return err
			}
			if _, err := pool.Exec(ctx,
				`UPDATE threads SET last_post_at = (SELECT max(created_at) FROM posts WHERE thread_id=$1) WHERE id=$1`,
				rth.ID); err != nil {
				return err
			}
		}
		// 同步主题的最后发表人与用户发帖统计
		if _, err := pool.Exec(ctx, `
			UPDATE threads t SET last_post_uid = p.author_id, last_post_at = p.created_at
			FROM posts p
			WHERE p.thread_id = t.id AND p.floor = (SELECT max(floor) FROM posts WHERE thread_id = t.id AND NOT deleted)
			  AND t.id = $1`, th.ID); err != nil {
			return err
		}
	}
	if _, err := pool.Exec(ctx, `
		UPDATE users u SET post_count = (SELECT count(*) FROM posts p WHERE p.author_id = u.id AND NOT p.deleted)`); err != nil {
		return err
	}
	// 用户档案真实感：注册时间错开在过去、最近登录、老用户为正式成员（TL1）
	// 管理员为资深成员（TL2）
	stamps := map[string]struct {
		daysAgo int
		tl      int
	}{
		"admin":    {60, 2},
		"码农老张":  {45, 1},
		"前端小美":  {38, 1},
		"DBA老王":   {30, 1},
		"潜水员":    {20, 1},
	}
	for name, st2 := range stamps {
		if _, err := pool.Exec(ctx, `
			UPDATE users SET created_at = now() - make_interval(days => $2),
				last_login_at = now() - interval '2 hours',
				trust_level = $3
			WHERE username = $1`, name, st2.daysAgo, st2.tl); err != nil {
			return err
		}
	}
	slog.Info("演示数据灌入完成", "threads", threadsAdded, "posts", postsAdded, "users", len(users))
	return nil
}

// interval 把时长转成 PostgreSQL interval 字面量。
func interval(d time.Duration) string {
	return fmt.Sprintf("%.0f seconds", d.Seconds())
}
