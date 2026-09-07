# 当前后端 API（前端剥离版本）

2026-09-06：本工作区已移除 Go 页面渲染，HTTP 实现在 `internal/api`。Nuxt 前端尚未创建；当前源码构建的服务不能直接作为旧版完整网站使用，现有线上二进制本次未替换。

本文件描述本轮已实现的接口。完整目标见 [前后端分离方案](FRONTEND_BACKEND_SEPARATION.md)。本轮先保留既有业务处理与部分动作式接口；OpenAPI、统一写入字段命名、幂等写入及完整前端仍待后续阶段完成。

会员等级、成长规则、经验流水、徽章、版块访问限制及 12 个新增接口见 [会员 API 文档](MEMBERSHIP.md)。该文档定义配置预览、人工调整幂等和等级额度语义。

任务称号、自动补发、佩戴、作者采纳及 13 个新增接口见 [称号 API 文档](TITLES.md)。当前数据库 schema 为 9，Nuxt 管理和展示页面仍待开发。通知、楼层回复及定位、草稿标题和本人内容状态见 [基础流程 API](FORUM_WORKFLOWS.md)。标签、关注/粉丝、订阅投递和私信接口见 [社区 API](COMMUNITY_API.md)，该文档补充下方基础路由清单。

## 请求与响应

- 普通接口返回 `application/json; charset=utf-8`，不返回页面或 Location 跳转。
- 普通成功响应为 `{"data": ...}`；分页响应另含 `meta: {page, pageSize, total, totalPages}`。
- 错误响应为 `{"error":{"code":"FORBIDDEN","message":"..."}}`，使用对应 HTTP 状态码。服务端内部错误不会直接返回数据库错误文本。
- ID 在新版普通 DTO 和 SSE 中使用字符串；时间通常使用 RFC 3339。`/me/export` 为独立下载格式，`/api/status` 及 ready 接口保留旧监控结构，是 envelope 的例外。
- API 数据响应使用 `Cache-Control: no-store`；受控附件也不使用共享缓存。公开头像/表情有独立媒体缓存策略。
- 第一版业务写入接受 JSON 或 URL 编码表单，上传接受 multipart。JSON 使用下面列出的**现有业务字段名**，并非全部已统一为 camelCase；数组表示同名重复字段，布尔值按 1/0 传给旧校验流程。
- 列表从 `page=1` 开始，版块主题/帖子楼层沿用站点每页设置；当前未开放任意 pageSize 参数。越界返回空数据及总数。

## 会话与 CSRF

1. `GET /api/v1/session` 获取 `data.user`、`data.csrfToken` 和 `data.setupRequired`。游客 `user=null`，同时设置匿名 CSRF Cookie。
2. 浏览器同域请求携带 Cookie，所有写入发送 `X-CSRF-Token`。过渡期兼容表单 `_csrf` 字段；不能把 token 只放在 URL 查询参数中。
3. 登录成功设置 HttpOnly `forum_session` Cookie，返回本人安全资料；随后重新获取 session，使用登录态 CSRF token。
4. `data.mustChangePassword=true` 时前端引导改密；后端继续拒绝相关发帖和后台操作。退出及改密保留会话撤销语义。
5. 前端将邮件中的 `/verify?token=...`、`/reset?token=...` 页面参数交给对应 POST API；Go 不再处理这些页面 URL。

状态示例：未登录 401、权限/CSRF 不通过 403、不可见内容 404、编辑冲突/重复安装 409、校验失败 422、频率限制 429、关站 503。空库通过 session/setup 接口取得安装状态，业务 API 返回 `SETUP_REQUIRED`。

## 常用接口的字段

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
| `PATCH /me` | `signature`、`email` |
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
- 设置保存为完整表单，字段仍为 `site_name`、`register_enabled` 等 snake_case；切勿把它当作局部 PATCH。布尔值支持 JSON true/false 或表单 1/0。
- 回收站：单主题使用 `tid`；批量清理使用 `kind`、`author`、`keyword`、`forum`、`days`，范围规则继续在 Go 校验。

## 已注册路由清单

以下清单对应本轮实际路由；不包含 Nginx/Nuxt 页面：

