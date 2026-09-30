# 积分账户与账本

schema 13 建立账户与账本，schema 19–20 已接入悬赏和签到。积分与会员经验分别记账；等级、升级与权限仍使用经验。用户任意转账及购买未开放，悬赏转账只由作者采纳结算。

## 奖励规则

| 行为 | 默认积分 | 每日积分上限 | 经验 |
| --- | ---: | ---: | --- |
| 发主题 | 1 | 10 | 沿用会员配置，默认 5 |
| 发回复 | 1 | 20 | 沿用会员配置，默认 2 |
| 被点赞 | 1 | 20 | 沿用会员配置，默认 1 |
| 设为精华 | 5 | 25 | 沿用会员配置，默认 20 |
| 回复被作者采纳 | 10 | 50 | 新增默认 30，每日 150 |
| 每日活跃 | 默认关闭 | 0 | 沿用会员配置，默认 1 |
| 每日签到 | 1 | 每天一次 | 默认 5，独立互动配置 |

此前讨论的发帖 10 经验、回复 3 经验是建议值。本次保留已有经验配置，管理员可以继续在会员配置中调整；新增采纳经验在积分配置的 `acceptedExperience` 中调整。每日活跃是已有 `/me/activity` 记录，不是签到。

各类积分使用 `enabled`、`points`、`dailyCap`、`reverse` 配置。`points` 范围 0 至 10000，`dailyCap` 范围 0 至 1000000，允许设置为 0；每日上限按 Asia/Shanghai 自然日和业务发生时间计算，冲回不会重新开放当天已用额度。

事件创建时保存积分和经验规则快照。后台改规则只影响之后的事件，重启重试不会套用新数值。原有成长 worker 在同一数据库事务内发放经验、积分并删除事件，任一写入失败均回滚。已启用 worker 的 `forumd` 无需新增进程或环境变量。

待审核内容不奖励，通过审核后才奖励；自己点赞不给自己奖励。取消点赞、撤销采纳、取消精华及删除内容，按原奖励的 `reverse` 设置追加等额负流水。零奖励也记录回执，同一业务来源不可重复发奖，恢复或重新点赞不会再次领到奖励。

升级时不自动补发历史积分。旧经验来源、已有非待审核帖子、点赞、精华和采纳建立零额基线，不能通过反复操作领取新积分；升级前已经排队的成长事件也不会补发积分。迁移只创建一次基线，重复启动不重置余额或自定义配置。

## 账户与流水

`balance` 是净余额，`frozen` 是冻结额，`available=max(balance-frozen,0)`，`debt=max(frozen-balance,0)`。悬赏创建增加 `frozen`；退款只释放冻结；结算同时减少作者 `balance/frozen` 并增加答主 `balance`。业务冲回可以产生欠额：例如先得到 1 积分，管理员扣除该积分后帖子又被删除，余额变为 -1；以后赚取 2 积分，净余额回到 1。人工扣减禁止超过可用余额。

账户行锁串行化同一用户的调账和发奖，账户写入与流水、审计同事务提交。流水保存业务来源、增减额、变更后余额、规则版本、操作方、原因及业务/入账时间，数据库触发器拒绝 UPDATE 和 DELETE。修正采用新的调整流水，不覆盖历史记录。

账号只读对账比较账户余额/冻结额与流水合计，返回 `consistent`。不提供自动覆盖余额的“修复”接口。含积分流水的账号禁止物理删除，匿名化注销仍是后续功能。

## 用户 API

需登录，仅返回本人账户，不提供公开用户余额或排名。

- `GET /api/v1/me/points`：返回字符串 `userId`、整数 `balance`、`frozen`、`available`、`debt`、`version`。尚未发生积分事件的用户返回 0 和版本 1，不因 GET 创建账户。
- `GET /api/v1/me/points/ledger?before=123`：按流水 ID 降序，每页 50 条，返回 `items`、`nextBefore`；无下一页时游标为空字符串。隐藏迁移零额基线，保留业务零奖励记录。

流水 ID 与 userId/actorId 使用字符串。`kind` 为 `thread`、`reply`、`like`、`digest`、`accepted`、`active`、`admin`、`bounty`、`checkin`；冲回来源为 `reverse:<原来源>`，`ruleVersion` 保留原奖励版本。`createdAt` 是入账时间，`eventAt` 是业务发生时间。

## 后台 API

默认仅管理员拥有以下权限，继续通过角色权限矩阵配置。写入要求 JSON 和 CSRF。

| 方法 | 路径 | 权限 |
| --- | --- | --- |
| GET | `/api/v1/admin/points/config` | `points.view` |
| PUT | `/api/v1/admin/points/config` | `points.configure` |
| GET | `/api/v1/admin/points/users/{uid}` | `points.view` |
| GET | `/api/v1/admin/points/users/{uid}/ledger?before=123` | `points.view` |
| GET | `/api/v1/admin/points/users/{uid}/reconcile` | `points.view` |
| POST | `/api/v1/admin/points/users/{uid}/adjust` | `points.adjust` |

配置保存使用 GET 返回的完整对象：`version`、包含六类行为的 `rules`、`acceptedExperience`。乐观锁检查版本，成功返回递增后的版本，过期版本返回 409。保存写入 `points.configure` 审计。

调账示例：

```json
{"version":1,"delta":10,"reason":"活动奖励","key":"campaign-2026-001"}
```

单次 `delta` 为 -1000000 至 1000000 的非零整数，`reason` 为 1 至 200 个非空字符且不含控制字符，`key` 为 8 至 80 位 ASCII 字母、数字、下划线或连字符。相同操作方、用户和 key 重复提交相同 delta/reason 返回成功但不重复入账，重复提交无须更新原版本号；改动请求内容或新的请求使用旧版本返回 409。成功记录 `points.adjust` 审计。`points.adjust` 不授予配置修改权限。

错误码：`POINTS_INVALID`（422）、`POINTS_CONFLICT`（409）、`POINTS_INSUFFICIENT`（409）、`NOT_FOUND`（404）、`POINTS_UNAVAILABLE`（503）。

## 悬赏与签到账本

`bounty:<tid>:freeze/award/receive/refund` 为悬赏唯一业务来源，`ruleVersion` 来自创建时互动配置；冻结/退款只改变 `frozenDelta`，结算双方 `delta` 合计为零。采纳与转账同事务，退款重复执行不重复入账。后续删帖不自动冲回已支付赏金。

签到来源 `checkin:<日期>`（唯一键包括用户），类型 `checkin`。记录、经验、积分和升级同事务，保存原奖励和互动规则版本，零额也有回执。签到配置位于 `/admin/engagement/config`，不加入六类异步行为奖励 `rules`；首次记录之后不可换时区。

## 后续范围

schema 17 已有积分 Top 100 小时快照和管理员读取接口 `/api/v1/admin/analytics/points`；公开积分余额榜已通过 `/leaderboard/points` 提供，后台报表与积分配置页面也已接入。热门作者、热门话题、日榜/周榜等多维榜单、用户排名查询和榜单规则配置仍未实现。悬赏冻结/结算/退款和超时任务、每日签到记录/连续天数/经验积分奖励已实现，详见 [互动 API](ENGAGEMENT_FEATURES.md)。补签、连续额外奖励、积分交易和用户任意转账仍未实现。
