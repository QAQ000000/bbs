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

## 阶段五：运维硬化（能独自跑生产）

**目标**：把「看文档手工做」变成「产品里必须过」。

| # | 任务 | 落点 |
| --- | --- | --- |
| 5.1 | `-seed` 强制改密：种子管理员首次登录强制进改密页（`users.must_change_password` 标志） | `seed.go`、登录后跳转中间态 |
| 5.2 | 启用编号迁移：`db.Migrate` 读 `schema_migrations.max(version)`，顺序执行 `assets/db/migrations/NNN_*.sql`，`schema.sql` 只保留基线 | `internal/db/db.go`、迁移文件目录 |
| 5.3 | 健康检查升级：`/api/status` 迁移版本、DB 可达、队列积压（审核/举报条数）；不含内部连接数（口径已收） | `handler_static.go` |
| 5.4 | 备份自检命令：`forumd -check-backup` 验证 DSN 可写、`pg_dump` 在 PATH、data 目录可写，输出上线检查单 | `cmd/forumd/main.go` |
| 5.5 | 磁盘守护：上传目录超阈值（后台可设 GB 数）拒绝新上传并在仪表盘告警 | `handler_notify.go` 上传路径、`admin_dash.html` |

**验收**：新装站点首次登录必须改密；破坏性迁移演练（改一列类型）走编号迁移成功；检查单命令绿。

**依赖**：5.2 是后续所有 schema 变更的前置，应最先做。

---

## 阶段六：法律与发布包装（开源运营站点的最后一页）

**目标**：对外开站不缺合规页面，发布流程有自检。

| # | 任务 | 落点 |
| --- | --- | --- |
| 6.1 | 服务条款 / 隐私政策页：模板 + 后台可编辑（存 settings，支持 Markdown） | `page_terms.html`、`page_privacy.html`、`admin_settings.html` |
| 6.2 | 注册页勾选「已阅读并同意」（开关可关） | `page_register.html` |
| 6.3 | 用户数据导出：`/profile/export` 输出本人帖子 JSON（含 md 原文），审计日志留痕 | `handler_profile.go` |
| 6.4 | 项目定名确认：替换 GoBBS 暂用名（用户保留替换权），README/模板/logo 一致性检查 | 全局文案 |

**验收**：游客可读条款页；导出文件可解析且只含本人数据；`scripts/check-clean.sh` 通过。

**依赖**：无；6.4 由用户决策触发。

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
