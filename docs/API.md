# 当前后端 API（前端剥离版本）

2026-09-30：Go 页面渲染已移除，HTTP 实现在 `internal/api`；独立 Next.js 前端位于 `frontend/`，负责浏览器页面、SSR、Markdown 出口和 SEO。本文只描述 Go API 二进制，不应将其单独视为完整网站。当前功能状态见 [功能矩阵](FEATURE_STATUS.md)。

本文件描述当前已实现的接口。完整目标见 [前后端分离方案](FRONTEND_BACKEND_SEPARATION.md)。部分历史动作式接口仍保留旧字段命名和 toggle 语义，完整字段错误分支、通用幂等写入和 SDK 生成仍待继续补强；页面实现见 [前端状态](FRONTEND_STATUS.md)。

会员等级、成长规则、经验流水、徽章、版块访问限制及 12 个新增接口见 [会员 API 文档](MEMBERSHIP.md)。该文档定义配置预览、人工调整幂等和等级额度语义。

任务称号、自动补发、佩戴、作者采纳及 13 个新增接口见 [称号 API 文档](TITLES.md)。当前数据库 schema 为 21；schema 18–20 的投票、积分悬赏、签到新增 16 个 API，配置和第一版规则见 [互动 API](ENGAGEMENT_FEATURES.md)；schema 21 补齐退款后台全状态查询、诊断与重试，另提供公开互动规则和主题摘要。通知、楼层回复及定位、草稿标题和本人内容状态见 [基础流程 API](FORUM_WORKFLOWS.md)。标签、关注/粉丝、订阅投递和私信接口见 [社区 API](COMMUNITY_API.md)，该文档补充下方基础路由清单。邮箱换绑及旧恢复链接撤销见 [安全邮箱换绑](EMAIL_CHANGE.md)。数据库读取优化、主题游标分页、超时和连接池诊断见 [数据库性能](DATABASE_PERFORMANCE.md)。

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
- `GET /me/feed/forums`、`GET /me/feed/users` 是登录用户的关注聚合流，使用与 `GET /threads?pagination=cursor` 不同的**签名游标**：游标用 HMAC 绑定当前账号、流类型（forums/users）与排序（created），跨流、跨账号或篡改后返回 422 `INVALID_CURSOR`。两类流都在数据库层过滤待审、删除与版块可见性，再按主题 `created_at`、`id` 倒序分页；`limit` 限制在 1-50，默认取站点每页设置。版块流只认订阅关系（通知关闭或静音仍算订阅，不因此移出）；人的流只认被关注者作为**主题作者**发布的主题，不把回复当作新主题。`meta.followingCount` 用于区分“尚无关注”与“暂无内容”。
- 主题列表投影新增 `excerpt`（≤160 字的纯文本摘要）与 `coverUrl`（可选，仅站内 `/uploads/` 图片）。两者取自当前访问者**可见的首楼**，SQL 内已过滤待审与删除，为空时正常展示纯文字信息流；读取时计算、不做缓存，编辑 / 审核 / 删除立即生效，不依赖缓存过期。
- 普通 API 默认 15 秒超时，上传默认 60 秒，超时响应为 HTTP 503 `REQUEST_TIMEOUT`。SSE 和媒体保留流式策略，个人导出单独限制查询上下文。超时不保证此前写入未提交，客户端不得自动重试非幂等动作。

## 会话与 CSRF

