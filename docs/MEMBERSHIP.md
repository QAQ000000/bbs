# 会员等级、成长与权限 API

2026-09-30。状态：后端与 Next.js 会员展示、会员后台配置页面已实现；生产部署和容量验收另计。本文件仍以 API 与数据规则为主。

## 1. 模型与默认行为

管理角色继续使用 `users.group_id`（会员 / 版主 / 管理员）；会员成长使用 `member_states`。自动升级、等级配置和经验调整不能授予管理权限。

默认采用五级经验成长体系，直接选择符合条件的最高等级：

| ID | 名称 | 经验要求 |
| --- | --- | --- |
| 0 | 新手会员 | 0 |
| 1 | 正式会员 | 100 |
| 2 | 活跃会员 | 500 |
| 3 | 资深会员 | 1500 |
| 4 | 核心会员 | 5000 |

以上为可修改的初始配置。默认升级只看经验，不要求活跃天数、阅读量、发帖量或邮箱验证；后台可以增加这些必要条件，配置后各条件须同时满足。已有访问、阅读、发帖统计不能替代经验要求。默认 LV1 开放外链直接发布，LV2 开放免常规审核；具体权限仍以后台配置为准。

- 等级 ID 稳定，与排序 `rank` 分开。0 是默认等级，必须保留且不设门槛。
- `automatic=false`：不作为自动升级目标，仍可人工指定。
- 自动评估只升级，不因经验撤销、长期不活跃或提高门槛自动降级。
- `locked=true`：暂停自动升级；管理员可指定等级并锁定。封禁/禁言期间也暂停自动升级。
- 人工只调整经验或解除锁定时，会重新评估升级；明确指定 `levelId` 时保留本次指定结果。未锁定的人工等级可被后续自动升级覆盖。
- 等级唯一来源是 `member_states.level_id` 与版本化会员配置。数据库移除 `trust_level`，用户模型、公开 API、管理 API 和个人导出均不再返回旧信任等级字段，也不进行双写。资料和后台用户列表使用 `level`，个人导出使用 `membership`。

## 2. 会员权限与额度

等级权限必须完整提供下列 17 个布尔项，不接受任意权限名：

| 权限 | 含义 |
| --- | --- |
| `forum.read` | 阅读版块及其中主题 |
| `thread.create` | 创建主题 |
| `poll.create` | 为本人主题创建投票 |
| `poll.vote` | 参与投票 |
| `bounty.create` | 创建积分悬赏 |
| `checkin.claim` | 领取每日签到奖励 |
| `post.reply` | 回复主题 |
| `post.edit` | 编辑本人内容，另检查角色及编辑时限 |
| `post.delete` | 删除本人内容，另检查角色权限 |
| `post.like` | 点赞他人内容 |
| `thread.favorite` | 收藏 / 取消收藏 |
| `post.report` | 举报他人内容 |
| `upload.image` | 上传图片 |
| `upload.file` | 上传文件 |
| `attachment.download` | 访问已挂载的附件 |
| `post.link.direct` | 外链不触发新用户审核；邮箱验证要求仍适用 |
| `post.skip.moderate` | 免常规发帖审核 |

schema 18 迁移保留已有权限值并补齐缺失动作：投票/悬赏创建继承 `thread.create`，投票参与继承 `post.reply`，签到默认开启。签到奖励参与现有升级事务，规则见 [互动 API](ENGAGEMENT_FEATURES.md)。

游客单独使用 `guestPermissions`，只包含 `forum.read` 和 `attachment.download`。默认允许游客访问公开内容。限制某个会员等级不会自动改变游客规则；需要仅会员可见时，配置版块的 `membersOnly` 或提高 `minimumLevel`。

每个等级的 `limits`：

| 字段 | 单位 / 语义 |
| --- | --- |
| `threadsPerDay` | 每日成功创建主题数，包括进入审核队列的主题 |
| `repliesPerDay` | 每日成功回复数，包括待审核回复 |
| `uploadsPerDay` | 每日图片与文件上传合计次数 |
| `uploadBytesPerDay` | 每日上传字节总量 |
| `imageBytes` / `fileBytes` | 单张图片 / 单个文件最大字节数 |
| `attachmentsPerPost` | Markdown 中本站附件地址去重后的数量 |
| `editMinutes` | 从原始发布时间起的编辑时限 |
| `signatureLength` | 签名 Unicode 字符数 |

