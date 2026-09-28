# 投票、积分悬赏、签到 API

2026-09-28，schema 18–20。第一版后端、会员权限、后台配置、退款 Worker 和 OpenAPI 已实现。前端页面、生产部署和容量验收不包含在本次交付中。

沿用纯 API 架构与独立积分账本；投票 → 悬赏 → 签到三项已完成，以下为实际代码规则。

## 通用约定

所有路径以 `/api/v1` 开头。写入使用 `application/json`、会话 Cookie 和 `X-CSRF-Token`，无业务字段的动作提交 `{}`；请求最大 1 MiB，拒绝未知字段。成功响应为 `{"data": ...}`，ID 为十进制字符串（投票选项 ID 为整数），时间为 RFC 3339，签到日为 `YYYY-MM-DD`。

帖子可见性、版块规则、封禁/禁言、初始密码修改和站点状态仍生效。新增会员动作默认每用户每动作 20 次/分钟，幂等重试仍检查当前权限、截止时间和限流。API 超时不代表事务未提交，先读取实际状态再决定重试。

## 投票

- 每主题最多一个投票，只允许公开、未锁定主题的作者创建。发主题与附加投票是两个 API 操作，不承诺一起成功。
- 问题与每个选项去除首尾空白后为 1–200 字符；选项至少 2 个，不可大小写重复。选项数和有效期受后台限制，创建后不可编辑或重开。
- `maxChoices=1` 为单选，较大值为多选，不能超过选项数。`durationHours` 为整数，1 至后台 `maxDays×24`；截止时间由数据库计算，从创建时开始，审核等待不会延长。
- 每个登录用户一次。相同选项（顺序不同也相同）重放不计新票，改投 409；选项、选票和人数同事务。截止、主题锁定/删除/待审、功能停用时拒绝新投票。
- 问题和选项使用现有敏感词与审核规则。`pending/rejected` 仅作者及有后台投票管理权限者可读；作者关闭待审投票转为 `rejected`，不会公开未审文本。
- 有权读取已发布投票的人都能看到匿名结果，不公开投票者名单。`myChoices` 只包含本人选择；账号删除时清除其关联选票，历史匿名汇总保留。

| 方法 | 路径 | 输入 / 返回 |
| --- | --- | --- |
| GET | `/threads/{tid}/poll` | 详情，无投票或不可见为 404 |
| POST | `/threads/{tid}/poll` | `question, options[], maxChoices, durationHours`；成功 201，重复创建 409 |
| PUT | `/threads/{tid}/poll/vote` | `optionIds: [1,2]`；200 返回当前投票 |
| POST | `/threads/{tid}/poll/close` | `{}`；作者提前关闭 |
| GET | `/admin/polls?before=123` | 待审摘要，50 条/页；审核前读取主题投票详情 |
| POST | `/admin/polls/{tid}/moderation` | `action: approve/reject/close`，可选 `reason`（最多 500 字），写审计 |

详情字段：`threadId, question, maxChoices, state, closesAt, createdAt, closed, voters, options, myChoices`；选项为 `{id,text,votes}`。状态为 `pending/published/closed/rejected`。`closed` 综合截止和主题状态，不包含全局功能开关。待审列表的 `options/myChoices` 为空、`voters` 为零，应先读取详情再审核。

## 积分悬赏

- 每主题最多一笔，取消后也不重新发布。仅公开、未锁定且未采纳答案的主题作者可创建，金额/有效期受后台限制，余额必须足够。
- 创建增加 `frozen` 而不减 `balance`，`available=max(balance-frozen,0)`。相同金额/时长重试返回原记录（包括终态），不同参数返回 409。
- 沿用作者采纳接口，不能采纳本人或首楼回复。采纳标记、冻结释放、作者扣款、答主入账同事务；按用户 ID 顺序锁定双方账户。
- 同一答案重放不重复支付。已支付后不可撤回或改付；后续删帖、审核不自动追回赏金。采纳获得的额外行为经验/积分仍走既有异步奖励，不等同于赏金。
- 作者仅在尚无公开有效回复时可提前取消。有回复则等待采纳、超时或后台取消；管理员需要 1–500 字非空原因并写审计。已退款重放成功，已支付退款返回 409。
- 超时或主题删除/物理清理后由 Worker 退款，只释放冻结。状态持久保存，启动后继续扫描，每分钟处理至多 100 条，失败退避；进程暂停期间不保证准时退款。停用功能仍处理存量结算/退款。
- `thread_bounties` 保留主题 ID，不级联删除金融记录。到期且尚未退款时拒绝支付；退款后仍可普通采纳。

| 方法 | 路径 | 输入 / 返回 |
| --- | --- | --- |
| GET | `/threads/{tid}/bounty` | 详情，无记录或主题不可见为 404 |
| POST | `/threads/{tid}/bounty` | `amount, durationHours`；首次和重放均 200 |
| POST | `/threads/{tid}/bounty/cancel` | `{}`；作者取消/领取到期退款 |
| PUT | `/posts/{pid}/acceptance` | `{}`；已有接口，现同时结算活动悬赏 |
| DELETE | `/posts/{pid}/acceptance` | `{}`；普通采纳可撤销，已支付悬赏 409 |
| GET | `/admin/bounties?before=123` | 活动悬赏，50 条/页，包括等待退款记录 |
| POST | `/admin/bounties/{tid}/cancel` | `reason`；退款与审计同事务 |

