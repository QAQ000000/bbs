# GoBBS 演进计划（ROADMAP）

> 目标：从「设计诚实的中小论坛」推进到「可以交给非开发站长日常开站」。
> 原则不变：单实例、零外部基础设施（不上 Redis / Elastic / WebSocket）、SSR + SSE。
> 每阶段独立可交付、可验收、可单独发版；顺序即建议实施顺序。

---

## 阶段一：账户与资料（用户每天会用的第一条路径）✅ 2026-09-05

**目标**：个人空间从只读变为可用，用户登录后有完整的自我管理能力。

| # | 任务 | 落点 | 状态 |
| --- | --- | --- | --- |
| 1.1 | 资料编辑页：签名（限长 200）、邮箱修改（唯一性校验 + 友好冲突文案） | `page_profile.html`、`internal/web/handler_profile.go`、`store.UpdateProfile` | ✅ |
| 1.2 | 修改密码：旧密码验证 + 新密码强度（≥8 位），成功后撤销其他会话（含会话缓存同步失效）；顺带修复 logout 二次哈希导致会话行未删除的 bug | `handler_profile.go`、`store.ChangePassword`、`sessionCache.invalidateUser` | ✅ |
| 1.3 | 「我回复过的主题」：按 `posts.author_id` 查最近参与的主题（去重、公开口径），主页两个列表 | `store.RecentRepliesOfUser`、`page_user.html` | ✅ |
| 1.4 | 头像上传（可选）：暂缓，字母 SVG 头像已可用 | — | ⏸ |

**验收**（已达成）：登录用户可改签名/邮箱/密码并即时生效；改密后其他设备会话失效；主页能看到回复历史；冒烟测试含 `/profile` GET 断言与 `TestProfileFlow`（资料保存/旧密码拒绝/不一致拒绝/改密往返）。

**依赖**：无。**风险**：改邮箱涉及唯一索引冲突文案（`users_email_unique_idx`），需友好提示。

---

## 阶段二：首页时间线与发帖入口（把「目录」变「社区」）✅ 2026-09-05

**目标**：第一眼进来的人能看到内容在流动；发新帖不再依赖记住 `?fid=`。

| # | 任务 | 落点 | 状态 |
| --- | --- | --- | --- |
| 2.1 | 首页「最新回复」块：最近 10 条公开口径主题（复用 `LatestThreads`，标题+版块名+回复数+时间） | `page_home.html`、`store.LatestThreads`、`latestItems` | ✅ |
| 2.2 | 「最新」入口 `/latest`：全站最新主题分页（20/页，含版块名前缀，游客走页缓存） | `page_latest.html`、`latestPage`、导航「最新」 | ✅ |
| 2.3 | 无 `fid` 发新帖：版块选择器页（optgroup 分组下拉，纯 HTML 零 JS）；提交仍无 `fid` 时重定向选择页而非 404 | `page_newforum.html`、`forumPicker`、`newThreadSubmit` 守卫 | ✅ |
| 2.4 | 首页「最新回复 + 公告」双栏（宽屏并排 / ≤800px 堆叠） | `page_home.html`、`app.css`（`.home-top`） | ✅ |

**验收**（已达成）：游客首页可见最新回复；`/latest` 分页可用；导航出现「最新」；`/new` 无参数出现版块选择器；375px 堆叠无横滚；冒烟测试新增 `/latest` 与 `/new`（无 fid）断言。

**依赖**：无。**注意**：最新列表只走公开口径（`NOT pending AND NOT deleted`），勿引入新计数。

---

## 阶段三：互动补全（举报 + 引用，通知体系的自然延伸）✅ 2026-09-05

**目标**：治理从「翻帖」变成「用户上报」；回复有上下文。

