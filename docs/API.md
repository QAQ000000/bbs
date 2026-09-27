# 当前后端 API（前端剥离版本）

2026-09-27：Go 页面渲染已移除，HTTP 实现在 `internal/api`。本文对应当前源码；Nuxt 前端尚未创建，不能将纯 API 二进制当作包含页面的旧版网站。当前功能状态见 [功能矩阵](FEATURE_STATUS.md)。

本文件描述本轮已实现的接口。完整目标见 [前后端分离方案](FRONTEND_BACKEND_SEPARATION.md)。本轮先保留既有业务处理与部分动作式接口；OpenAPI 路由基线已建立；完整字段契约、统一写入字段命名、通用幂等写入及完整前端仍待后续阶段完成。

会员等级、成长规则、经验流水、徽章、版块访问限制及 12 个新增接口见 [会员 API 文档](MEMBERSHIP.md)。该文档定义配置预览、人工调整幂等和等级额度语义。

任务称号、自动补发、佩戴、作者采纳及 13 个新增接口见 [称号 API 文档](TITLES.md)。当前数据库 schema 为 17，新增异步版块统计、搜索任务和分析快照的编号迁移；Nuxt 管理和展示页面仍待开发。通知、楼层回复及定位、草稿标题和本人内容状态见 [基础流程 API](FORUM_WORKFLOWS.md)。标签、关注/粉丝、订阅投递和私信接口见 [社区 API](COMMUNITY_API.md)，该文档补充下方基础路由清单。邮箱换绑及旧恢复链接撤销见 [安全邮箱换绑](EMAIL_CHANGE.md)。数据库读取优化、主题游标分页、超时和连接池诊断见 [数据库性能](DATABASE_PERFORMANCE.md)。

## 请求与响应

- 普通接口返回 `application/json; charset=utf-8`，不返回页面或 Location 跳转。
- 普通成功响应为 `{"data": ...}`；分页响应另含 `meta: {page, pageSize, total, totalPages}`。
- 错误响应为 `{"error":{"code":"FORBIDDEN","message":"..."}}`，使用对应 HTTP 状态码。服务端内部错误不会直接返回数据库错误文本。
- ID 在新版普通 DTO 和 SSE 中使用字符串；时间通常使用 RFC 3339。`/me/export` 为独立下载格式，`/api/status` 及 ready 接口保留旧监控结构，是 envelope 的例外。
- API 数据响应使用 `Cache-Control: no-store`；受控附件也不使用共享缓存。公开头像/表情有独立媒体缓存策略。
- 首页 `/home` 的 `stats` 和版块 `todayCount` 使用服务端最多 5 秒的统计快照；版块权限、名称、最后回复及主题列表仍实时读取。发布或审核后，汇总数字可能短暂滞后于列表。后台统计和独立版块接口保持实时查询，不影响积分、经验或额度结算。
- 第一版业务写入接受 JSON 或 URL 编码表单，上传接受 multipart。JSON 使用下面列出的**现有业务字段名**，并非全部已统一为 camelCase；数组表示同名重复字段，布尔值按 1/0 传给旧校验流程。
- 列表从 `page=1` 开始，版块主题/帖子楼层沿用站点每页设置；当前未开放任意 pageSize 参数。越界返回空数据及总数。
- `GET /threads` 可选 `pagination=cursor`，下一页携带 `cursor=meta.nextCursor`；只支持默认最后回复排序，不与 `page` 或 `sort` 混用。此模式的 meta 为 `{pagination, pageSize, hasMore, nextCursor}`，不计算 total；权限每次重新校验。
- 普通 API 默认 15 秒超时，上传默认 60 秒，超时响应为 HTTP 503 `REQUEST_TIMEOUT`。SSE 和媒体保留流式策略，个人导出单独限制查询上下文。超时不保证此前写入未提交，客户端不得自动重试非幂等动作。

## 会话与 CSRF

设备列表、重命名与会话撤销接口见 [设备会话管理](DEVICE_SESSIONS.md)（schema 11）。

