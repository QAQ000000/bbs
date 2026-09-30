# 任务称号与作者采纳 API

2026-09-30。后端已实现；Next.js 称号展示和后台页面已接入。称号引入迁移 007；后续基础流程使用 schema 8，加入称号/采纳站内通知，见 [基础流程 API](FORUM_WORKFLOWS.md)。生产部署与外部邮件验收另计。

## 1. 模型和范围

会员等级负责成长权益，称号独立记录任务成就，不授予业务或管理权限。用户可持有多个称号，同时佩戴一个，也可不佩戴。

后台创建草稿，配置条件，预览符合人数，发布后由后台任务自动发放。人工称号由管理员直接授予。没有经验前置要求，经验仅是可选指标。

首版支持累计有效业务指标、首次成就（门槛 1）、同一楼层获赞、指定版块、全部/任意条件、历史补发、限时发放、永久或限时有效、人工授予/撤销、佩戴及审计。不提供任意 SQL/脚本、嵌套表达式、排行榜、连续签到、滚动周期任务、收藏任务和自动收回的资格称号。

## 2. 条件和统计口径

所有条件均为 `current >= target`，最多 10 条；`match=all` 要求同时满足全部条件，`any` 任一满足。条件来自有效业务数据，不从经验流水推导；每天经验封顶不影响发帖、点赞、采纳任务。

| metric | 口径 | 版块范围 |
| --- | --- | --- |
| `threads_created` | 作者有效首楼数量 | 支持 |
| `replies_created` | 作者有效回复数量，不含首楼 | 支持 |
| `likes_received` | 有效楼层获得他人点赞总数 | 支持 |
| `featured_threads` | 作者当前有效且为精华的主题数量 | 支持 |
| `accepted_replies` | 作者当前有效且被主题作者采纳的回复数量 | 支持 |
| `post_likes_max` | 作者单个有效楼层的最多他人点赞数 | 支持 |
| `experience` | 当前经验余额 | 仅全站 |
| `active_days` | 累计活跃天数，非连续签到 | 仅全站 |
| `registered_days` | 注册后经过的完整 24 小时天数 | 仅全站 |
| `email_verified` | 已验证为 1，否则 0；门槛只能为 1 | 仅全站 |

- `forumId="0"` 或省略为全站；不同条件可以配置不同版块。
- 有效内容要求楼层及所在主题都未删除、审核通过。版块访问等级限制不会使内容失去业务有效性。
- 自赞排除；每人每楼层只算一次，同一人对多个楼层点赞分别计数。不是独立点赞人数。
- 取消点赞/精华/采纳，或内容删除/待审，会减少下一次评估的计数；反复取消恢复不会累计额外次数。
- 按评估时有效状态判断；短暂达标后在处理前取消，不保证获得称号。首版不把所有历史瞬间状态当作“曾经达成”。
- 后台计算并持久化进度，GET 只读取快照。普通访问和爬虫不会增加活跃或触发发奖。
- 既有有效帖子、精华和点赞可参与历史补发；采纳是新增业务，没有旧采纳数据可导入。

示例：首次回复 `replies_created >= 1`；热心答疑者 `accepted_replies >= 10`；优质创作者 `threads_created >= 50 AND featured_threads >= 5`。

## 3. 定义、状态和有效期

创建草稿示例，ID 使用字符串，版本及数量使用数字：

```json
{
  "id": "0", "version": 0,
  "name": "热心答疑者", "description": "在问答版块获得 10 次作者采纳",
  "badge": {"label": "答疑", "icon": "star", "color": "#166534", "background": "#dcfce7"},
  "status": "draft", "mode": "automatic", "match": "all",
  "conditions": [{"metric": "accepted_replies", "target": 10, "forumId": "1"}],
  "startsAt": null, "endsAt": null,
  "durationDays": 0, "expiresAt": null, "sort": 0
}
```

- 最多 200 个称号；名称 1–30 字，说明最多 500 字，标签最多 20 字；排序 0–10000。
- `automatic` 必须有条件；`manual` 必须使用空条件数组。
- `draft`：不对用户展示；创建只能是草稿，发布后不能退回草稿。
- `active`：开放获取和佩戴。
- `paused`：停止自动及人工发放，已获得的仍允许展示/佩戴。
- `disabled`：停止发放及展示，清除佩戴，保留持有与审计记录。没有物理删除 API。
- `startsAt` / `endsAt` 为发放窗口，开始包含、结束不包含，使用 RFC3339 时间。**不限制贡献发生时间**，窗口开启后历史达标用户也可获奖。
- `durationDays=0` 且 `expiresAt=null` 为永久；正数表示授予后固定天数；`expiresAt` 为固定到期时间，两者互斥。
- 修改有效期仅影响之后的授予，不修改已经获得的到期时间。
- 徽章只允许预设图标（空、seedling、star、crown、shield、gem）和六位十六进制颜色。名称/说明/标签按普通文本渲染，不接收 HTML/CSS/SVG。

