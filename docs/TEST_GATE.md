# 后端测试门禁

## CI 与发布

`.github/workflows/backend-tests.yml` 在 PR 和分支 push 时运行，也供 Release 的 `tests` job 调用。PostgreSQL 18 服务创建三个独立数据库，测试角色不具备超级用户、建库或建角色权限。Release 构建/上传依赖该 job 成功，不能再仅靠不接数据库的 `go test ./...` 发布。

`scripts/test-backend.sh` 是本地与 CI 共用入口：

1. 校验 OpenAPI 生成文件与源码/审核定义一致；解析并检查三个 DSN，拒绝空值、业务库名以及重复库名。
2. 设置 `FORUM_REQUIRE_TEST_DB=1`，实测缺库时 store、API、db 三个测试包必须失败。
3. 分库执行完整 store、API、迁移回归，再自动发现其余有测试的包；每包使用 `-race -count=1`，10 分钟超时。
4. 解析 `go test -json` 事件，任何失败、跳过、零测试或缺失包通过事件均拒绝。门禁解析器也有正反例测试。
5. 执行 `go vet ./...`、`go build ./...`、gofmt 与差异格式检查。

CI 上传 JSON 日志为 artifact，失败时同样保留。远端分支保护是否将 `Backend tests / test` 设为必需检查由仓库管理员配置，本次代码变更不会自动更改 GitHub 分支保护。

CI 在完整回归后另跑 `BenchmarkForumTraffic` 一次，启用真实异步配置与共享 Worker，并检查 HTTP、SSE 和派生队列最终一致性；不使用 RPS 数值作为通过阈值。此步骤失败也会阻止 Release，日志保留为 `load.log`。

## 本地运行

`bash scripts/verify-local-backend.sh test` 创建只监听私有 Unix socket 的 PostgreSQL 集群，退出时清理，日志保留在输出的 `/tmp/gobbs-backend-results.*`。Linux 环境需要 PostgreSQL bin 工具、Go、ripgrep；可通过 `RECOVERY_PG_BIN` 指定工具目录，root 运行需 postgres 系统用户。外部 SMTP 在本地 wrapper 中关闭，API 邮件测试使用本地夹具。

已有专用测试库时，设置三个 DSN 后运行 `bash scripts/test-backend.sh`；可以用 `FORUM_TEST_RESULTS_DIR` 指定日志目录。测试会重建目标 schema，禁止指向业务库。

未设置 `FORUM_REQUIRE_TEST_DB` 时保留普通开发运行非数据库测试的行为，但这样的结果不计为发布验收。

## 新增基础回归（2026-09-27）

迁移并发启动、失败回滚、取消等待、当前版本无业务 DDL 和异常账本拒绝；显式派生修复入队与重试状态保留；空 DSN 拒绝和命令模式互斥；API 契约生成一致、真实路由匹配及核心响应形态。详情见 [迁移](MIGRATIONS.md) 和 [契约](API.md)。

## 搜索与订阅优化回归（2026-09-27）

- 搜索对照旧查询，覆盖相关性、同分顺序、主题去重、作者/版块筛选、空权限、待审/删除、中文及超出末页的总数。
- SSE Hub 覆盖 topic 匹配、多连接、取消、慢消费者溢出及并发；离线计数无需数据库，在线计数实时读取并去重。
- 离线订阅通知仍持久保存；Worker 将连续事件的计数合并，三条持久通知对应一个最终计数事件。
- 注入后续批次数据库失败，验证早先已提交通知仍推送计数、失败事件仍待处理、恢复后重试不丢失或重复通知。
- 消费轮次覆盖空队列、批次数上限、时间预算、取消和错误退出。

定点性能比较使用 `bash scripts/verify-local-backend.sh perf`。这类平均耗时比较与完整 HTTP 压测分开报告，不计作持续容量验收。

## 既有搜索回归范围

