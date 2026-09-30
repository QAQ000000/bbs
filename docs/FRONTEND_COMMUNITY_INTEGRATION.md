# 标签、关注、订阅与私信接入

适用于独立前端对接已有社区 API；不代表前端页面已经实现。请求与响应字段以 [OpenAPI](openapi.json) 的已审核定义为准，内容浏览与编辑另见 [内容接入说明](FRONTEND_CONTENT_INTEGRATION.md)。

## 按页面接入

| 页面 / 动作 | 接口 | 前端处理 |
| --- | --- | --- |
| 标签目录 | GET /api/v1/tags?q=&page=1 | 仅列出 active 标签，每页 30 条，按名称和 ID 排序 |
| 标签页 / 旧链接 | GET /api/v1/tags/{tagId}、/tag-slugs/{slug}、/tags/{tagId}/threads | slug 查询直接返回规范标签，不发送 HTTP 重定向；主题列表按最后回复时间倒序、受可见权限过滤 |
| 后台标签维护 | GET、POST /api/v1/admin/tags；PUT /api/v1/admin/tags/{tagId} | 需要管理员及 tags.configure；保存时保留 version，冲突先刷新再合并 |
| 编辑主题标签 | PUT /api/v1/threads/{tid}/tags | 发送首楼 version 和 tagIds；成功用返回 version 更新编辑状态 |
| 关注与粉丝 | POST、DELETE /api/v1/users/{id}/follow；GET /me/following、/me/followers、/users/{id}/followers | 所有接口要求登录；关注与取消关注是设置目标状态，不是 toggle |
| 订阅按钮 / 偏好 | POST、PUT、DELETE /api/v1/threads/{tid}/subscribe、/forums/{fid}/subscribe、/tags/{tagId}/subscribe | POST 新建默认开启通知，PUT 保存完整偏好，DELETE 取消 |
| 我的订阅 | GET /api/v1/me/subscriptions?kind=thread&page=1 | kind 为 thread、forum、tag，默认 thread；每页 30 条；列表按当前资源可见性过滤 |
| 通知设置 | GET、PUT /api/v1/me/notification-preferences | 先读取再保存全部开关；全局开关会影响单项订阅投递 |
| 发起私信 | POST /api/v1/users/{id}/messages | 无需互关；首条成功后等待对方回复，不自动重发超时请求 |
| 会话列表 | GET /api/v1/me/conversations?page=1 | 每页 30 条，按最后消息时间和 ID 倒序；展示 unread、blocked、waitingForReply |
| 会话历史 | GET /api/v1/conversations/{cid}/messages?before={messageId} | 最新 50 条，ID 倒序；使用最小 ID 向前加载；读取本身不标记已读 |
| 已读 / 屏蔽 | POST /api/v1/conversations/{cid}/read；POST、DELETE /conversations/{cid}/block | 已读发送本会话 messageId；屏蔽是当前用户的目标状态 |

## 请求与重试

- 资源 ID 使用十进制字符串；不要转成 JavaScript Number。成功为 {data: ...}，按页列表另含 meta；消息历史没有 meta 或 nextCursor。页码默认 1，服务端限制在 1–10000，前端使用响应 meta 中的实际页码。
- 先 GET /api/v1/session 获取会话和 csrfToken。写入附会话 Cookie 和 X-CSRF-Token，建议 application/json；无字段动作发送 {}。不可发送空 JSON 请求体或 JSON null。
- 401 重新登录；403 按 error.code 区分权限、初始密码、禁言、CSRF 和屏蔽；404 也可能是资源不可见或并非会话成员；429 等待后再操作。
- 关注、取消关注、订阅 POST/DELETE、会话已读、屏蔽/解除屏蔽可重复设置相同目标状态；订阅 PUT 是覆盖偏好，避免并发覆盖别的设备修改。
- 标签编辑和主题标签绑定使用版本检查；409 后先刷新，不能盲目重放旧版本。标签创建和发送私信没有客户端幂等键，不自动重试；网络超时先查询结果。
- 订阅新建与修改、关注变更、发送私信和编辑标签要求完成初始密码修改；取消订阅、标记已读和屏蔽操作仍可用于管理已有内容。

## 标签字段与版本

