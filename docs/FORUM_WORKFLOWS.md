# 论坛基础流程 API

2026-09-06，schema 8。补齐通知、审核反馈、草稿标题和楼层回复；仅实现 Go API，Nuxt 页面另行开发。迁移文件为 `008_forum_workflows.sql`，保留原有草稿正文、通知及已读状态。本轮不部署现有网站或操作业务库。

## 1. 通知中心

所有路径前缀为 `/api/v1`。个人接口需会话，写操作需 `X-CSRF-Token`，响应沿用 `data` / `meta`。ID 为字符串。

| 方法 | 路径 | 参数或返回 |
| --- | --- | --- |
| GET | `/me/notifications` | `page=1`，可选 `unread=true/false`；每页 30 条，最新在前，含 meta |
| GET | `/me/notifications/summary` | `data.unread` 为可见未读数 |
| POST | `/me/notifications/read` | `{"ids":["123","124"]}` 或 `{"all":true}`；最多 100 个 ID；返回 read、updated |
| GET | `/me/notification-preferences` | 返回全部八项布尔偏好 |
| PUT | `/me/notification-preferences` | 必须提交全部八项布尔值，缺项返回 422 |

GET 不修改已读状态。空对象不再代表全部已读，须显式提交 `all=true`；不能同时传 all=true 和非空 ids。只能标记自己的当前可见通知，其他账号或隐藏通知 ID 不产生更新。

```json
{
  "mentions": true,
  "replies": true,
  "acceptance": true,
  "membership": true,
  "titles": true,
  "moderation": true,
  "reports": true,
  "email": true
}
```

全部默认开启。关闭某类偏好仅影响之后产生的通知，不删除历史通知；email 仅控制已有 @/回复邮件，不影响验证、找回密码邮件。新系统事件首版只发站内通知。

通知字段：id、fromUserId、fromName、type、threadId、postId、excerpt、read、createdAt、scope、payload。无关联 ID 为字符串 `"0"`。excerpt 为纯文本，前端须按文本展示。

| type | 触发 | 偏好 | scope / payload |
| --- | --- | --- | --- |
| `mention` | 公开内容 @ 提及 | mentions | content / {} |
| `reply.direct` | 回复指定楼层 | replies | content / {} |
| `reply` | 主题收到回复 | replies | content / {} |
| `reply.accepted` | 作者采纳他人回复 | acceptance | content / {} |
| `title.granted`、`title.revoked` | 获得/撤销称号 | titles | account / titleId |
| `membership.upgraded` | 自动升级等级 | membership | account / levelId |
| `moderation.approved`、`moderation.rejected` | 审核通过/拒绝 | moderation | account / status |
| `report.resolved`、`report.dismissed` | 举报处理/驳回 | reports | account / reportId |

- content 通知要求关联楼层、主题都未删除、已公开，且接收者现在有版块访问权。列表、总数、未读数、已读操作使用相同过滤。
- account 通知仅属于接收者，关联内容删除或失去版块权限后仍可查看简要处理结果；不携带其他作者的隐藏正文。链接访问仍由内容 API 鉴权，404 时前端保留处理结果即可。
- 同一楼层对同一接收者只生成一条发布通知，优先顺序为 @、指定回复、主题回复。不通知自己；重复审核/编辑后的再次公开不重复发同一提醒。取消后再次采纳同一回复不会重复提醒。
- 审核通过主题时，同时通知首次公开的首楼 @ 和回复。审核通过回复时通知目标作者及楼主。
- 新系统事件与审核、采纳、会员/称号日志共享数据库事务。持久化成功后，普通 @/回复才投递邮件和 SSE；重复请求不重复投递。
- 现有历史通知保留；本次不为历史业务补发系统消息。发布通知的 event_key 去重从新版本写入开始，旧版记录不追溯合并。