TOTP 绑定、验证登录、关闭和恢复码更新见 [二次验证](MFA.md)（schema 14）。已启用账户提交正确密码后返回 HTTP 401 `data.code=MFA_REQUIRED`，前端完成第二因素验证后才能取得登录 Cookie。

1. `GET /api/v1/session` 获取 `data.user`、`data.csrfToken` 和 `data.setupRequired`。游客 `user=null`，同时设置匿名 CSRF Cookie。
2. 浏览器同域请求携带 Cookie，所有写入发送 `X-CSRF-Token`。过渡期兼容表单 `_csrf` 字段；不能把 token 只放在 URL 查询参数中。
3. 登录成功设置 HttpOnly `forum_session` Cookie，返回本人安全资料；随后重新获取 session，使用登录态 CSRF token。
4. `data.mustChangePassword=true` 时前端引导改密；后端继续拒绝相关发帖和后台操作。退出及改密保留会话撤销语义。
5. 前端将邮件中的 `/verify?token=...`、`/reset?token=...` 页面参数交给对应 POST API；Go 不再处理这些页面 URL。

状态示例：未登录 401、权限/CSRF 不通过 403、不可见内容 404、编辑冲突/重复安装 409、校验失败 422、频率限制 429、关站 503。空库通过 session/setup 接口取得安装状态，业务 API 返回 `SETUP_REQUIRED`。

## 常用接口的字段

独立积分账户、奖励配置、流水、调账及对账见 [积分账本](POINTS.md)（schema 13）。

| 接口 | 输入 / 输出要点 |
| --- | --- |
| `GET /site` | 公开站点设置、注册/验证码/上传开关、Markdown 条款/隐私内容；不包含内部连接配置 |
| `GET /home` | 分类版块、统计、最新主题、公告的聚合数据 |
| `GET /threads` | `forumId` 可选；`page`、`sort=new/digest/hot`；返回 `data.threads`、`data.stickies` 及 meta；省略 forumId 取最新回复时间线 |
| `GET /threads/{tid}` | 主题信息、当前用户的收藏状态及操作能力；不增加浏览量 |
| `GET /threads/{tid}/posts` | 当前用户可见楼层分页，`content` 为 Markdown 原文，含附件与 capabilities；不写阅读记录 |
| `GET /posts/{pid}` | 单楼层及附件，供编辑、引用和 SSE 事件后的重新读取 |
| `GET /search` | `q`、`forumId`、`author`、`page`；摘要为纯文本，前端自行高亮 |
| `GET /users/{id}` | `tab=threads/replies`、`page`；公开用户、主题列表和声望；不含邮箱/密码/IP |
| `POST /auth/login` | `username`、`password` |
| `POST /auth/register` | `username`、`password`、`email`、`consent`；开启验证码时还需 `captcha_id`、`captcha` |
| `GET /auth/captcha` | 返回挑战 `id`、图像 `url`，不返回答案 |
| `POST /auth/password/forgot` | `email`；邮箱存在与否返回相同通用结果 |
| `POST /auth/password/reset` | `token`、`password` |
| `POST /auth/email/verify` | `token`；一次性验证 |
| `POST /setup` | `site_name`、`username`、`email`、`password`、`confirm_password` |
| `PATCH /me` | 可选 `signature`，省略保持原值；不能直接换绑邮箱 |
| `POST /me/email/change` | `email`、`password`；启用 2FA 需 `code` 或 `recovery`；成功 202，验证前不变更邮箱 |
| `POST /me/email/confirm` | `token`；在发起会话确认新邮箱，旧恢复链接与其他会话同时失效 |
| `POST /me/password` | `old_password`、`new_password`、`confirm_password` |
| `POST /threads` | `forumId`、`subject`、`content`；成功 201，返回 threadId、postId、pending、version |
| `POST /threads/{tid}/posts` | `content`；成功 201，返回资源 ID、审核状态和版本 |
| `PATCH /posts/{pid}` | `content`、`version`；首楼还需 `subject`；version 必填，冲突 409 |
| `DELETE /posts/{pid}` | 删除本人或有权治理的内容；仍校验版主管辖 |
| `POST /posts/{pid}/like` | 切换点赞，返回 liked/count；本轮保留 toggle，客户端不得自动重试 |
| `POST /threads/{tid}/favorite` | 切换收藏，返回 favorite/threadId；客户端不得自动重试 |
| `GET /me/draft` | 查询参数 `context=new:版块ID / reply:主题ID / edit:楼层ID` |
| `POST /me/draft` | `context`、`subject`、`content`；CSRF 使用请求头；两者都空才删除 |
| `DELETE /me/draft` | `context` |
| `POST /threads/{tid}/read` | `postId` 指定实际展示的可见楼层；同用户/主题限一分钟一次，GET/SSR 预取不计入阅读 |
| `POST /me/notifications/read` | `ids` 数组（最多 100 个）或 `all=true`；空对象返回 422；GET 不标记 |
| `GET /me/notifications` | page、unread=true/false；30 条每页，data 数组加 meta |
| `GET /me/notifications/summary` | 当前可见未读数 data.unread |
| `GET/PUT /me/notification-preferences` | 八项布尔偏好；PUT 为完整覆盖 |
| `GET /me/content` | kind=threads/replies、status=all/published/pending/rejected/deleted、page |
| `GET /posts/{pid}/position` | 当前可见楼层的 page、pageSize、floor，供通知和回复定位 |
| `POST /posts/{pid}/reports` | `reason`，不能举报自己或不可见的内容 |
| `POST /uploads` | multipart：`file`、`kind=image/file`；成功 201，返回 url/name/kind/mime |
| `POST /me/avatar` | multipart：`avatar`，最大 2MB；`DELETE /me/avatar` 恢复默认头像 |
| `DELETE /me` | `password`；仍校验账号归属、管理员限制和已有内容 |

