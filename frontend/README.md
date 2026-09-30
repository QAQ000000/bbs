# GoBBS 前端（Next.js App Router）

GoBBS 论坛前台与后台的独立前端。Go 后端只提供 JSON API、SSE、鉴权和受控媒体；本工程负责页面、SSR、交互与 SEO 输出。

技术基线：**Next.js 14.2（App Router）+ React 18 + TypeScript 5.9 + Arco Design React 2.66**，样式使用 CSS Modules + 统一主题变量。

## 运行环境

- Node.js >= 18.17（本机验证 24.16.0）
- pnpm 11（本工程提供 `pnpm-workspace.yaml` 与锁文件）
- 运行中的 Go API（默认 `http://127.0.0.1:8090`）

## 快速开始

```bash
cd frontend
pnpm install
cp .env.example .env        # 按需修改 API_INTERNAL_URL
pnpm dev                    # http://127.0.0.1:3000
```

生产构建：

```bash
pnpm lint
pnpm typecheck
pnpm build
pnpm start                  # 默认 3000 端口
```

## 环境变量

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `API_INTERNAL_URL` | `http://127.0.0.1:8090` | 仅服务端可见的内部 Go API 地址，供 SSR 取数；不要写数据库凭据 |
| `NEXT_PUBLIC_API_BASE` | `/api/v1` | 浏览器访问前缀；同域部署时保持默认 |
| `NEXT_PUBLIC_SITE_URL` | 空 | canonical / Open Graph 绝对地址（可选） |

浏览器请求经 `next.config.mjs` 的 rewrites 同域代理到 Go：`/api/v1/*`、`/uploads/*`、`/avatar/*`、`/captcha/*`、`/smiley/*`。SSR 请求使用固定内部地址并转发当前请求的 Cookie，第一版统一 `cache: 'no-store'`，不做共享整页缓存。

## 目录结构

```
src/
  app/
    (public)/     前台：首页、版块、主题、标签、搜索、用户、签到、排行榜、条款
    (auth)/       登录、注册、找回 / 重置密码
    (member)/     用户中心：主页、收藏、草稿、通知、订阅、关注、私信、安全、资料
    (admin)/      管理后台：概览、审核与举报、分类与版块
    layout.tsx    根布局（会话 Provider、Arco 样式、主题变量）
  components/
    layout/       站点页头 / 页脚 / 移动底部导航 / 用户中心与后台导航
    forum/        信息流、楼层、徽章、订阅、点赞、举报、通知、私信等业务组件
    content/      Markdown 编辑器与草稿
    member/        安全与资料设置表单
    admin/        后台外壳、审核动作、版块管理
    ui/           基础控件（头像、分页、空态、面包屑、内联图标）
  lib/
    api/          服务端 / 浏览器请求适配、DTO 类型、错误映射
    auth/         会话 Provider 与服务端会话
    markdown/     SSR / 预览共用的 Markdown 渲染、清理与表情
    data.server.ts 按请求缓存的取数函数
  styles/         主题变量与 Arco 覆盖
```

## 关键实现约定

- **SSR 与账号隔离**：公开首页、版块、主题、标签、搜索、用户页均由 Server Components 输出真实 HTML；`getSession` 等取数按请求转发 Cookie，CSP 由前端统一处理。
- **会话与 CSRF**：沿用 HttpOnly Cookie，不在前端使用 JWT。`SessionProvider` 在浏览器侧初始化 `csrfToken`，所有写请求带 `X-CSRF-Token`。
- **资源 ID**：始终按十进制字符串处理，避免 Number 精度丢失。
- **Markdown**：`react-markdown + remark-gfm + rehype-sanitize`，禁用原始 HTML，SSR 与预览共用同一套规则；表情短代码按 `/api/v1/smileys` 渲染为字符或受控 `/smiley` 图片。
- **写操作**：点赞、收藏、关注、订阅为显式目标状态或 toggle，不使用自动重试；失败保留用户输入。编辑冲突（409）保留本地内容。
- **关系状态**：论坛 / 主题 / 标签 / 用户详情由后端返回当前用户的订阅与关注状态；按钮以 `boolean | null` 区分“确定未关注”与“状态未确认”，不把查询失败当成未关注。
- **不存在与故障**：需要区分 404 与接口故障的页面使用三态取数（`ok` / `notfound` / `error`），只有真实 404 才返回“不存在”。