| # | 任务 | 落点 | 状态 |
| --- | --- | --- | --- |
| 3.1 | 举报：楼层「举报」details 浮层表单（零 JS），`reports` 表（同人同楼未处理举报唯一），审核队列「待处理举报」区：删除楼层（resolved + 广播 + 计数回补）/驳回（dismissed）；版主限管辖版块；限流 10/h | `schema.sql`、`store/report.go`、`handler_report.go`、`p_post.html`、`admin_moderate.html` | ✅ |
| 3.2 | 引用回复：楼层「引用」→ `/reply/{tid}?quote={pid}`，服务端预填 `引用 作者（N 楼）：\n> 摘要`（截 ~140 字），与云端草稿恢复共存（预填优先） | `handler_post.go`（`buildQuote`）、`p_post.html` | ✅ |
| 3.3 | 处理留痕：只写审计日志（`report.delete` / `report.dismiss`），被举报者不通知 | `logOp` | ✅ |

**验收**（已达成）：匿名举报 302 跳登录；登录用户举报后队列可见（理由+楼层号+举报人）；驳回/删楼后出队且落审计日志；引用预填进编辑器；冒烟测试 `TestReportFlow` 全流程回归。
**踩坑记录**：pgx 扩展协议下 SQL 占位符必须连续引用（跳号 `$1,$3` 报 42P18 无法推断 `$2` 类型；psql PREPARE 显式声明类型所以测不出来）。

---

## 阶段四：反垃圾（注册入口收口）✅ 2026-09-05

**目标**：批量注册与灌水号的成本显著高于收益。

| # | 任务 | 落点 | 状态 |
| --- | --- | --- | --- |
| 4.1 | 注册算术验证码：`internal/captcha` 自绘 SVG（表达式数字/噪声全走 crypto/rand，答案进程内存储、一次性、10min 过期），`/captcha/{id}` 输出，后台开关 | `internal/captcha/`、`page_register.html`、`settings.captcha_enabled` | ✅ |
| 4.2 | 邮箱验证（可选开关）：`email_verifications` 表（哈希令牌 24h、一用户一令牌）、注册即发验证邮件、`/verify` 一次性消费、资料页未验证横幅 + 重发（3 次/h）；未验证发外链入队（`email_unverified` 原因码）；SMTP 关闭时闸门自动失效 | `schema.sql`、`store.CreateEmailVerify/ConsumeEmailVerify`、`mail.NotifyEmailVerify`、`moderationDecision` | ✅ |
| 4.3 | 保留用户名：精确表（admin/root/system/管理员/版主…）+ 前缀表（admin/moderator/gobbs/official），大小写不敏感 | `registerSubmit`、`reservedName` | ✅ |

**验收**（已达成）：后台开验证码后注册页出现算术题、错误答案被拒、关闭后恢复；保留名注册被拒；captcha 包单测（通过/一次性/错误消费/SVG 渲染）；冒烟测试 `TestRegisterGate`（开关切换 + 页面控件 + 错误验证码 + 保留名）。验证码挑战全量 crypto/rand。

---

## 阶段五：运维硬化（能独自跑生产）✅ 2026-09-05

**目标**：把「看文档手工做」变成「产品里必须过」。

| # | 任务 | 落点 | 状态 |
| --- | --- | --- | --- |
| 5.1 | `-seed` 强制改密：种子管理员带 `must_change_password` 标志，登录重定向资料设置页；改密前内容写入口（发帖/回复/编辑/删除）与整个管理后台 403 拦截，改密成功自动清除标志 | `migrations/002`、`seed.go`、`loginSubmit`、`checkMustChangePassword` | ✅ |
| 5.2 | 启用编号迁移：启动先幂等重放 schema.sql，再按序执行 `db/migrations/NNN_*.sql`（> max(version)，逐条登记版本）；002 迁移已实战验证存量库升级 | `assets.Migrations`、`db.Migrate` | ✅ |
| 5.3 | 健康检查升级：`/api/status` 返回 DB 可达、schema 版本、治理队列积压（待审主题/回复/举报）；不含内部连接数 | `handler_static.go`、`store.SchemaVersion` | ✅ |
| 5.4 | `forumd -check-backup`：DSN 可写（临时表探测）、schema 版本、pg_dump 在 PATH、数据目录可写，失败退出码 1 | `cmd/forumd/check.go` | ✅ |
| 5.5 | 磁盘守护：设置项「目录占用上限 GB」，达上限拒绝新上传（507），仪表盘显示占用并在 ≥90% 时告警 | `settings.go`、`uploadImage`、`uploadDirBytes`（5min 缓存）、`admin_dash.html` | ✅ |