详情字段：`threadId, ownerId, amount, durationHours, state, closesAt, createdAt, settledAt, recipientId, postId, ruleVersion, note`。状态为 `active/awarded/canceled/expired`；未结算 `settledAt=null`、`recipientId/postId="0"`。退款扫描前可能仍为 `active`，界面还应检查 `closesAt`。后台列表目前不是全状态检索。

账本来源为 `bounty:<tid>:freeze/award/receive/refund`，类型 `bounty`。转账不改变总积分，冻结/退款仅改变冻结额。既有奖励冲回可产生欠额，结算沿用该债务模型；人工扣款仍不能突破可用余额。

## 签到

- 每用户每自然日一条记录，日期取数据库时钟和配置时区，默认 `Asia/Shanghai`。与 `/me/activity` 每日活跃独立，二者奖励可以分别获得。
- 签到记录、经验流水、会员状态、积分流水、自动升级同事务。重复/并发请求返回当日原回执，不重复奖励；之后改规则不修改已有奖励。
- 默认每天 5 经验 + 1 积分，均可设 0，零奖励也有回执。昨天签到则连续天数加一，中断后从 1 开始；今天未签时显示昨日连续天数，已中断则为 0。
- 时区须有效且不能为 `Local`。首次记录之后拒绝换时区，避免自然日倒退与重复领奖。保存历史时区、规则版本和奖励数值。
- 无补签、连续额外奖励和签到榜。历史记录可供前端展示日历，但没有独立月份聚合接口。

| 方法 | 路径 | 返回 |
| --- | --- | --- |
| GET | `/me/checkin` | `{day,timeZone,enabled,checkedIn,streak,checkin}`，未签时 `checkin=null` |
| POST | `/me/checkin` | 请求 `{}`；200 返回当日回执 |
| GET | `/me/checkins?before=2026-09-28` | 本人历史，30 条/页，严格日期游标 |

回执字段：`userId, day, streak, experience, points, ruleVersion, timeZone, createdAt`。经验/积分来源均为 `checkin:<日期>`，唯一性包括用户，类型 `checkin`，不自动冲回。停用后已完成的当日签到仍返回原回执，权限及账号状态仍须通过校验。

## 后台配置与权限

`GET /admin/engagement/config` 读取；`PUT /admin/engagement/config` 保存完整对象与当前 `version`。成功版本加一，与审计同事务；过期版本返回 409，应重新读取合并。

```json
{
  "version": 1,
  "poll": {"enabled": true, "maxOptions": 10, "maxDays": 30},
  "bounty": {"enabled": true, "minPoints": 1, "maxPoints": 10000, "maxDays": 30},
  "checkin": {"enabled": true, "experience": 5, "points": 1, "timeZone": "Asia/Shanghai"}
}
```

- `maxOptions`：2–20；`maxDays`：1–365；悬赏金额：1–1000000 且最小值不超过最大值；签到经验/积分：0–10000。
- 停用投票阻止新建/参与；停用悬赏只阻止新建；停用签到阻止新领取。历史读取、关闭、审核、退款保留。
- 会员动作：`poll.create`、`poll.vote`、`bounty.create`、`checkin.claim`。迁移补齐缺失项：创建类继承 `thread.create`、投票参与继承 `post.reply`、签到默认开启；已有明确布尔值保留。
- 后台权限：读取 `engagement.view`、保存 `engagement.configure`、投票管理 `polls.manage`、悬赏管理 `bounties.manage`；还须 `admin.panel`。默认管理员拥有，普通会员/版主不拥有，可用角色矩阵配置。
- 会员矩阵现在需完整 17 项权限。互动配置独立版本化，不混入站点 settings 或行为积分 rules。当前没有公开互动配置接口，前端通过自身权限、详情/状态及操作结果判断，不能调用管理员接口作为公开配置入口。

## 错误与分页

401 `UNAUTHENTICATED`；403 `FORBIDDEN/MEMBER_PERMISSION_DENIED/CSRF_INVALID`；404 `NOT_FOUND`；409 `ENGAGEMENT_CONFLICT/ENGAGEMENT_CLOSED/POINTS_INSUFFICIENT`；415 `UNSUPPORTED_MEDIA_TYPE`；422 `VALIDATION_FAILED/ENGAGEMENT_INVALID`；429 `RATE_LIMITED`；503 `ENGAGEMENT_UNAVAILABLE` 或请求超时/站点状态拒绝。

沿用的采纳接口普通冲突为 `TITLE_CONFLICT`，内部失败仍为 500 `INTERNAL_ERROR`。列表返回 `data: {items,nextBefore}`，游标为空表示无后续页。后台按主题 ID 降序且满 50 条就返回游标，下一页可能为空；签到按日期降序，多读一条判断下一页。

## 验证及边界

只在一次性 `gobbs_test_` 集群定向回归：投票权限/审核/并发幂等，悬赏冻结/支付/退款/故障回滚/对账，签到并发/连续天数/回滚/时区限制均通过。历史迁移、新库、schema 17→20 配置保留及重复启动已验证；证据见 [测试门禁](TEST_GATE.md)。

本轮没有全套业务门禁、持续压测、生产库迁移或部署；事务回滚验证不等同于真实 Worker 强杀/数据库断连演练。没有改投、赏金追加/分摊/仲裁、补签/连续额外奖励、全状态后台检索、对应前端页面或公开多维排行榜。
