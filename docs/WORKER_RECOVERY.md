# Worker 恢复验证

运行 `bash scripts/verify-worker-recovery.sh`。需要 Linux、Go、ripgrep、PostgreSQL 的 `initdb`、`pg_ctl`、`psql`；通过 `RECOVERY_PG_BIN` 指定 PostgreSQL bin 目录（本地默认 `/www/server/pgsql/bin`）。root 运行需已有 postgres 系统用户，普通用户可直接运行。

脚本新建专用临时 PostgreSQL 集群，只监听私有目录中的 Unix socket，不接收现有业务 DSN 或 PGDATA。应用连接使用无超级用户、建库、建角色权限的测试账号。退出时停止并删除临时集群，日志保留在输出的 `/tmp/gobbs-recovery-results.*` 目录。

执行流程：

1. 全部 db 迁移测试开启 `-race`，验证升级、重复迁移和存量数据保留；搜索 SQL 失败退避测试使用独立 schema 运行。任何跳过或失败均使脚本失败。
2. 构建独立 probe 进程，运行服务使用的 `RunSearchIndex` 与 `RunForumStats`，事务内创建主题、回帖和派生事件。
3. 触发器暂停索引写入，查询 `pg_stat_activity` 确认 Worker 正在处理任务，再 SIGKILL 进程。确认未完成任务保留，启动新进程，验证队列清空、搜索词命中、版块帖子数和主题数一致。
4. 再次创建任务并暂停处理，对专用数据库执行 immediate stop。在原 Worker 进程存活期间重启数据库、解除故障，验证连接池自动恢复与最终一致性。

probe 不调用启动全量重建来代替队列恢复。脚本覆盖搜索和版块统计两个 Worker；未覆盖所有队列、完整 HTTP 服务重启、外部 SMTP、备份恢复和持续容量。SIGKILL 后仅终止测试库遗留会话以释放触发器锁，不触及外部 PostgreSQL。

schema 17 包含三个派生结构：`forum_stat_events`、`search_index_events`、`analytics_snapshots`。迁移测试直接执行编号 SQL 后再重放完整 schema，防止 schema.sql 提前建表掩盖缺失的编号迁移。

## 本地验证记录

2026-09-08，在 PostgreSQL 18.0 的独立临时集群运行完整脚本成功。db 包测试和搜索 SQL 重试回归以 `-race -count=1` 执行，无跳过；实际进程终止重启、数据库 immediate stop/start 后原进程自动重连均通过。两次 `verify` 确认搜索/版块统计队列清空、正文关键词可检索、帖子及主题数量一致。脚本退出清理集群，未重启常驻数据库或替换论坛服务。

本次日志目录为 `/tmp/gobbs-recovery-results.Le1LTk`（本地临时证据，不随仓库分发）。这是故障恢复验证，不是吞吐量或生产容量结论；完整 HTTP 服务生命周期与其他队列恢复仍需另行演练。
