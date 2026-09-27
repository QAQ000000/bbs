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

修改搜索消费者后，另行复跑 `bash scripts/verify-worker-recovery.sh` 通过：迁移重放、SQL 故障重试、Worker 被强制终止后重启，以及 Worker 存活期间 PostgreSQL 突然停机后重连；最终搜索结果和版块计数一致。证据目录：`/tmp/gobbs-recovery-results.1xU5Z2`。本脚本目前单独执行，尚未纳入上述 GitHub 工作流，也不代表所有异步队列均已完成同等故障覆盖。