**验收**（已达成）：生产库经 002 迁移升级至版本 2；`-check-backup` 全项通过；未改密账号发帖/后台被 403 且资料页出现强制提示、改密后自动解除；健康检查含 schema 与积压字段；仪表盘显示上传占用。

---

## 阶段六：法律与发布包装（开源运营站点的最后一页）✅ 2026-09-05（6.4 待定名）

**目标**：对外开站不缺合规页面，发布流程有自检。

| # | 任务 | 落点 | 状态 |
| --- | --- | --- | --- |
| 6.1 | 服务条款 / 隐私政策页：内置默认文案（settings 存 Markdown，后台可编辑），`/terms` `/privacy` 渲染，页脚入口 | `page_doc.html`、`settings.go`（defaultTerms/defaultPrivacy）、`admin_settings.html` | ✅ |
| 6.2 | 注册页勾选「已阅读并同意」：后台开关（默认开），服务端强制校验 | `page_register.html`、`registerSubmit`、`require_consent` | ✅ |
| 6.3 | 用户数据导出：`/profile/export` 输出本人账号+主题+楼层 JSON 附件（不含密码哈希），审计日志留痕，限流 5/h | `store/export.go`、`profileExport` | ✅ |
| 6.4 | 项目定名确认：**待站长决策**（GoBBS 为暂用名），定名后替换 README/模板/logo 文案 | 全局文案 | ⏸ |

**验收**（已达成）：游客可读条款/隐私页（页脚入口）；注册页出现勾选且未勾选被拒；登录用户可导出本人数据（JSON 可解析、无哈希泄漏、附件下载）；冒烟测试新增 /terms /privacy 页面与 `TestProfileExport`、勾选缺失断言。

---

## 阶段七：口径修正与运营 P0（外部评审收缩版）✅ 2026-09-05

**背景**：外部评审提出「中小论坛 vs 主流社区 CMS」目标裁决。按既定定位先落地双路线都需要的 P0：
版主删除管辖口径修复、回复通知楼主、移帖、SEO 最小集、Logo 接入，以及设计债清理。

| # | 任务 | 落点 | 状态 |
| --- | --- | --- | --- |
| 7.1 | 版主删除限管辖：前台编辑/删除按钮拆分（`Deletable` 独立），帖子页按 `staffForumScope` 收窄，`/delete` 提交侧强制校验（自己的内容始终可删） | `render.go`、`p_post.html`、`handleThread`、`deletePost` | ✅ |
| 7.2 | 回复通知楼主：`notifyMentions` 重构出共用投递通道 `deliverNotifications`，新增 `notifyReply`（与 @ 去重、不通知自己），通知页区分「回复了你的主题」 | `handler_notify.go`、`mail.NotifyReply`、`page_notify.html` | ✅ |
| 7.3 | 移帖：`store.MoveThread`（重算新旧版块公开口径），后台内容管理批量操作加「移动到…」，双版块 SSE 广播；版主仅限管辖版块之间互移 | `admin.go`、`handler_admin_ops.go`、`admin_threads.html` | ✅ |
| 7.4 | SEO 最小集：帖子页 meta description（首楼摘要）+ Open Graph，版块页 description，`/sitemap.xml`（版块+公开主题≤2000），`/rss`（最新 20），robots 指向 sitemap | `layout.html`、`handler_page.go`、`handleRobots` | ✅ |
| 7.5 | Logo 接入：`FORUM_SITE_LOGO` 非默认值时渲染自定义 Logo，否则保留双色字标 | `layout.html` | ✅ |
| 7.6 | 设计债：密码策略统一 ≥8（注册/重置/改密）；TL2 权益落地（全站审核开启时资深成员免审核，`post.skip.moderate`）；公告支持 Markdown；隐私政策措辞与实际采集对齐；后台用户列表表头修正 | `handler_user.go`、`handler_reset.go`、`perm.go`、`page_home.html`、`settings.go`、`admin_users.html` | ✅ |

