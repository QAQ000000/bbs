# 部署运维手册

> 2026-09-30：Go 服务仅提供 API/SSE/媒体，浏览器页面由独立 Next.js 服务提供。新架构与发布约束见 [分离方案](FRONTEND_BACKEND_SEPARATION.md)。旧版 `/api/live` 已改为 `/api/v1/events`；生产 Nginx 应将 `/api/` 和 `/api/v1/events` 转发到 Go，将其他页面转发到 Next.js。后端仍兼容 `/api/status`。

面向生产部署的完整流程。默认使用 [GitHub Releases](https://github.com/QAQ000000/bbs/releases) 中的二进制，不需要下载源码；只有开发或自行构建时才需要源码。

## 1. 目录规划（推荐）

```
/opt/gobbs/
├── bin/forumd          # 从 GitHub Release 下载的二进制
├── bin/gobbsctl        # 发布升级工具
├── data/               # 运行时数据（uploads/、smiley/）—— 必须备份
└── .env                # 环境变量（权限 600）
```

`.env` 示例（systemd 的 `EnvironmentFile` 格式）：

```ini
FORUM_DSN=postgres://gobbs:强密码@127.0.0.1:5432/forum
FORUM_ADDR=127.0.0.1:8090
FORUM_SITE_URL=https://bbs.example.com
FORUM_PROD=1
FORUM_SMTP_HOST=smtp.example.com
FORUM_SMTP_PORT=587
FORUM_SMTP_USER=noreply@example.com
FORUM_SMTP_PASS=xxx
FORUM_SMTP_FROM=noreply@example.com
FORUM_MAIL_KEY=替换为64位十六进制随机密钥
```

启用 SMTP 时使用 `openssl rand -hex 32` 生成 `FORUM_MAIL_KEY` 并填入上述配置，重启和多实例保持一致。该密钥加密持久邮件队列中的认证令牌，必须与数据库备份分开保管。恢复和接口说明见 [邮件队列](EMAIL_QUEUE.md)。

启用 TOTP 2FA 时另行执行 `openssl rand -hex 32` 生成 `FORUM_MFA_KEY`，加入同一受限环境文件；不要复用邮件密钥。必须保留原密钥用于重启、多实例和数据库恢复。缺失或错误密钥会拒绝已启用 2FA 账户的验证，不能通过重设密钥恢复访问。schema 14 保留旧 TOTP 保护、作废旧登录挑战及旧格式恢复码；升级后已启用用户需用 TOTP 生成新恢复码。完整接口和迁移边界见 [二次验证](MFA.md)。

## 2. systemd

`/etc/systemd/system/gobbs.service`：

```ini
[Unit]
Description=GoBBS forum
After=network.target postgresql.service
Wants=postgresql.service

[Service]
Type=simple
WorkingDirectory=/opt/gobbs
EnvironmentFile=/opt/gobbs/.env
ExecStart=/opt/gobbs/bin/forumd
Restart=on-failure
RestartSec=3
User=gobbs
Group=gobbs
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/opt/gobbs/data
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

```bash
useradd -r -s /usr/sbin/nologin gobbs
chown -R gobbs:gobbs /opt/gobbs
systemctl daemon-reload && systemctl enable --now gobbs
systemctl status gobbs && journalctl -u gobbs -f
```

> 工作目录（WorkingDirectory）决定 `data/` 相对路径的实际位置，务必与备份路径一致。

## 3. PostgreSQL

最小授权（应用账号不需要超级用户）：

```sql
CREATE ROLE gobbs LOGIN PASSWORD '强密码';
CREATE DATABASE forum OWNER gobbs;
```

`FORUM_DSN` 必须显式设置。程序仍自动迁移，也可先用 `forumd -migrate` 独立完成迁移后退出。同库迁移器通过事务级 advisory lock 串行执行，DDL 与版本记录一次提交；正常重启不再重放完整 schema。超时、回滚、旧库和维护命令说明见 [迁移与维护](MIGRATIONS.md)。

当前 schema 21；`017_async_workers.sql` 登记版块统计队列、搜索索引队列和分析快照结构，018–020 增加投票、悬赏和签到，021 保存退款失败与重试时间。已有任务、快照和配置在升级/重启时保留，健康接口应返回 `schema: 21`。开发环境恢复演练见 [Worker 恢复验证](WORKER_RECOVERY.md)。

schema 16 增加读取索引。当前迁移使用普通 `CREATE INDEX`，存量大表建索引期间会阻塞相应表的写入，应在维护窗口升级；尚不支持无停机在线建索引迁移。

数据库连接池默认每进程最多 20、最少 2 个连接，可在环境文件配置 `FORUM_DB_MAX_CONNS` 和 `FORUM_DB_MIN_CONNS`（最大值 1–1000，最小值 0–最大值）。普通 API 的 `FORUM_API_TIMEOUT_SECONDS` 默认 15 秒；上传和导出查询的 `FORUM_UPLOAD_TIMEOUT_SECONDS` 默认 60 秒。修改上述运行参数后重启服务。多个实例的连接数要合计预留，后台任务也使用同一连接池。详见 [数据库性能与监控](DATABASE_PERFORMANCE.md)。

## 4. nginx 反向代理

```nginx
server {
    listen 80;
    server_name bbs.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name bbs.example.com;

    # 与后台"附件上限"保持一致（默认 20MB，另留头部余量）
    client_max_body_size 25m;

    # 浏览器页面、SSR、robots/sitemap/RSS 由 Next.js 提供
    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # API SSE 实时通道：必须关闭缓冲
    location /api/v1/events {
        proxy_pass http://127.0.0.1:8090;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Connection "";
        proxy_buffering off;
        proxy_read_timeout 3600s;
    }

    location /api/ {
        proxy_pass http://127.0.0.1:8090;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

启用 HTTPS 后在 `.env` 加 `FORUM_PROD=1`（Cookie Secure 标志）。

## 5. 备份与恢复

`/opt/gobbs/scripts/backup.sh`（加入 cron，如每日 03:00）：

```bash
#!/bin/bash
set -euo pipefail
KEEP=14                                  # 保留天数
DEST=/var/backups/gobbs
DSN="${FORUM_DSN:?必须设置 FORUM_DSN}"
STAMP=$(date +%F_%H%M)
mkdir -p "$DEST"

pg_dump "$DSN" | gzip > "$DEST/db_$STAMP.sql.gz"
tar -czf "$DEST/data_$STAMP.tar.gz" -C /opt/gobbs data
find "$DEST" -type f -mtime +$KEEP -delete
echo "备份完成: $DEST/db_$STAMP.sql.gz"
```

crontab：`0 3 * * * /opt/gobbs/scripts/backup.sh >> /var/log/gobbs-backup.log 2>&1`

**恢复流程**：

```bash
systemctl stop gobbs
gunzip -c db_2026-09-03_0300.sql.gz | psql "postgres://gobbs:强密码@127.0.0.1:5432/forum"
rm -rf /opt/gobbs/data && tar -xzf data_2026-09-03_0300.tar.gz -C /opt/gobbs
systemctl start gobbs
```

## 6. 升级流程

从 GitHub Release 下载对应平台的压缩包并校验 `SHA256SUMS`，再使用 `gobbsctl` 执行二进制替换。数据库缺少的结构由新
`forumd` 启动时自动执行编号迁移，不需要手工补 SQL：

```bash
# 将 Release 中的新 forumd 放到临时路径，例如 /opt/gobbs/bin/forumd.new
/opt/gobbs/bin/gobbsctl upgrade \
  -binary /opt/gobbs/bin/forumd.new \
  -service gobbs \
  -url http://127.0.0.1:8090/api/status
```

工具先复制并保留旧二进制，再原子替换新文件。新文件必须与目标 `forumd` 同目录；默认备份位于 `releases/`，恢复时也保留备份。

- `-timeout`：每次启动后的整体健康等待上限，默认 60 秒。
- `-request-timeout`：单次 HTTP 健康请求上限，默认 5 秒，同时受整体期限限制。
- `-command-timeout`：每个 `systemctl` 调用上限，默认 30 秒；停止、启动、恢复分别计时。超时参数须为正值。
- 重启失败或新程序健康等待失败均进入统一恢复：停止服务、恢复旧文件、重新启动、检查旧服务健康。只有恢复后的 `/api/status` 返回 HTTP 200、`ok=true`、`db=up` 才报告恢复成功；仅恢复文件不算成功。
- 即使恢复成功，本次升级仍以非零退出码结束；恢复重启或健康失败会明确报错，不宣称已恢复可用。新程序替换前的备份/校验失败不会重启服务。

这是**二进制恢复**，不会撤销已提交的数据库迁移。旧程序可能因 schema 版本过新而拒绝启动；这种情况恢复健康检查也会失败。跨 schema 升级前必须备份数据库、媒体及部署密钥，按维护窗口准备经过验证的数据库恢复或前向修复方案，不能修改迁移账本冒充回退。磁盘文件复制时间不属于 HTTP/服务命令的超时预算。

```bash
开发或无法使用 gobbsctl 时，才从源码构建并按上述方式替换；生产环境优先使用 Release 二进制。
```

- schema 迁移全部幂等，随启动自动执行；`gobbsctl` 可回滚二进制，数据库迁移不自动回滚
- 新库初始化完整 schema；已有版本的库只执行缺失的 `assets/db/migrations/NNN_*.sql` 编号迁移，
  启动日志出现「已应用编号迁移 version=N」即表示存量库完成升级
- 需要全量重分词或修复版块统计时，显式执行 `forumd -enqueue-derived-repair`，再由普通服务 Worker 消费；命令成功只表示入队，不表示修复已经完成
- 当前 API 二进制只 embed 数据库结构；页面和前端静态资源由独立 Next.js 服务部署

## 7. 监控与健康检查

- `GET /api/status` → `{"ok":true,"db":"up","schema":21,"pending":{...},"ts":"..."}`，可接入拨测：
  - `db` 数据库可达性；`schema` 为 `schema_migrations` 迁移版本（与发布版本核对）
  - `pending` 为治理队列积压（待审主题/回复/待处理举报），持续增长说明该去后台处理了
- `forumd -check-backup`：上线检查单（DSN 可写、schema 版本、pg_dump 在 PATH、
  数据目录可写），任一项失败退出码为 1，可直接用于部署脚本断言；
  注意 systemd/部署环境需把 PostgreSQL 的 `bin` 目录加入 PATH（如
  `Environment=PATH=/usr/local/bin:/usr/bin:/www/server/pgsql/bin`），否则 pg_dump 检查会失败
- 日志：stdout（systemd → journald），访问日志含路径与耗时；`panic` 会被中间件捕获并记 ERROR

## 8. 上线检查单

- [ ] `./bin/forumd -check-backup` 全项通过（需 pg_dump 在 PATH）
- [ ] 默认管理员已改密（`-seed` 站点首次登录会被强制改密后方可发帖/进后台）
- [ ] `FORUM_PROD=1`（HTTPS）
- [ ] `client_max_body_size` 与后台"上传限额"匹配
- [ ] `/api/v1/events` 无缓冲（否则实时刷新失效）
- [ ] `/api/` 转发到 Go，其他页面路由转发到 Next.js
- [ ] 备份 cron 已配置且试跑过一次恢复
- [ ] `/api/status` 的 `schema` 版本与本次发布一致
- [ ] 发布前跑 `scripts/check-clean.sh`（仓库不含第三方素材）