额度统一为 `-1` 不增加等级限制、`0` 禁止、正数限制。站点上传开关、磁盘容量、全局文件大小、签名 200 字硬上限和短时间防刷限制仍然生效，取更严格的限制。外链图片不计入本站附件数量。

每日统一按 `Asia/Shanghai` 自然日。次数 / 字节通过 PostgreSQL 条件更新原子预留，未提交业务事务的失败响应退回额度；业务已提交但后续读取失败时保留额度。进程在请求中崩溃时，未退回的预留保守计入当日使用量。删除帖子、撤下附件不会补回已使用的每日额度。

## 3. 版块规则与权限顺序

配置示例（`forumId` 是字符串）：

```json
{
  "forumId": "12",
  "minimumLevel": 2,
  "membersOnly": true,
  "denied": ["upload.file", "post.skip.moderate"]
}
```

版块最低等级比较 `rank`；`denied` 只收紧会员权限，不授予新权限。没有版块规则时，使用站点和等级规则。

判断顺序：资源可见性 → 账号状态 → 等级权限与版块限制 → 本人 / 管辖范围 → 主题锁定、编辑时限及额度。管理人员的内容管理操作使用明确的角色权限和版块范围；普通发帖额度仍按其会员等级计算。管理员和管辖版主可以读取受限版块用于治理，普通会员不会因经验增长取得这项豁免。

受限版块过滤在 SQL 分页和计数之前执行，覆盖首页、版块列表、最新主题、用户主题 / 回复列表、搜索、收藏、通知、主题、楼层与附件。通知邮件投递前也检查接收者访问范围。SSE 在订阅时、事件发送前及心跳时重查权限，权限失效发送 `subscription.reset` 后关闭。

未授权内容一般返回 404，会员操作被禁止返回 403 `MEMBER_PERMISSION_DENIED`，每日额度耗尽返回 429 `MEMBER_QUOTA_EXCEEDED`。请求中的 UA 不参与授权。Next.js SSR、Markdown 和站点地图继续调用相同授权接口，不能缓存后向其他身份复用受限正文。

## 4. 成长事件与经验流水

| 事件 | 默认经验 | 每日奖励上限 | 默认撤销 |
| --- | --- | --- | --- |
| `active` 每日活跃 | 1 | 1 | 否 |
| `thread` 有效主题 | 5 | 50 | 是 |
| `reply` 有效回复 | 2 | 40 | 是 |
| `like` 获得他人点赞 | 1 | 20 | 是 |
| `digest` 主题加精 | 20 | 100 | 是 |

各规则固定字段：`enabled`、`points`、`dailyCap`、`reverse`。`dailyCap` 是每日奖励经验总量，0 表示不奖励。首版不提供脚本或任意规则表达式。

实现约定：

1. PostgreSQL 触发器在原业务事务内写入 `member_events`，记录业务来源、规则快照和配置版本。原事务回滚，事件也回滚。
2. `forumd` 每秒处理最多 100 条事件；处理失败保留队列，重启继续。队列积压时升级会延迟。无需额外消息队列或独立服务。
3. 同一业务来源最多奖励一次。并发消费者由数据库锁串行处理；经验流水的唯一键阻止重复发放。
4. 只奖励公开且审核通过的内容；点赞排除自赞。删除、重新送审、取消点赞、取消精华按原奖励的撤销设置冲回。
5. 首次奖励后撤销再恢复，不再次发奖。每日上限统计当天正向奖励，撤销不释放上限；达到上限的零分事件也保留凭据，不能次日反复切换刷分。
6. 事件使用产生时的规则快照和日期，修改规则不重算历史奖励。撤销按原金额记负流水，余额可因历史奖励撤销而出现负数；不自动降级。
7. 活跃、阅读、发帖数、邮箱验证和解除禁言 / 封禁等变化都会触发升级评估。普通 GET、SSR 抓取不会记录活跃或阅读。

阅读接口沿用 `POST /api/v1/threads/{tid}/read` 的 `postId` 字段，每用户对每个具体楼层只计一次；不再把最高楼层差值视为实际阅读量。阅读是客户端声明且受校验、去重、限流约束，不能证明实际阅读时长，因此首版不按阅读发放经验。

## 5. 等级徽章与前端接入

每个等级绑定一个 `badge`：

```json
{"label":"LV2","icon":"crown","color":"#334155","background":"#e2e8f0"}
```