## 4. 发放、补发和撤销

1. POST 创建草稿，取得 ID 和 version。
2. 编辑完整定义并设为 active，POST preview 查看 eligible 和 newAwards。
3. PUT 完整定义，版本必须匹配；返回新版本并创建按用户 ID 分批的补发任务。
4. GET jobs 查看处理数量、该任务新发放数量及状态。业务事件也可先发放，所以任务 awarded 不等于所有途径发奖总数。

预览是只读快照和估算，不冻结业务数据，也不返回会员模块的 previewToken。eligible 是当前未受限且满足条件的用户数；newAwards 还要求候选配置当下开放发放、该用户从未持有称号。实际发放重新检查当时状态。保存不强制先预览。

每次变更递增版本，旧任务变为 superseded，新活动自动称号重新创建任务。老用户无需再登录或贡献一次。提高门槛不会撤销已经发放的成就称号。

用户+称号唯一键保证自动发放一次；重复事件、并发消费者或重启不会重复发奖。到期后仍达标也不自动续期。管理员可使用新请求键明确重新授予/续期。

人工撤销保留 revoked 记录并清除佩戴，之后不会自动补发；即使未获得，也可建立撤销记录阻止未来自动发放。解除限制使用明确的人工授予。

封禁/禁言期间暂停自动发放，解除后重新评估；管理员明确授予可处理例外。过期/停用称号在读取和佩戴时立即失效，不依赖后台清理及时性。

## 5. 作者采纳

- 每主题最多采纳一条他人的有效回复，首楼和自己的回复不能采纳。
- 只允许主题作者操作，要求角色权限 `reply.accept`；管理身份不会代替作者。默认三个角色均开放该权限。
- 登录、强制改密、账号状态、CSRF、资源可见性和主题锁定均参与检查；锁定主题不能采纳/取消。
- 重复采纳同一回复、重复取消已取消回复均成功，不重复写日志。更换时先取消原采纳；并发采纳不同回复只有一个成功，另一个 409。
- 删除或重新送审已采纳楼层/主题会清除采纳；恢复内容不恢复采纳。
- 采纳变化与称号事件在同一事务提交，回滚不产生进度。
- 主题详情返回 `acceptedPostId`（无采纳为 `"0"`），楼层返回 `accepted` 和 `capabilities.canAccept/canUnaccept`。
- 采纳不奖励经验，不修改现有经验规则，也不自动锁定主题。

## 6. API 清单

使用现有 JSON envelope。所有新增写接口要求 JSON、Cookie 会话和 `X-CSRF-Token`；取消采纳的 DELETE 也发送 `{}`。

| 方法 | 路径 | 用途 / 权限 |
| --- | --- | --- |
| GET | `/api/v1/titles` | 已发布未停用定义；含不可访问版块条件的条目不在公开列表展示 |
| GET | `/api/v1/me/titles` | 本人称号、持有状态、进度、到期时间 |
| PUT | `/api/v1/me/title` | 佩戴 `{"titleId":"3"}`；取消 `{"titleId":"0"}` |
| PUT | `/api/v1/posts/{pid}/acceptance` | 采纳，JSON `{}` |
| DELETE | `/api/v1/posts/{pid}/acceptance` | 取消采纳，JSON `{}` |
| GET | `/api/v1/admin/titles` | 完整定义及指标清单；`titles.view` |
| POST | `/api/v1/admin/titles` | 创建草稿；`titles.configure` |
| POST | `/api/v1/admin/titles/preview` | 预览候选完整定义；`titles.configure` |
| PUT | `/api/v1/admin/titles/{titleId}` | 保存/发布完整定义；`titles.configure` |
| GET | `/api/v1/admin/titles/{titleId}/jobs?page=1` | 补发任务，30 条/页；`titles.view` |
| GET | `/api/v1/admin/titles/{titleId}/logs?page=1` | 配置及发放撤销日志，30 条/页；`titles.logs` |
| GET | `/api/v1/admin/users/{uid}/titles` | 用户持有及进度；`titles.view` |
| PATCH | `/api/v1/admin/users/{uid}/titles/{titleId}` | 调整；`titles.view` 加 `titles.grant` 或 `titles.revoke` |