补充搜索入队失败的发帖/回帖/编辑回滚、并发编辑消费、跳过被写事务锁住的主题，以及删除恢复、审核隐藏与重新公开后搜索一致性检查。搜索消费者按主题、楼层、事件顺序加锁，避免与编辑反向加锁；恢复主题在事务内重新排入搜索任务。

压测使用与服务相同的后台 Worker 启动入口，输出真实配置，等待派生状态校准后断言。短测入口为 `bash scripts/verify-local-backend.sh load`，不会替代持续容量或其他队列故障恢复验收。

## 本地验收记录

2026-09-27，搜索分页与订阅在线计数/轮次合并完成后，完整隔离门禁通过：store 92、API 83、迁移 19、其余包 22，共 216 个顶层测试、288 项含子测试。无失败、无跳过，race、三个缺库负例、vet/build、gofmt、diff 和 178 个操作的契约同步检查通过。证据：`/tmp/gobbs-backend-results.Mq7J3u`。

同日通过 `perf` 入口顺序复测搜索和通知定点性能，证据 `/tmp/gobbs-backend-results.JqZwOS`。随后 `load` 模式以 20/2 连接池、实际异步 Worker 通过：3,720 次 HTTP 请求零错误，100 条 SSE 连接收到 2,000 次事件及 100 次心跳，500 人订阅扇出完成，被检查的经验、称号、公开订阅、搜索、版块统计和邮件队列最终为零，生成两个分析快照。证据 `/tmp/gobbs-backend-results.EHBzNH`。这不是历史 30 分钟持续压力曲线的复验；未部署业务服务或推送远端。


2026-09-27，本阶段独立 PostgreSQL 集群完整门禁通过：store 91、API 77、迁移 19、其余包 20，共 207 个顶层测试；含子测试共 269 项。全部使用竞态检测，无失败或跳过；三个缺库负例、vet、build、格式和 OpenAPI 同步检查通过。证据目录：`/tmp/gobbs-backend-results.qYjr2g`。

随后增强响应形态检查器的 `allOf` 组合校验，单独在新隔离集群复测全部新增契约测试，通过实际路由匹配、匿名/登录会话、列表/游标、主题/楼层、搜索、发帖/编辑/回复，以及错误结构和字段漂移反例。证据：`/tmp/gobbs-backend-results.apYLVk`。不将路由级占位计为完整字段契约。

迁移和维护命令的真实二进制另在临时数据库验证：缺 DSN 的迁移失败，`-version` 无需 DSN，`-migrate`、`-enqueue-derived-repair` 正常退出；并发迁移、历史升级、回滚和维护入队回归通过。证据：`/tmp/gobbs-backend-results.ZYINue`。上述迁移/契约阶段未部署业务服务、执行新压测或验证远端 CI；后续优化阶段的性能复测单独记录，不更改历史故障恢复的覆盖范围。

2026-09-08，独立 PostgreSQL 集群完整门禁通过：store 90、API 73、迁移 13、其余包 14，共 190 个顶层测试；含子测试共 234 项，全部使用竞态检测，无跳过。三个缺库负例均按预期失败，vet、build 和格式检查通过。证据目录：`/tmp/gobbs-backend-results.MboTwQ`。

真实异步短测分别使用 50/5 和 CI 同款 20/2 连接池配置通过，每组 3,720 次 HTTP 请求零错误、100 条 SSE 连接收到 2,000 次事件和 100 次心跳；检查的派生队列最终清空，两个分析快照生成。详细数据见 [数据库性能](DATABASE_PERFORMANCE.md)。这是本地验证记录，尚未在 GitHub 执行远端工作流，也未修改分支保护设置。

修改搜索消费者后，另行复跑 `bash scripts/verify-worker-recovery.sh` 通过：迁移重放、SQL 故障重试、Worker 被强制终止后重启，以及 Worker 存活期间 PostgreSQL 突然停机后重连；最终搜索结果和版块计数一致。证据目录：`/tmp/gobbs-recovery-results.1xU5Z2`。该次历史运行只覆盖搜索/版块统计；新增覆盖见下一节。脚本仍独立执行，尚未纳入上述 GitHub 工作流。