## 已实现页面

状态含义：**已实现** = 页面与交互已写；**已联调** = 通过真实 API / SSR 验证。

| 页面 | 路由 | 状态 |
| --- | --- | --- |
| PUB-01 首页信息流 | `/` | 已实现 / 已联调（SSR；综合流分页，关注版块 / 关注的人为真实聚合流，游标加载更多 + 去重） |
| PUB-02 版块目录 | `/forums` | 已实现 / 已联调 |
| PUB-03 版块详情 | `/forums/[fid]` | 已实现 / 已联调（排序、分页、订阅、发帖入口） |
| PUB-04 主题详情 | `/threads/[tid]` | 已实现 / 已联调（正文、楼层、引用、附件、回复） |
| PUB-05/06 标签目录 / 标签主题 | `/tags`、`/tags/[slug]` | 已实现 / 已联调 |
| PUB-07 搜索 | `/search` | 已实现 / 已联调（noindex） |
| PUB-08 用户公开主页 | `/users/[id]` | 已实现 / 已联调（主题 / 回复、关注） |
| AUTH-01/02/03/04 登录 / 注册 / 找回 / 重置 | `/login` 等 | 已实现 / 已联调（含 2FA 挑战） |
| AUTH-05 邮箱验证 / 换绑 | `/verify`、`/settings/email/confirm` | 已实现 / 已联调（需 SMTP 才能真正投递） |
| CONTENT-01 发布主题 | `/new` | 已实现 / 已联调（Markdown、预览、草稿、图片上传、待审反馈） |
| CONTENT-02 回复主题 | `/threads/[tid]/reply` | 已实现（跳转到主题页内联编辑器） |
| CONTENT-03 编辑 / 删除 | `/posts/[pid]/edit` | 已实现 / 已联调（冲突区分基础版本与最新版本，明确确认后才采用新版本提交；首楼删除含主题） |
| CONTENT-04/05 投票 / 悬赏 | 主题详情内嵌卡片 | 已实现 / 已联调（创建、投票、结果、结束、发布、冻结、采纳结算、取消退款） |
| CONTENT-06 每日签到 | `/checkin` | 已实现 / 已联调 |
| CONTENT-07 排行榜 | `/leaderboard` | 已实现 / 已联调（公开积分余额榜；只读快照，展示数据口径、生成时间与过期状态） |
| MEMBER-01～09 用户中心 | `/me/*` | 已实现 / 已联调（通知、私信已接真实接口） |
| ADMIN-01 概览 | `/admin` | 已实现 / 已联调 |
| ADMIN-02 审核与举报 | `/admin/moderation` | 已实现 / 已联调 |
| ADMIN-03 分类与版块 | `/admin/forums` | 已实现 / 已联调 |
| ADMIN-04 标签管理 | `/admin/tags` | 已实现 / 已联调（创建、编辑、启停、版本冲突） |
| ADMIN-05 用户与封禁 | `/admin/users` | 已实现 / 已联调（查询、详情、禁言、封禁登录、用户组） |
| ADMIN-11 站点设置 | `/admin/settings` | 已实现 / 已联调（schema 分组表单、校验、保存、状态） |

其余页面（AUTH-06 安装向导、ADMIN-06～10/12、Markdown 导出、robots/sitemap/RSS）保留在后续批次。

## 待补接口 / 已知限制