后台还要求 `admin.panel`。新增权限纳入完整权限矩阵，前端必须按最新权限点生成保存字段。

人工调整示例，version 为称号定义版本：

```json
{"action":"grant","version":2,"reason":"确认历史贡献","key":"support-ticket-title-001"}
```

action 为 grant/revoke；原因非空，最多 500 字节；key 为 8–100 字符，同操作者全局唯一。相同 key 和请求重试成功且不再次授予/续期；同 key 不同请求、旧版本返回 409 `TITLE_CONFLICT`。

其它状态：无会话 401；无权限/不可佩戴 403；不可见内容 404；非法字段/状态/条件 422；限流 429。佩戴、采纳每分钟 20 次；预览每分钟 5 次。

## 7. 前端契约

会话、本人/公开用户资料、主题列表/详情、楼层列表/详情返回作者的 `equippedTitle`。无有效佩戴为 null。列表按作者批量获取，不逐楼层查询。

```json
{"equippedTitle":{"id":"3","name":"热心答疑者","badge":{"label":"答疑","icon":"star","color":"#166534","background":"#dcfce7"}}}
```

`/me/titles` 每项包含完整 title、status、source、earnedAt、expiresAt、earnedVersion、equipped、counts、checkedAt、progressPending。

- status：in_progress（自动未获得）、not_earned（人工未获得）、earned、expired、revoked。
- `counts[i]` 对应 `title.conditions[i]`，可以超过门槛；已获得不等于当前计数达标。
- progressPending 表示尚无当前规则版本的进度，此时 counts 为空；非 pending 计数仍是 checkedAt 的快照，可能存在队列延迟。
- 人工称号无条件进度；停用称号保留在本人记录，但不可佩戴。
- 个人导出包含称号；其他用户只展示佩戴摘要，不公开任务明细。
- 首版没有新增称号/采纳 SSE 事件。操作后刷新详情，后台主动查询补发进度。

## 8. 数据和运行

titles 保存版本定义；user_titles 保存资格及撤销记录；title_equipment 保存单个佩戴；title_progress 保存评估快照；title_logs 保存规则版本、发奖依据和人工请求凭据；title_events 为独立队列；title_jobs 保存补发游标；title_schedule 保存复查时间。accepted_replies / acceptance_logs 保存业务采纳和作者操作日志。

业务触发器在原事务内写独立称号事件，不竞争会员队列。forumd 每秒一批，最多消费 50 条事件并处理一个补发任务的 50 个用户；失败回滚，保留事件和游标，重启继续。任务按 ID 顺序推进，失败原因记录在服务日志。

每小时复查注册时长、发放窗口、禁言自然到期等无用户操作的变化。窗口应留出批量处理时间；首版不是精确到秒的活动结算系统。

面向当前小型论坛，在后台按用户/版块计算业务聚合，一次用户评估复用同范围结果；GET 不扫描业务表。不引入 Redis、消息队列或通用任务引擎。规模增大时可改为增量统计和分区任务，保持 API 口径。

迁移 007 可重放，不重置称号、经验或会员等级。测试必须使用 `gobbs_test_` 前缀的独立数据库，API/store/migration 使用不同数据库，禁止对业务库运行会重建 schema 的测试。

## 9. 验证记录

本轮 37 项 store、35 项 API、2 项迁移测试在三个独立测试库通过，均启用 `-race`；`go vet ./...`、构建通过。未配置测试 DSN 时集成测试明确跳过；修复上传测试辅助函数缺少隔离库检查导致的空指针，普通 `go test ./...` 也通过。

覆盖历史补发、零经验任务达标、all/any 和版块范围、待审/删除/自赞排除、进度减少但成就保留、单楼层获赞、过期/停用即时隐藏、撤销防补发、人工幂等、配置冲突、事务回滚、并发工作者、定时复查、封禁状态、采纳归属/并发/删除、CSRF、角色权限、读取限制及 schema 6 升级和重复启动保持状态。

新二进制的独立 HTTP 验证通过空库安装、用户注册、发帖/回复/点赞/采纳、称号预览和历史补发、真实后台工作者自动授予、游客展示、重启保留佩戴、取消采纳、幂等撤销及禁止自动补发。临时服务、三个测试数据库和专用测试角色均已清理；现有网站仍返回 200 HTML，未替换线上二进制或执行业务库迁移。
