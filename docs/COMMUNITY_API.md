# 社区关系与私信 API

2026-09-08 订阅修复：临时待审或删除的事件保留原始时间和进度，worker 跳过隐藏内容，重新公开后继续未完成的投递。隐藏事件不会阻塞后续公开事件；投递回执防重，后订阅用户不会补收旧事件。schema 15 首次升级会恢复旧 worker 可能误标完成的事件，保留原游标与回执，重复启动不会再次重置。

2026-09-07，schema 9。本页描述已经实现的后端接口。计划与未完成模块见 [社区扩展方案](COMMUNITY_FEATURES_PLAN.md)。路径均以 `/api/v1` 开头；写入需登录 Cookie 和 `X-CSRF-Token`，ID 使用字符串。

## 标签

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/tags?q=&page=1` | 活跃标签列表，30 条每页；threadCount 只统计当前用户可见的公开主题 |
| GET | `/tags/{tagId}` | 标签详情，停用标签仍可读取 |
| GET | `/tag-slugs/{slug}` | 当前 slug 或旧别名解析，返回标签详情 |
| GET | `/tags/{tagId}/threads?page=1` | 按标签筛选公开主题，复用版块权限和主题分页设置 |
| GET | `/admin/tags?q=&page=1` | 包括停用标签；需 tags.configure |
| POST | `/admin/tags` | 创建标签；需 tags.configure |
| PUT | `/admin/tags/{tagId}` | 全量编辑、重命名或停用；需 tags.configure 及当前 version |
| PUT | `/threads/{tid}/tags` | 替换标签；需主题首楼编辑权限，遵守等级编辑时间限制 |

标签字段为 name、slug、description、color、status、version。name 小写并折叠空白，最多 32 字；slug 为 1-64 个小写 ASCII 字母/数字/连字符，首字符为字母或数字；color 为空或 #RRGGBB；description 最多 500 字。status 为 active/disabled，创建默认 active。名称和 slug 冲突、旧版本更新返回 409。

重命名 slug 保留旧 slug 别名；其他标签不能占用旧别名。停用后禁止新绑定，但原有主题可保留并读取。第一版没有标签合并和手动别名管理接口。

创建主题 `POST /threads` 增加可选 `tagIds:["1","2"]`，最多 8 个，不允许重复。标签校验和主题写入在同一事务中，失败不留下部分主题。编辑标签提交 `{"version":1,"tagIds":["2"]}`，version 是首楼版本，成功递增版本；空数组清空标签。主题列表/详情返回 tags 数组。修改历史主题标签不补发订阅通知。

## 关注与粉丝

- `POST /users/{id}/follow`：幂等关注，不允许自己、不存在或封禁账号。
- `DELETE /users/{id}/follow`：幂等取消关注。
- `GET /me/following?page=1`、`GET /me/followers?page=1`、`GET /users/{id}/followers?page=1`：登录可用，30 条每页，返回 id、username、followedAt 和 meta。

关注不自动订阅内容，也不影响私信资格。会话被任一方屏蔽时禁止重新关注，屏蔽会清除双方关注关系；取消屏蔽不自动恢复关注。

## 订阅

主题、版块、标签分别使用 `/threads/{tid}/subscribe`、`/forums/{fid}/subscribe`、`/tags/{tagId}/subscribe`。

- POST：创建订阅，enabled、notifyInApp、notifyEmail 默认 true；重复请求保留已有设置。
- PUT：完整更新 enabled、notifyInApp、notifyEmail；可选 mutedUntil 为 RFC3339 时间，空字符串或省略取消静音。
- DELETE：取消订阅。即使后来失去内容权限，仍可取消自己的订阅。
- `GET /me/subscriptions?kind=thread&page=1`：kind 为 thread/forum/tag；每页 30 条，按当前版块权限过滤。

主题订阅通知该主题新回复；版块、标签订阅通知新主题。创建订阅需要资源存在、可见；不能订阅待审主题或停用标签。失去版块权限的订阅暂不显示，也不投递通知。

`/me/notification-preferences` 增加 subscriptions 布尔开关，默认 true；旧客户端不传此字段时保留当前值。email 全局开关也影响订阅邮件。notifyInApp 和 notifyEmail 可分别关闭，mutedUntil 生效期间两种通知均暂停。

发布/审核通过在原事务内写 subscription_events，后台每秒触发，每轮最多处理 20 批，每批最多 50 个订阅者，整轮执行时限 10 秒；实际吞吐取决于数据库负载。每批通过同一事务连接读取接收者的最新会员与版块权限，排除发帖者和封禁用户；取消/静音的订阅不参与尚未生成的通知。重复订阅多个来源只产生一条通知；@、指定回复和楼主通知在订阅事务开始前恢复投递，按用户+楼层去重。

订阅回执、站内通知、邮件入队与游标更新批量提交，失败整体回滚。提交后按接收者可见范围批量计算未读数，仍发送原 `notify` 事件；SSE 不保证送达，重连后应从通知 API 获取当前状态。

站内通知类型为 subscription，沿用 content 可见性规则。分发游标和站内通知事务提交，失败回滚，进程重启继续；迁移不向历史帖子补发消息，也不会向发布后才订阅的用户补发。订阅事件不依赖浏览器在线。

**邮件边界：**schema 10 已将订阅邮件和分发游标放入同一数据库事务，支持崩溃恢复、失败退避和后台重试，详见 [邮件队列](EMAIL_QUEUE.md)。未配置 SMTP 不创建邮件任务，邮件默认开启只是用户偏好，不代表投递成功。SMTP 不保证恰好一次交付。

## 私信

| 方法 | 路径 | 参数/行为 |
| --- | --- | --- |
| POST | `/users/{id}/messages` | body，1-5000 字，返回完整消息含 conversationId |
| GET | `/me/conversations?page=1` | 30 条每页，包含对方、安全 ID、lastMessageAt、unread、blocked、waitingForReply |
| GET | `/conversations/{cid}/messages?before=消息ID` | 最新在前，每次最多 50 条；下一页用最小 ID；读取不自动标已读 |
| POST | `/conversations/{cid}/read` | messageId，必须属于此会话；已读位置单调推进 |
| POST | `/conversations/{cid}/block` | 屏蔽对方；双方暂停发送 |
| DELETE | `/conversations/{cid}/block` | 取消自己的屏蔽；对方设置的屏蔽继续生效 |

无需关注。发起者只能发送首条；接收方回复后才能继续。双向同时发起只创建一个会话，数据库唯一约束及事务锁防止并发绕过。删除消息、刷新页面、再次调用发送接口均不能重置会话资格。屏蔽与发送共用锁，屏蔽成功后后续发送被拒绝；解除屏蔽保留原来的等待回复状态。

每用户每分钟最多 30 条消息、滚动 24 小时最多发起 20 个会话；限额在数据库事务内校验，API 另有入口限流。首版限额为后端常量，后台可配置限额尚未接入。发送复用禁言、封禁、强制改密和敏感词检查。正文为文本/Markdown，不返回服务端渲染 HTML。

未登录 401；不是会话成员（包括管理员）读取、已读或屏蔽返回 404；被屏蔽 403 MESSAGE_BLOCKED；等待回复 409 MESSAGE_REPLY_REQUIRED；超额 429 MESSAGE_RATE_LIMITED；无效内容 422。会话内的私信不会发布到公共 SSE 或公开通知。

当前没有私信附件、撤回、举报处理、会话实时推送及新私信邮件。会话未读可通过列表轮询获取。有会话或标签管理记录的账号暂按有关联内容处理，注销返回业务错误；匿名化注销和私信个人导出后续实现。

## 运行与验证

迁移 009 和 schema.sql 保持一致；forumd 启动自动执行迁移并启动订阅任务。迁移本身可重放，升级测试覆盖 schema 8 到 9、重复启动保留消息和关注，以及反向用户对的唯一约束。

新增数据库测试覆盖并发首条、双向发起、屏蔽及解除、非成员访问、未读和消息游标、标签冲突与回滚、订阅默认值、偏好保留、任务失败重试、权限撤销及 CSRF。必须配置隔离的 gobbs_test_ 测试库；普通 go test 在未配置 DSN 时会跳过这些数据库测试。

2026-09-07 验证记录：46 项 store、43 项 API、4 项迁移测试在独立数据库通过，均启用 `-race`；`go test ./...`、`go vet ./...`、forumd 构建和 `git diff --check` 通过。真实 HTTP 服务验证标签创建/改名/旧别名、多来源订阅去重、后台自动投递、提及优先、私信首条限制、已读、屏蔽/解屏蔽及重启持久化。临时服务、测试库和角色已清理；未验证真实 SMTP 收信，也未替换原本地服务。