前端登录/重连/回到页面时拉取 summary；通知页按需分页。收到现有 `notify` SSE 重新拉取。称号后台任务等系统通知已持久保存，但本版没有跨进程实时广播；前端可每 30 秒刷新 summary，进入通知页或相关操作后刷新列表。SSE 不是可靠消息存储，邮件仍沿用现有进程内队列。

## 2. 楼层回复和定位

`POST /threads/{tid}/posts` 新增可选 `replyToPostId`，未传或 `"0"` 仍为普通回帖。目标必须在同一主题、未删除、已公开；不可见/跨主题返回 404，无效 ID 返回 422。关系在发布时确定，编辑正文不改变关系。

`GET /posts/{pid}`、`GET /threads/{tid}/posts` 新增：

```json
{
  "viewerHasLiked": false,
  "replyTo": {
    "id": "123",
    "available": true,
    "floor": 2,
    "authorId": "8",
    "authorName": "example"
  }
}
```

普通回帖 replyTo=null；目标隐藏后只返回 id 和 available=false，不能展示此前缓存的用户名/引用内容；目标物理清理后关系为空。列表批量查询个人状态。

`GET /posts/{pid}/position` 返回 postId、threadId、floor、page、pageSize。按当前用户实际可见楼层数量定位，包含作者自己的待审楼层及有权版主可见楼层；不能用 floor / pageSize 替代。通知、引用跳转应先调用此接口，再加载对应页；权限变化或楼层删除返回 404。主题详情仍未增加继续阅读位置或只看楼主模式。

## 3. 草稿和本人内容

草稿保存 `POST /me/draft` 新增 subject，最多 80 个 Unicode 字符。context 必须为 `new:正整数版块ID`、`reply:正整数主题ID`、`edit:正整数楼层ID`，ID 为无前导零的十进制。reply 草稿不接受非空标题。content 和 subject 同时为空才删除，允许只保存标题。每个用户每个 context 一份；保存为全量覆盖，前端每次提交两字段。

`GET /me/draft?context=...` 和 `GET /me/drafts` 均返回 subject、content、context、updatedAt。草稿是用户私有暂存，context 不代表资源授权；真正发布/编辑仍通过内容权限校验。

`GET /me/content?kind=threads&status=all&page=1`：

- kind=threads/replies，默认 threads；status=all/published/pending/rejected/deleted，默认 all；每页 30 条。
- 返回 id（楼层 ID）、threadId、forumId、floor、subject、content（本人 Markdown）、status、moderationNote、parentAvailable、createdAt。
- 仅查询当前会话作者的内容，继续应用版块可见范围；不接受 userId 指定其他用户。
- 他人主题隐藏/删除后，本人回复仍可按状态查看，但父主题标题隐藏。本人主题标题可以查看；不返回敏感词内部命中规则。
- rejected 是本版本审核拒绝形成的软删除；普通删除为 deleted。旧版已删除内容没有拒绝审计字段，统一按 deleted 展示，不能猜测旧拒绝原因。
- published 表示当前公开，pending 表示自己或父主题待审；parentAvailable 表示父主题现在公开且未删除。公共资料页仍只显示公开内容。

审核接口继续用 `op=approve/delete`，新增可选 note（最多 500 字）；拒绝未填则使用默认说明。首楼使用主题审核接口，回复接口不审核首楼。审核结果不是自动重新投稿功能；被拒内容暂可读取并复制重新投稿。

## 4. 回归边界

本轮新增测试覆盖删楼后的收藏未读、待审/删除过滤、重复和并发审核、通知分页与跨用户已读隔离、偏好、系统事件、楼层定位、隐藏目标、本人待审/拒绝内容、草稿标题与 schema 7 升级保留数据。实际验证结果在开发交付说明中记录。

未新增标签、订阅、私信、屏蔽、只看楼主、回复回收站、设备管理和可靠邮件队列；未完成 Nuxt 页面或 SMTP 实际投递验收。完整待办见 [功能核查](FORUM_FEATURE_AUDIT.md)。