## 扩展 Worker 恢复演练（2026-09-27）

`bash scripts/verify-worker-recovery.sh` 使用共享生产 Worker 启动入口，分别在搜索、版块统计、成长/积分、称号、订阅和邮件的提交前关键点注入进程强杀、数据库 immediate stop/start，共 12 项场景全部通过。每项覆盖回滚、自动恢复、源事件重放、最终停机后状态校验；确认经验、积分、称号及站内通知无重复业务结果。邮件使用独立本地 SMTP，确认两项「已收信、未标记成功」场景会按相同 Message-ID 再次投递，不承诺外部收件去重。

证据：`/tmp/gobbs-recovery-results.T8l0s0`。前置迁移/SQL 重试/邮件 API/probe 夹具回归共 33 个顶层测试、36 项含子测试，均开启 race，无失败、无跳过；故障矩阵另有 36 次最终状态校验。probe 安全连接校验、SMTP 关闭防阻塞及 SMTP 既有回归均通过；定向 vet、shell 语法、gofmt 和 diff 检查通过。

生产代码路径与 schema 未变，本阶段没有重跑 288 项完整门禁或持续压力曲线，没有部署业务服务或推送远端。邮件租约仅在临时库中显式推进到期，周期快照虽随共享入口启动但不属于本轮故障断言范围。完整方法和限制见 [Worker 恢复](WORKER_RECOVERY.md)。

## 完整持续复测（2026-09-27）

提交 `2fb3d91` 上运行 `FORUM_DB_MAX_CONNS=20 FORUM_DB_MIN_CONNS=2 FORUM_SOAK_STAGE_DURATION=10m bash scripts/verify-local-backend.sh soak`，三档各 10 分钟及自然排空全部结束。证据 `/tmp/gobbs-backend-results.wZ6KN1`，含 `soak.jsonl`、`summary.json`、`load.log`、`postgres.log` 和 `run-context.json`。

200/5、400/15 两档无漏发、无 HTTP 错误；600/30 档完成 358,409 次读取及全部 18,000 次回帖，但漏发 1,591 次读取，订阅持续积压，**整体容量门禁按预期返回 1**。全程实际执行 748,409 次 HTTP 请求，无 HTTP 错误，不能因此忽略未发出的请求或下游延迟。

停压后 543.4 秒自然排空，**最终一致性检查通过**：30,000 条成功新增回帖全部可索引，帖子总数 80,000，订阅通知 600,000，版块计数一致，两类快照存在，被检查队列和未索引写入均为零，监控错误为零。临时数据库日志无长锁等待或死锁记录，累计死锁/临时文件写入为零；测试集群和进程已清理。

本次为热缓存英文合成数据与 20 人订阅扇出，SMTP 关闭；没有执行完整 race 门禁、持续 SSE、真实邮件发送或生产部署。详细 CPU/I/O、短暂锁等待、搜索收敛和边界见 [持续负载验收](SUSTAINED_LOAD.md)，下一项调度试验见 [Worker 背压方案](WORKER_PRESSURE.md)。这次测量未改变生产代码或 schema。

## 订阅连续调度回归

新增确定性时钟回归，覆盖快轮次遇到新前台压力时让出、忙池常规轮次保留首批、批次/时间预算、空队列低频轮询、1～30 秒错误退避、恢复后重置退避，以及等待中和批次中的取消。既有部分批次失败时发布先前已提交通知计数、持久游标重试和去重检查继续通过。

完整隔离门禁共 223 个顶层测试、295 项含子测试，无失败/跳过，race、缺库负例、契约、vet/build/格式检查均通过，证据 `/tmp/gobbs-backend-results.N62Y99`。共享 Worker 入口的 12 项故障矩阵及 36 次状态校验亦通过，证据 `/tmp/gobbs-recovery-results.UDSLrw`；邮件租约只在隔离夹具内显式推进到期，外部 SMTP 和周期快照恢复仍不属于覆盖范围。