**验收**（已达成）：冒烟新增 `TestModeratorScope`（管辖内可见可删/管辖外按钮隐藏+提交 403/管理员不受限）、`TestReplyNotification`、`TestMoveThread`（含越权移动不生效）、`TestSEO`；生产库 sitemap/RSS 通过 XML 校验。

## 阶段八：P1 运营与建站能力（外部评审清单）✅ 2026-09-05

| # | 任务 | 落点 | 状态 |
| --- | --- | --- | --- |
| 8.1 | **可配置权限矩阵**：`role_perms` 表（空表播种默认），`perm.Load/Matrix/Defaults` 运行时热更新（AdminPanel×管理员硬保护防自锁），后台「权限矩阵」页逐格勾选，保存即时生效 | `matrix.go`、`perm.go`、`admin_perms.html`、`handler_setup.go` | ✅ |
| 8.2 | **禁言 ≠ 封禁**：`users.blocked_until`（封禁=禁止登录，与禁言分离），封禁即踢全部会话，登录被拒、既有会话按匿名处理；后台封禁（1/3/7 天/永久）/解封 | `admin.go`、`middleware.go`、`handler_user.go`、`admin_users.html` | ✅ |
| 8.3 | **头像上传**：`/profile/avatar`（≤2MB 内容嗅探 JPG/PNG/GIF/WebP），`/avatar/{uid}` 自定义优先、字母 SVG 兜底；可恢复默认。模板零改动（同 URL 智能解析） | `handler_profile.go`、`handler_notify.go` | ✅ |
| 8.4 | **附件挂楼层**：`uploads.post_id`，发帖/编辑/过审时按内容中的 `/uploads/...` 引用自动挂靠/解挂，楼层渲染附件列表（文件名+大小+下载） | `notify.go`、`handler_post.go`、`p_post.html` | ✅ |
| 8.5 | **版块内筛选**：最新回复（默认）/最新发表/精华/热门（按查看数），分页链接携带排序 | `forum.go`、`handler_page.go`、`page_forum.html` | ✅ |
| 8.6 | **搜索范围**：按版块、按作者过滤（SQL 侧 `$2/$3` 可空参数），搜索页过滤表单 | `search.go`、`page_search.html` | ✅ |
| 8.7 | **Logo/页脚可配置**：`site_logo`（后台优先于环境变量，默认双色字标）与 `footer_text` 设置项 | `settings.go`、`server.go`、`layout.html` | ✅ |
| 8.8 | **安装向导**：空库时全站 302 → `/setup`（站点名+管理员账号+密码），完成即登录；有用户时 /setup 重定向回首页防重复安装 | `setupGuard`、`page_setup.html` | ✅ |
| 8.9 | **自助删号**：资料页「注销账号」（密码确认），无公开内容时复用 `DeleteUser` 硬删，有内容时引导联系站长；管理员账号禁止自助删除；审计留痕 | `handler_profile.go` | ✅ |