本表路径省略 `/api/v1`。写入字段以 [API 处理代码](../internal/api) 为准。图片支持 JPG/PNG/GIF/WebP，附件支持 PDF/TXT/ZIP，依后台限额和内容嗅探校验；已有 `/uploads/`、`/avatar/`、`/smiley/` 链接继续有效。

发帖与编辑只保存 Markdown，旧 `content_html` 列暂留但新写入不再生成 HTML。不能直接把新写入数据交给依赖 content_html 的旧页面版本做无条件回滚；如需回退，必须先恢复兼容的正文转换流程。

## SSE

`GET /api/v1/events?thread=主题ID`、`?forums=版块ID,版块ID` 或 `?user=本人ID`，参数可组合。返回 `text/event-stream`，25 秒心跳；订阅建立及长连接期间均检查会话/资源可见性。

每次事件或心跳发送前，通过一次 SQL 重新读取会话、封禁、会员等级、版主管辖、版块权限与主题状态；不使用跨请求权限缓存。会话撤销、关闭站点或失去访问权限后发送 `subscription.reset` 并断开，数据库读取失败也按失效处理。

```json
{"type":"post.edit","postId":"456","threadId":"123","forumId":"1","version":3}
```

第一版保留旧业务事件名称：`post.new`、`post.edit`、`post.delete`、`post.like`、`thread.new`、`thread.update`、`thread.delete`、`thread.deleted`、`notify`。载荷已删除 postHtml、threadRow、forumRow，仅有 ID、版本、楼层、计数及通知信息；各字段只按对应事件类型使用，不能用其他事件的默认零计数覆盖本地状态。

前端合并密集事件后重新获取对应 API。收到 `subscription.reset` 时清除可能已失效的本地内容并重新验证访问；重连同样重新读取当前数据。本轮不提供持久化事件重放，也不保证恰好一次交付。

## 后台动作字段

后台数据读取已经改为 JSON；写接口暂沿用既有动作名和字段，避免在剥离页面时同时更换整套治理业务：