`backlog` 模式在每个对照前启动新测试进程并重置 schema，保留同一连接预算、数据规模与投递路径；`load` 验证 HTTP、SSE 和派生状态。实际结果及限制见 [Worker 背压与调度](WORKER_PRESSURE.md)。本轮尚未部署或推送。持续负载在前两档各 10 分钟完成后，按用户要求于第三档提前停止；没有自然排空或最终一致性记录，不作为完整容量通过或容量失败的结论。证据 `/tmp/gobbs-backend-results.ILq0U6` 保存 `interrupted.json`，测试进程和独立数据库已清理；不再追加测试，详情见持续负载报告。

## 快照重试与时效定向回归

订阅优化已提交 `dc87818`；其后增加快照独立超时、5 秒至 5 分钟失败退避，以及管理员读取的时效字段。按控制测试时间的要求，只运行 `TestAnalytics*` 三项定向回归（开启 race）、API 契约生成校验测试和 `go build ./...`，均通过，未追加长压测或完整门禁。证据：`/tmp/gobbs-backend-results.9tRq9V`。

确定性时钟验证重试上限、成功后恢复小时周期、类别互不重复刷新、独立超时及取消；独立 PostgreSQL 验证真实读取权限、过期标识、不存在返回 404、写入失败保留旧快照和故障解除后的刷新。另修正此前不存在快照被映射为 500 的问题。该覆盖不是进程强杀或 PostgreSQL 断连演练，不扩大既有 12 项故障矩阵的结论。测试库已清理，本轮没有部署或推送。

## 快照保留期定向回归

基于 `b25c31a` 增加 `analyticsRetentionDays` 设置和每分钟最多 500 条的清理。独立数据库运行 4 项 `TestAnalytics*` 及 2 项配置 API 回归（全部开启 race），API 包 3.598 秒通过；API 契约生成校验测试、`go build ./...` 和差异检查通过。证据：`/tmp/gobbs-backend-results.tzYWwS`。

覆盖默认 30 天、0 关闭、非法类型/范围拒绝、保存即时读取、完整和部分配置写入、权限及元数据；502 条旧积分快照分批清理为 500/1/0，保留每类最新结果和非内置类别，验证 29/31 天边界、锁定行跳过、非法存量配置停止删除、SQL 故障回滚及恢复。确定性时钟确认每分钟清理不会把每小时快照刷新变成高频执行。未运行完整门禁、长压测或额外进程故障演练，没有部署、推送或操作业务库。

## 报表时区定向回归

基于 `f7fce50` 增加 `reportTimeZone` 和站点报表日历边界。隔离存储层 1 项顶层测试（4 个日期子项）、API 7 项定向测试全部开启 race 并通过，包耗时分别为 2.803 秒、3.831 秒；契约校验测试及 `go build ./...` 通过。证据：`/tmp/gobbs-backend-results.4PFWmP`。

校验 UTC 跨年、上海跨日、纽约 23/25 小时夏令时日的今天/昨天计数，使用固定时刻而非真实等待；空可见范围仍返回日期元数据及零计数。API 校验非法时区拒绝、配置保存及读取、新快照携带实际时区和边界，并复用已有快照/保留期/设置接口回归。仅证明后台报表时区，不代表首页、每日额度或积分结算已完成统一时区迁移；未执行长压测、完整门禁或部署。

## 后台配置契约定向回归

基于 `f98224e`，补齐后台站点配置六个操作的字段级 OpenAPI：20 项业务设置及 version、PUT/PATCH 和旧 POST 输入、schema/status 返回，以及权限、版本冲突和主要校验错误。人工覆盖从 35 增至 41 个操作（25 个 fields、12 个 request、4 个 transport），总路由仍为 178 个；未更改生产代码或数据库 schema。