| 方法 | 路径 |
| --- | --- |
| GET | `/api/v1/session` |
| GET | `/api/v1/site` |
| GET | `/api/v1/home` |
| GET | `/api/v1/forums` |
| GET | `/api/v1/forums/{fid}` |
| GET | `/api/v1/threads` |
| GET | `/api/v1/threads/{tid}` |
| GET | `/api/v1/threads/{tid}/posts` |
| GET | `/api/v1/posts/{pid}` |
| GET | `/api/v1/search` |
| GET | `/api/v1/users/{id}` |
| GET | `/api/v1/me` |
| GET | `/api/v1/me/favorites` |
| GET | `/api/v1/me/drafts` |
| GET | `/api/v1/me/draft` |
| GET | `/api/v1/me/notifications` |
| GET | `/api/v1/me/notifications/summary` |
| GET | `/api/v1/me/notification-preferences` |
| GET | `/api/v1/me/content` |
| GET | `/api/v1/posts/{pid}/position` |
| GET | `/api/v1/posts/{pid}/history` |
| GET | `/api/v1/me/export` |
| GET | `/api/v1/smileys` |
| GET | `/api/v1/setup` |
| GET | `/api/v1/auth/captcha` |
| GET | `/api/v1/events` |
| GET | `/api/v1/posts/{pid}/likes` |
| POST | `/api/v1/auth/login` |
| POST | `/api/v1/auth/register` |
| POST | `/api/v1/auth/logout` |
| POST | `/api/v1/auth/password/forgot` |
| POST | `/api/v1/auth/password/reset` |
| POST | `/api/v1/auth/email/verify` |
| POST | `/api/v1/me/email/verify-resend` |
| POST | `/api/v1/setup` |
| PATCH | `/api/v1/me` |
| POST | `/api/v1/me/password` |
| POST | `/api/v1/me/avatar` |
| DELETE | `/api/v1/me/avatar` |
| DELETE | `/api/v1/me` |
| POST | `/api/v1/threads` |
| POST | `/api/v1/threads/{tid}/posts` |
| PATCH | `/api/v1/posts/{pid}` |
| DELETE | `/api/v1/posts/{pid}` |
| POST | `/api/v1/posts/{pid}/like` |
| POST | `/api/v1/threads/{tid}/favorite` |
| POST | `/api/v1/me/draft` |
| DELETE | `/api/v1/me/draft` |
| POST | `/api/v1/me/notifications/read` |
| PUT | `/api/v1/me/notification-preferences` |
| POST | `/api/v1/threads/{tid}/read` |
| POST | `/api/v1/uploads` |
| POST | `/api/v1/posts/{pid}/reports` |
| GET | `/api/v1/admin` |
| GET | `/api/v1/admin/forums` |
| GET | `/api/v1/admin/threads` |
| GET | `/api/v1/admin/users` |
| GET | `/api/v1/admin/settings` |
| GET | `/api/v1/admin/perms` |
| GET | `/api/v1/admin/logs` |
| GET | `/api/v1/admin/recyclebin` |
| GET | `/api/v1/admin/censor` |
| GET | `/api/v1/admin/announcements` |
| GET | `/api/v1/admin/moderate` |
| POST | `/api/v1/admin/forums/save` |
| POST | `/api/v1/admin/forums/delete` |
| POST | `/api/v1/admin/forums/move` |
| POST | `/api/v1/admin/cats/save` |
| POST | `/api/v1/admin/cats/delete` |
| POST | `/api/v1/admin/threads/action` |
| POST | `/api/v1/admin/users/ban` |
| POST | `/api/v1/admin/users/unban` |
| POST | `/api/v1/admin/users/group` |
| POST | `/api/v1/admin/users/delete` |
| POST | `/api/v1/admin/users/block` |
| POST | `/api/v1/admin/users/unblock` |
| POST | `/api/v1/admin/settings` |
| POST | `/api/v1/admin/perms/save` |
| POST | `/api/v1/admin/recyclebin/restore` |
| POST | `/api/v1/admin/recyclebin/purge` |
| POST | `/api/v1/admin/recyclebin/purgeall` |
| POST | `/api/v1/admin/censor/add` |
| POST | `/api/v1/admin/censor/delete` |
| POST | `/api/v1/admin/moderate/thread` |
| POST | `/api/v1/admin/moderate/post` |
| POST | `/api/v1/admin/report/handle` |
| POST | `/api/v1/admin/prune/execute` |
| POST | `/api/v1/admin/announcements/add` |
| POST | `/api/v1/admin/announcements/toggle` |
| POST | `/api/v1/admin/announcements/delete` |
| GET | `/uploads/` |
| GET | `/smiley/{pkg}/{file}` |
| GET | `/avatar/{uid}` |
| GET | `/captcha/{id}` |
| GET | `/api/status` |
| GET | `/api/v1/health/ready` |
| GET | `/api/v1/health/live` |

## 运维与尚未完成项

`/api/status` 和 `/api/v1/health/ready` 保留 `{ok, db, schema, pending, ts}` 监控结构；数据库不可用时返回 503。`/api/v1/health/live` 仅检查进程 HTTP 服务。

本次完成后端展示层剥离和业务接口化，不表示分离方案全部完成。后续需要 Nuxt SSR/交互、HTML 与 Markdown 出口、SEO/旧页面 URL、OpenAPI/生成类型、请求幂等、前后端发布编排。部分动作式接口与字段将在契约阶段规范化，联调前应锁定版本。