标签 name 会转小写并合并空白，规范化后 1–32 个字符；slug 转小写并去首尾空白，只允许字母、数字、连字符，以字母或数字开头，最多 64 位。description 最多 500 字，color 为空或 #RRGGBB，status 为 active / disabled。创建时省略 status 默认为 active；更新必须发送 name、slug、status 和当前标签 version，省略 description / color 会将其清空。

修改 slug 会保留旧别名；旧别名不能分配给另一个标签。禁用标签仍可通过 ID 或旧 slug 查看详情，目录不再展示，不可新建或修改对它的订阅。threadCount 仅在标签目录 / 后台列表中计算可见公开主题数；详情、保存响应和主题附带标签里的 0 表示该投影未计算，不能当作真实统计。

主题最多绑定 8 个不重复的正整数标签 ID。已绑定的禁用标签可以保留，不可新增绑定；tagIds=[] 清空标签，当前省略 tagIds 也会清空，前端应始终明确发送数组。这里的 version 是首楼版本，不是标签自身版本；保存会增加首楼版本，并遵循首楼编辑权限和编辑时限。修改标签不会补发历史订阅通知。

## 订阅与通知

POST 新建订阅默认 enabled、notifyInApp、notifyEmail 全为 true，mutedUntil 为 null；重复 POST 保留已有偏好与原订阅时间，不会重新打开关闭的通知。PUT 是创建或覆盖，必须发送这三个布尔字段；mutedUntil 为 RFC3339 时间，省略或空字符串表示取消静音。响应中的 name 只在列表中提供；mutedUntil 始终存在，可以为 null。

主题订阅通知新回复，版块 / 标签订阅通知新主题。同一事件命中多项订阅会合并；不会通知内容作者自己。只面向事件发生时已订阅、投递时仍有权限且未关闭或静音的用户；内容待审期间暂缓，恢复公开后继续处理，后来订阅不会收到历史事件。取消订阅不要求继续拥有目标资源的阅读权限，隐藏后仍可取消；我的订阅不一定展示已隐藏的主题或版块，禁用标签订阅仍可能出现在列表中。

全局通知偏好包含 mentions、replies、acceptance、membership、titles、moderation、reports、email、subscriptions。PUT 除 subscriptions 外必须提供所有布尔字段；省略 subscriptions 保留原值以兼容旧客户端，新前端应保存全部字段。subscriptions 控制订阅通知，email 控制邮件，单项 notifyEmail=true 不代表邮件已经投递：还需要站点启用邮件、有效的用户邮箱、相关偏好以及邮件 Worker 正常处理。

## 私信与屏蔽

无互关要求。发起人只允许首条消息；接收方未回复时再发返回 409 / MESSAGE_REPLY_REQUIRED。接收方首次回复后永久解除此首条限制，但发送频率限制仍适用：每个发送者每分钟最多 30 条、滚动 24 小时最多发起 20 个新会话，并叠加 API 速率限制。body 经敏感词处理并去首尾空白后为 1–5,000 字。

waitingForReply 仅表示“我是发起人且尚未收到回复”；为 false 不保证一定能发。blocked 仅表示当前用户主动屏蔽此会话，不暴露对方的屏蔽设置。任意一方屏蔽都会拒绝双方继续发送，返回 403 / MESSAGE_BLOCKED；提示不应认定一定是对方屏蔽了自己。屏蔽还会解除双方关注；解除屏蔽仅清除自己的设置，不恢复关注，不清除首条等待状态，对方仍屏蔽时依然不能发。

只有会话参与者可读历史、标记已读或屏蔽，即使管理员也不能通过这些用户 API 读取他人会话。历史按消息 ID 倒序，before 是正整数且不包含该条；加载到空数组结束。前端按 ID 去重后可反转为聊天显示顺序。已读 messageId 必须来自该会话，较旧游标不会降低已读进度。unread 为对方发送、在我的已读游标之后且未删除的消息数。

会话已解锁后，重复发送相同文本会新增消息；超时先拉取会话历史，由用户决定是否重发。当前接口不提供私信附件、撤回、私信举报、私信专用 SSE 或邮件提醒；不能假设论坛内容通知会自动覆盖私信。

## 交付边界

本阶段完善已有后端接口的契约和接入流程，不创建完整前端，也不新增备份中心。备份、定时任务和恢复执行由部署者通过宝塔或自己的数据库脚本安排。验证使用一次性隔离数据库，定向覆盖社区主流程、权限及字段响应，不运行长压测或操作业务数据库。