**验收**（已达成）：冒烟新增 `TestPermMatrix`（关权限→行为变化→恢复）、`TestBlockUser`（会话失效+登录被拒+解封）、`TestAvatarUpload`（PNG 生效→SVG 兜底）、`TestAttachments`（上传→引用→楼层附件区）、`TestSelfDelete`（无内容删/有内容拒）、`TestSetupRedirect`；生产库经 003 迁移升级至版本 3，`role_perms` 播种 29 格（3 角色 × 18 点 - 硬保护差值按默认填充）。

---

## 阶段九：P2 社区闭环（外部评审清单）✅ 2026-09-05

| # | 任务 | 落点 | 状态 |
| --- | --- | --- | --- |
| 9.1 | **收藏 / 订阅 + 未读提醒**：`thread_favorites` 表（开关式收藏），收藏页含「新回复」标记（对比 `thread_reads` 已读进度与楼层数），铃铛计入收藏未读数 | `community.go`、`page_favorites.html`、`page_thread.html` | ✅ |
| 9.2 | **草稿箱页面**：列出云端草稿（上下文标签+摘要+跳转），可删除 | `page_drafts.html`、`draftsPage` | ✅ |
| 9.3 | **简单声望**：公开发帖数 + 获赞数，个人空间展示（`声望：N 帖 + M 赞`） | `Reputation`、`page_user.html` | ✅ |
| 9.4 | **编辑历史**：`post_edits` 保存改前快照（每次编辑），`/post/{pid}/history` 作者与版主可查，楼层显示「历史」入口（有历史才显示） | `community.go`、`page_history.html` | ✅ |
| 9.5 | **posts IP 列**：发帖/回帖记录来源 IP（隐私政策声明对齐），楼层仅管理员可见掩码（IPv4 前两段 / IPv6 前四组），首写后不随编辑变化 | `write.go`、`maskIP`、`p_post.html` | ✅ |
| 9.6 | **个人页分页**：最近主题/回复过的主题改为 tab + 每页 10 条分页，列表 SQL 返回 total | `forum.go`、`handler_user.go`、`page_user.html` | ✅ |

**验收**（已达成）：冒烟新增 `TestFavoriteFlow`、`TestDraftsPage`（API 保存→页面→删除）、`TestPostHistory`（快照含改前内容，游客跳登录）、`TestPostIP`（DB 记录+管理员掩码+非管理员不可见）、`TestUserPagination`；生产库经 004 迁移升级至版本 4；收藏/声望/分页在生产环境实测可用。

**未纳入**：标签/主题分类、私信、投票/活动帖、积分商城、OAuth/2FA、主题引擎与插件钩子——待阶段十裁决（同「CMS 转型」决策）。

---

### 待裁决：是否转向「主流社区 CMS」

评审指出 CMS 路线需要：版块读写权限矩阵、前台版主工具条、可配置权限入库、禁言≠封禁、头像、附件挂楼层、版块筛选、搜索过滤、安装向导、标签/订阅/积分等（工期差一个数量级），且与本文「明确不做」清单冲突。**维持当前定位则到此为止**；若决定转 CMS，从「版块权限 + 前台版主工具 + 权限矩阵入库」开始，并先改本文档目标。

---

## 明确不做（保持定位）

- Redis / 消息队列 / 外接搜索引擎 / WebSocket
- 自定义用户组后台、插件市场、多主题引擎、多实例与水平扩展
- 私信（若社区需要，作为独立大版本再评估，不塞进互动批次）

## 里程碑建议

| 里程碑 | 内容 | 交付口径 |
| --- | --- | --- |
| M1（阶段一+二） | 账户可用 + 首页有内容流 | 「新用户能自己注册、完善资料、看到活跃社区」 |
| M2（阶段三+四） | 举报/引用 + 注册收口 | 「站长敢开放注册」 |
| M3（阶段五+六） | 运维硬化 + 合规页面 | 「非开发站长可独立开站与升级」 |

每个阶段完成后：`go vet ./... && go test ./...` 全绿、冒烟测试补对应 GET 断言、Windows 镜像同步、更新 README 功能总览与本文件勾选状态。