- `icon` 可选空字符串、`seedling`、`star`、`crown`、`shield`、`gem`；由 Next.js 前端映射到图标组件。
- 颜色仅允许六位十六进制颜色；标签最多 20 字。标签与等级名称须通过普通文本绑定显示。
- 首版不接收自定义 HTML、CSS 或 SVG，不提供徽章图片上传；没有图标时显示文字标签即可。
- `/session`、`/me`、用户资料返回 `level` 摘要；主题、主题列表、楼层返回 `authorLevel`。作者等级按列表批量读取，前端无需逐个查询用户资料。
- 自己的经验、升级门槛、锁定状态和额度使用量从 `/me/membership` 获取；`effectiveLimits` 已合并站点大小和签名硬上限。角色标识与等级徽章分别展示。
- 编辑、删除、回复、点赞等按钮以资源返回的 `capabilities` 为准；服务端仍会在写入时重新验证。

## 6. API 清单

以下接口全部返回现有 JSON envelope。新增复杂配置和用户调整请求仅接受 JSON，使用 Cookie 会话及 `X-CSRF-Token`。

| 方法 | 路径 | 用途 / 权限 |
| --- | --- | --- |
| GET | `/api/v1/membership/levels` | 公开等级、权益和徽章 |
| GET | `/api/v1/me/membership` | 本人等级、经验、下一等级、额度与配置版本 |
| GET | `/api/v1/me/experience?page=1` | 本人经验流水，每页 30 条 |
| POST | `/api/v1/me/activity` | 显式记录每日活跃，JSON `{}` |
| GET | `/api/v1/admin/membership` | 完整配置；`membership.view` |
| POST | `/api/v1/admin/membership/preview` | 预览完整候选配置；`membership.configure` |
| PUT | `/api/v1/admin/membership` | 应用预览配置；`membership.configure` |
| GET | `/api/v1/admin/membership/users/{uid}` | 用户会员详情；`membership.view` |
| PATCH | `/api/v1/admin/membership/users/{uid}` | 人工调整；按字段检查权限 |
| GET | `/api/v1/admin/membership/users/{uid}/experience?page=1` | 用户经验流水；`membership.view` |
| GET | `/api/v1/admin/membership/logs?page=1` | 配置 / 等级调整日志；`membership.logs` |
| GET | `/api/v1/admin/membership/diagnose` | 判断目标用户的有效权限；`membership.view` |

后台接口同时要求 `admin.panel`；进入后台不等于可以修改会员配置。调整等级 / 锁定需要 `membership.adjust`；调整非零经验需要 `experience.adjust`；用户调整接口还要求 `membership.view`。

补齐现有后台接口的 `settings.edit`、`censor.manage`、`announce.manage`、`logs.view`、`forum.manage`、`user.ban` 检查，新增 `users.view` 和 `permissions.edit`。管理员的 `admin.panel`、`permissions.edit` 保留防自锁保护。

`/admin/perms/save` 按完整权限矩阵保存，发送 `allow.<role>.<point>=0/1`。未提交的权限点视为关闭，不再为旧客户端保留特殊缺省行为；管理员防自锁权限例外。前端必须依据 `/admin/perms` 返回的完整权限点构建请求。

### 配置预览与应用

1. GET 完整配置，保留 `version`，编辑需要修改的字段。
2. 将完整候选配置 POST 到 `/admin/membership/preview`。
3. 返回 `token`、`configVersion`、`users`、`affectedUsers`、`upgrades`、`locked`。`affectedUsers` 包括等级资料或权限变化、版块规则变化及预计升级涉及的会员；规则变更的未来影响不计入当前升级数。
4. PUT 提交：

```json
{"config":{"version":1,"...":"此处使用完整候选配置"},"previewToken":"预览返回的 token"}
```

以上 `config` 是结构示意，实际请求必须是 GET 取得并修改后的完整对象。

应用时重新检查配置版本及用户等级、经验、锁定、资格指标；变化返回 409 `MEMBERSHIP_CONFLICT`，前端重新预览。新配置、自动升级和日志同事务提交。配置应用短暂锁住用户及会员状态的写入以保持预览一致，适合当前单实例、小型论坛；大量会员时应另行设计分批任务。

使用中的等级不能直接从配置移除：先通过用户调整接口迁移到保留的等级，再重新预览。版块引用的等级必须存在，引用的版块必须有效。

### 人工调整