新增目录一致性检查，将公开字段名称、类型、整数范围、字符串长度、必填字段和 PATCH 最小字段数与存储层字段表核对。复用原有配置回归核验六个操作的真实响应：正常读写、缺版本 428、非法输入 422、冲突 409、未登录 401、权限/CSRF 403、配置不可读 503，以及非法历史配置下 status 的 200/valid=false/effective=null 和修复流程。

仅运行 4 项定向 API 测试（含 5 个契约子项，共 9 项，开启 race），无失败或跳过，API 包耗时 4.647 秒；API 契约生成检查及 `go build ./...` 通过。证据：`/tmp/gobbs-backend-results.Yn7Ift`，含 `settings-contract.log`、`contract.log`、`build.log` 和本轮源文件指纹。临时数据库已清理。

没有重跑完整门禁或长压测；旧 POST 表单编码、外部服务和前端生成 SDK 不属于本轮新增实测范围。OpenAPI 描述不代表已引入运行时通用请求校验器，本轮未部署或推送。


## 2026-09-28：投票、悬赏、签到定向验证

当前代码新增 schema 18–20，完成三项第一版后端及 16 个新 API。新增接口及原有采纳/撤销 2 个操作均已补齐字段契约；总操作 194 个，人工覆盖 59 个（43 fields、12 request、4 transport）。

按本轮约定，仅做必要回归，没有重跑完整门禁或持续压测，没有连接业务数据库、部署或推送。

- 一次性 PostgreSQL 私有 socket 集群，仅使用 `gobbs_test_*` 库，测试结束自动清理集群。
- API 定向 `-race`：9 个顶层测试通过、无失败/跳过，含 6 个新功能测试及路由注册/契约检查。投票覆盖审核/访问权限、选项校验、重复/并发、防改投、截止和投票用户删除；匿名票数不被删除账号改写。
- 悬赏覆盖重复冻结/重复采纳、双账户转账和只读对账、已支付拒绝撤销、采纳写入故障时全部回滚、作者/管理员取消、超时/删除退款和重复 Worker 消费。
- 签到覆盖当日重复/并发只奖励一次、昨日连续天数、奖励写入失败时记录/经验/积分全部回滚、时区变更拒绝及本人历史读取。
- 数据库与迁移包 20 个顶层测试通过、无失败/跳过；含 17→20 自定义会员权限保留、互动配置保留及重启幂等。原迁移测试的最新版本断言已从硬编码 17 改为实际最新编号。
- 权限包、API 契约生成器测试、`go build ./...`、`go vet ./...`、gofmt 与 `git diff --check` 通过；源码注册与 OpenAPI 的 194 个操作一致。

证据：API/race/权限 `/tmp/gobbs-backend-results.NYVDm0`；数据库/迁移 `/tmp/gobbs-backend-results.JN0vPr`。这些是本地临时证据目录，不是仓库附件或生产部署证明。

复跑时先准备隔离数据库，保持 API 与迁移 DSN 指向不同的 `gobbs_test_*` 库：

```bash
FORUM_REQUIRE_TEST_DB=1 FORUM_TEST_DSN="$FORUM_API_TEST_DSN" go test -race ./internal/api -run '^(Test(Poll|Bounty|Checkin).*|TestOpenAPIRegisteredOperations|TestSettingsContractCatalog|TestContractShapeRejectsIDAndRequiredFieldDrift)$' -count=1 -timeout=90s
FORUM_MIGRATION_TEST_DSN="$FORUM_MIGRATION_TEST_DSN" go test ./internal/db -count=1 -timeout=90s
go test ./internal/perm ./scripts/api-contract -count=1
```

退款持久状态及重放已验证，但本轮没有对新增退款 Worker 做真实进程强杀/数据库断连演练；没有验证生产数据量升级耗时、长期积压、前端联调或公开排行榜。实现边界见 [互动功能 API](ENGAGEMENT_FEATURES.md)。