设备列表、重命名与会话撤销接口见 [设备会话管理](DEVICE_SESSIONS.md)（schema 11）；管理员按用户查看 / 撤销会话使用 `GET|DELETE /api/v1/admin/users/{uid}/sessions[/{sessionId}]`，需要权限点 `sessions.manage`。

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
| `GET /me/feed/forums` | 关注版块流；`cursor`、`limit`；返回 `data.threads` 与签名游标 meta，不计算 total |
| `GET /me/feed/users` | 关注的人流；口径同上，只收录被关注者作为主题作者发布的主题 |
| `GET /leaderboard/points` | 公开积分余额榜；只读快照，返回 status/generatedAt/stale 与 entries |
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
- 站点设置新增带版本号的 JSON PUT 完整更新、PATCH 局部更新，以及 schema/status 元数据与生效诊断接口；GET 返回 20 项 camelCase 设置（含快照保留期 analyticsRetentionDays 和报表时区 reportTimeZone）和 `version`。原 POST 保留完整 snake_case 字段，但现在也必须提交 `version`，缺字段不再被解释为关闭开关。详见 [站点配置 API](SETTINGS.md)。
- 回收站：单主题使用 `tid`；批量清理使用 `kind`、`author`、`keyword`、`forum`、`days`，范围规则继续在 Go 校验。
- `GET /api/v1/admin` 另含 `data.databasePool`，提供本实例的连接使用量、获取耗时及等待/取消累计次数；沿用后台仪表盘权限，不公开到健康接口。
- `GET /api/v1/admin/forum-stats` 返回异步版块统计开关、待处理任务、重试数量和最老任务年龄；仅管理员可读。
- `GET /api/v1/admin/search-stats` 返回搜索索引队列待处理数、重试数和最老任务年龄；仅管理员可读。
- `GET /api/v1/admin/diagnostics` 返回连接池、数据库事务累计数、块读取/缓存命中累计数与命中率、锁等待、版块统计队列和搜索索引队列指标；仅管理员可读。块读取不等于物理磁盘 I/O，此接口不提供进程 CPU 或磁盘写入量。
- `GET /api/v1/admin/analytics/{name}` 读取指定排行榜或报表的最新 JSON 快照；仅管理员可读。保留最后成功结果，返回生成时间、存储桶、年龄和 `stale` 标识，未生成返回 404；见 [快照刷新与时效](ANALYTICS_SNAPSHOTS.md)。

## OpenAPI 与已注册路由

[openapi.json](openapi.json) 是当前 205 个 API 操作的机器可读索引，由源码路由和 [openapi.overrides.json](openapi.overrides.json) 中的人工审核定义生成。查看一个操作的 `x-source`、`x-handler` 可定位实现。媒体 URL `/uploads/`、`/avatar/{uid}`、`/smiley/{pkg}/{file}`、`/captcha/{id}` 另由受控媒体处理器提供。

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

当前 114 个操作有人工覆盖（98 fields、12 request、4 transport），其余 91 个为路由级定义。覆盖内容包括社区关系/订阅/私信/通知偏好、内容浏览/互动/审核、站点设置、互动功能、退款运维、公开规则、主题摘要和采纳结算契约。角色、版块、会员、会话和站点状态共同决定访问结果；`security` 声明登录要求不等于授予业务权限。所有写入要求 CSRF，首选请求头，过渡期兼容表单字段。

测试同时检查生成文件未漂移、引用可解析、operationId 唯一、路径参数、实际 ServeMux 注册，以及核心读写响应（会话、主题/楼层、列表/游标、搜索、发帖/编辑/回复以及社区标签、关系、订阅、私信）的字段形态。此阶段不是完整 OpenAPI 形式化校验器或全接口 SDK；新增字段和错误分支仍需逐组补齐。

内容浏览、点赞/收藏、定位/历史、删除/举报及后台审核的接入顺序、返回差异和重试规则见 [内容接入说明](FRONTEND_CONTENT_INTEGRATION.md)。后台审核队列不等同于前台详情 DTO，举报行保留 tId/threadTtl 字段；内容治理入口与其他后台入口一样要求完成初始改密。

标签目录/管理、关注粉丝、三类订阅、私信与屏蔽以及通知偏好的 31 个既有操作已补齐字段契约，页面流程见 [社区接入说明](FRONTEND_COMMUNITY_INTEGRATION.md)。三类订阅的新建/修改要求完成初始改密，取消不受此限制；私信 blocked 只表示当前用户自己的屏蔽，任一方屏蔽均拒绝发送。发送私信没有客户端幂等键，超时先读取历史，不自动重发。

## 运维与剩余边界

`/api/status` 和 `/api/v1/health/ready` 保留 `{ok, db, schema, pending, ts}` 监控结构；数据库不可用时返回 503。`/api/v1/health/live` 仅检查进程 HTTP 服务。

后端展示层剥离和业务接口化已完成，Next.js SSR/交互、HTML 与 Markdown 出口、SEO、安装向导和机器可读出口已由 `frontend/` 提供。剩余工作主要是完整字段契约/生成类型、请求幂等、生产域名与 Nginx 同域分流、真实 SMTP/生产容量验收，以及邮件详情、投票全状态台账和多维排行榜等非阻塞扩展。部分动作式接口与字段仍会在契约阶段继续规范化，联调前应锁定版本。