1. **摘要与封面口径**：`excerpt` 是去标记的纯文本（≤160 字，保留图片 alt 与代码块内容），封面只取首个站内 `/uploads/` 图片；取不到时展示纯文字信息流，封面加载失败会自动隐藏。
2. **关注流一致性**：游标按 `created_at,id` 推进，被删除或转为待审的主题会从后续页消失，不做位置补偿；“加载更多”按主题 ID 去重。
3. **热门话题 / 热门作者 / 多维榜**：目前只有积分余额榜一个公开榜单；热门作者、热门话题、日榜 / 周榜等新排名规则未实现，右栏继续使用真实的公告、快捷入口、标签与统计。
4. **引用内容摘要**：`replyTo` 只提供楼层与作者，引用块不显示被引用正文。
5. **邮件投递**：本地未配置 SMTP 时，换绑与重发验证邮件返回 `EMAIL_UNAVAILABLE`，界面提示“邮件服务未配置”。生产需配置 `FORUM_SMTP_HOST/PORT/FROM` 与 `FORUM_MAIL_KEY`（64 位十六进制），并把 `FORUM_SITE_URL` 指向**前端域名**，邮件里的 `/verify`、`/reset`、`/settings/email/confirm` 才由 Next.js 承接。
6. **私信附件 / 撤回 / 专用 SSE**：后端未提供，页面不做假设。
7. **投票改投**：后端不支持改投（返回 409），界面不做改投入口；投票创建后也不可编辑或重开。
8. **投票 / 悬赏后台**：待审投票与悬赏退款运维页面（`/admin/polls`、`/admin/bounties`）属后续批次，本批只在主题内接入用户侧流程。
9. **站内设置的部分保存**：保存走整体 PUT 并携带版本；PATCH 部分更新未在界面开放。schema 不含密钥字段，SMTP 凭据只来自部署环境变量。

关系状态由后端在详情 DTO 中返回 `subscribed` / `following`，详情页与关注列表不再依赖列表第一页近似判断。

## 本地联调说明

前端联调使用隔离的开发数据库（`forum_dev`），不复用、不重置原 `forum` 库。下面的 `forum_app` 是示例数据库角色；请替换为本地角色，并通过环境变量 `FORUM_DEV_DSN` 提供连接串，勿提交真实凭据：

```bash
# 1) 克隆演示库（示例）
psql "postgres://postgres@127.0.0.1:5432/postgres" -c "CREATE DATABASE forum_dev OWNER forum_app"
pg_dump "postgres://postgres@127.0.0.1:5432/forum" | psql "postgres://postgres@127.0.0.1:5432/forum_dev"

# 2) 启动 Go API
cd ..
go build -o .tmp/gobbs-forumd ./cmd/forumd
FORUM_DSN="${FORUM_DEV_DSN:?请先设置指向 forum_dev 的开发库连接串}" \
FORUM_ADDR='127.0.0.1:8090' ./.tmp/gobbs-forumd

# 3) 启动前端
cd frontend && API_INTERNAL_URL=http://127.0.0.1:8090 pnpm dev
```

演示管理员为 `admin / admin123456`（首次登录按站点规则可能需要改密）。

### 邮箱验证 / 换绑本地验证（可选）

邮件流程需要 SMTP 与令牌密钥；本地可用一个收件桩验证（不发送真实邮件）：

```bash
# 收件桩：把邮件写入 .tmp/mailbox
node .tmp/tools/smtp-sink.mjs        # 监听 127.0.0.1:2525

# 带邮件配置启动 Go API，并把站点地址指向前端，邮件链接才由 Next 页面承接
FORUM_SMTP_HOST=127.0.0.1 FORUM_SMTP_PORT=2525 FORUM_SMTP_FROM=noreply@gobbs.local \
FORUM_MAIL_KEY=<64 位十六进制> FORUM_SITE_URL=http://127.0.0.1:3000 ./gobbs-forumd
```

随后把开发库的 `email_verify_enabled` 设为 1，注册时带上邮箱即可收到验证邮件。