- 版块保存：`id`、`category_id`、`name`、`description`、`moderators`；分类保存：`id`、`name`。
- 主题动作：`op`、重复 `tid`，移动时另传 `move_to` 目标版块 ID。
- 用户动作：`uid`，禁言/封禁另传 `days`、`reason`，改组传 `group`。
- 审核：`tid` 或 `pid`、`op=approve/delete`、可选 `note`（最多 500 字）；首楼使用主题接口。举报处理：`id`、`op=delete/dismiss`。
- 权限矩阵：完整字段集 `allow.角色编号.权限点`；缺失即关闭，管理员入口有防自锁保护。
- 站点设置新增带版本号的 JSON PUT 完整更新、PATCH 局部更新，以及 schema/status 元数据与生效诊断接口；GET 返回 18 项 camelCase 设置和 `version`。原 POST 保留完整 snake_case 字段，但现在也必须提交 `version`，缺字段不再被解释为关闭开关。详见 [站点配置 API](SETTINGS.md)。
- 回收站：单主题使用 `tid`；批量清理使用 `kind`、`author`、`keyword`、`forum`、`days`，范围规则继续在 Go 校验。
- `GET /api/v1/admin` 另含 `data.databasePool`，提供本实例的连接使用量、获取耗时及等待/取消累计次数；沿用后台仪表盘权限，不公开到健康接口。
- `GET /api/v1/admin/forum-stats` 返回异步版块统计开关、待处理任务、重试数量和最老任务年龄；仅管理员可读。
- `GET /api/v1/admin/search-stats` 返回搜索索引队列待处理数、重试数和最老任务年龄；仅管理员可读。
- `GET /api/v1/admin/diagnostics` 返回连接池、数据库事务累计数、块读取/缓存命中累计数与命中率、锁等待、版块统计队列和搜索索引队列指标；仅管理员可读。块读取不等于物理磁盘 I/O，此接口不提供进程 CPU 或磁盘写入量。
- `GET /api/v1/admin/analytics/{name}` 读取指定排行榜或报表的最新 JSON 快照；仅管理员可读。保留最后成功结果，返回生成时间、存储桶、年龄和 `stale` 标识，未生成返回 404；见 [快照刷新与时效](ANALYTICS_SNAPSHOTS.md)。

## OpenAPI 与已注册路由

[openapi.json](openapi.json) 是当前 178 个 API 操作的机器可读索引，由源码路由和 [openapi.overrides.json](openapi.overrides.json) 中的人工审核定义生成。查看一个操作的 `x-source`、`x-handler` 可定位实现。媒体 URL `/uploads/`、`/avatar/{uid}`、`/smiley/{pkg}/{file}`、`/captcha/{id}` 另由受控媒体处理器提供。

```bash
go run ./scripts/api-contract         # 更新生成文件
go run ./scripts/api-contract -check  # 检查路由/审核定义与生成文件一致
```

每个操作使用 `x-contract-level` 区分完成度：

| 标记 | 含义 |
| --- | --- |
| fields | 已审核主要输入和核心返回字段，仍允许额外字段；权限条件和部分错误分支继续见专题文档 |
| request | 已审核主要请求字段，返回使用通用结构 |
| transport | SSE、原始下载或监控等非标准 envelope 传输 |
| route | 路由、基础鉴权提示和通用占位；尚未完成字段审核，不可据此生成完整业务类型 |

当前 35 个操作有人工覆盖，其余为路由级定义。角色、版块、会员、会话和站点状态共同决定访问结果；`security` 声明登录要求不等于授予业务权限。所有写入要求 CSRF，首选请求头，过渡期兼容表单字段。

测试同时检查生成文件未漂移、引用可解析、operationId 唯一、路径参数、实际 ServeMux 注册，以及核心读写响应（会话、主题/楼层、列表/游标、搜索、发帖/编辑/回复）的字段形态。此阶段不是完整 OpenAPI 形式化校验器或全接口 SDK；新增字段和错误分支仍需逐组补齐。

## 运维与尚未完成项

`/api/status` 和 `/api/v1/health/ready` 保留 `{ok, db, schema, pending, ts}` 监控结构；数据库不可用时返回 503。`/api/v1/health/live` 仅检查进程 HTTP 服务。

本次完成后端展示层剥离和业务接口化，不表示分离方案全部完成。后续需要 Nuxt SSR/交互、HTML 与 Markdown 出口、SEO/旧页面 URL、完整字段契约/生成类型、请求幂等、前后端发布编排。部分动作式接口与字段将在契约阶段规范化，联调前应锁定版本。