先读取目标用户详情取得 `version`，然后 PATCH：

```json
{
  "version": 4,
  "levelId": 2,
  "locked": true,
  "delta": 20,
  "reason": "确认历史贡献，补发经验并指定等级",
  "key": "support-ticket-20260906-001"
}
```

`levelId`、`locked` 可省略；必须包含至少一项实际调整意图。`delta` 是增减量，人工扣分后余额不能为负。`key` 是 8–100 字符幂等键，同用户、同操作者、相同请求重复提交不会再调整；同键不同内容或旧版本返回 409。原因必填，最多 500 字节。

### 权限诊断

查询参数：`userId`、`action`，以及适用的 `forumId` / `threadId` / `postId`。例如：

```text
GET /api/v1/admin/membership/diagnose?userId=18&postId=42&action=post.edit
```

返回 `allowed`、`reason`、`action`、`limit`、`used`；诊断只评估权限与已用额度，不代表正文、文件类型、CSRF 等请求校验也已通过。

## 7. 数据迁移与运行

初版通过编号迁移 `006_membership.sql` 引入 schema 6；当前 schema 20，新增互动权限见本文第 2 节。原迁移内容：

| 表 | 职责 |
| --- | --- |
| `membership_config` | 版本化完整配置 |
| `member_states` | 每用户当前等级、经验、锁定及版本 |
| `member_events` | 待处理的事务事件，成功后删除 |
| `member_experience` | 经验流水、来源唯一键、撤销凭据 |
| `member_daily` | 当日次数 / 字节额度 |
| `member_changes` | 配置、自动升级和人工调整日志 |
| `member_read_posts` | 具体楼层阅读去重 |

首次接入的现有账号和新注册账号均以 LV0、0 经验初始化，不映射旧信任等级，不补发历史经验，也不导入旧阅读进度到新会员去重表。已有业务内容、角色及访问 / 阅读 / 发帖统计继续保留；这些统计默认不决定等级。新的阅读去重从接入后开始记录，因此重读旧楼层可增加阅读统计，但不会获得阅读经验。

编号迁移 006 删除旧 `trust_level` 列。已经建立的新会员状态、后台配置和经验余额不会被启动时 schema 重放覆盖。该迁移在真实部署执行时会使旧信任等级程序失去所需字段，不能与旧二进制混用。本轮只验证隔离测试库，未执行生产迁移。

新触发器会参与业务写入。未来实际升级前需备份并评估迁移时间；若回退到不处理会员队列的旧二进制，应同时规划事件队列与规则的回退，不能只替换程序后继续长期写入。

自定义徽章图片、付费 VIP、独立荣誉勋章、兑换积分和多实例 SSE 不在本轮实现内；会员展示与后台表单已在 Next.js 接入。

## 8. 验证

API、store 和迁移测试必须使用三个不同的隔离数据库，数据库名均以 `gobbs_test_` 开头。API/store 测试会清空其目标 schema；迁移测试也会重建测试库 schema。禁止指向业务库。

```bash
# 分别设置各自独立数据库后执行；不能让两个包共用同一测试库
FORUM_TEST_DSN='<独立 API 测试库>' go test -race ./internal/api -count=1
FORUM_TEST_DSN='<独立 store 测试库>' go test -race ./internal/store -count=1
FORUM_MIGRATION_TEST_DSN='<独立迁移测试库>' go test -race ./internal/db -count=1
go vet ./...
go build ./...
```

回归覆盖五级默认配置一致性、旧统计不能绕过经验门槛、API 无旧信任字段、管理权限隔离、事件事务回滚、规则快照、重复奖励、奖励撤销、每日上限、并发额度、阅读去重、锁定与升级、人工幂等、配置冲突、所有公开读取入口的等级限制、附件 / 徽章、SSE 权限撤销，以及从旧用户初始化新会员状态、移除旧列与重启幂等。

本次新规则验证：32 项 API、31 项 store 和 1 项迁移测试在三个独立测试库通过（均使用 `-race`），`go vet ./...` 和新二进制构建通过。迁移测试验证旧等级不映射、新会员从 LV0 初始化、旧字段删除及重启幂等；API 测试验证五级经验门槛、旧字段不再输出、权限矩阵缺省项关闭。实际 HTTP 验证覆盖空库安装、配置预览 / 应用、后台经验和自动升级、活跃、人工锁定及幂等、受限内容与日志。现有网站和业务库未升级。
