# 部署运维手册

> 2026-09-06：当前源码已剥离页面，以下原单体部署流程保留供旧版运维参考。新版本仅提供 API/SSE/媒体，Nuxt 前端尚未实现，不能直接替换现有完整网站。新架构与发布约束见 [分离方案](FRONTEND_BACKEND_SEPARATION.md)。原 `/api/live` 已改为 `/api/v1/events`，所有 `/` 页面转发配置必须在 Nuxt 完成后调整。后端仍兼容 `/api/status`。

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

当前 schema 20；`017_async_workers.sql` 登记版块统计队列、搜索索引队列和分析快照结构，018–020 增加投票、悬赏和签到。已有任务、快照和配置在升级/重启时保留，健康接口应返回 `schema: 20`。开发环境恢复演练见 [Worker 恢复验证](WORKER_RECOVERY.md)。

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

    location / {
        proxy_pass http://127.0.0.1:8090;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # SSE 实时通道：必须关闭缓冲
    location /api/live {
        proxy_pass http://127.0.0.1:8090;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_buffering off;
        proxy_read_timeout 3600s;
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

工具会保留旧二进制、原子替换新文件、重启 systemd 服务，并等待数据库迁移及
`/api/status` 通过；健康检查失败时自动恢复旧二进制并重启。旧二进制只代表
程序回滚，已执行的数据库迁移不会自动回滚。

```bash
开发或无法使用 gobbsctl 时，才从源码构建并按上述方式替换；生产环境优先使用 Release 二进制。
```

- schema 迁移全部幂等，随启动自动执行；`gobbsctl` 可回滚二进制，数据库迁移不自动回滚
- 新库初始化完整 schema；已有版本的库只执行缺失的 `assets/db/migrations/NNN_*.sql` 编号迁移，
  启动日志出现「已应用编号迁移 version=N」即表示存量库完成升级
- 需要全量重分词或修复版块统计时，显式执行 `forumd -enqueue-derived-repair`，再由普通服务 Worker 消费；命令成功只表示入队，不表示修复已经完成
- 当前 API 二进制只 embed 数据库结构；页面和前端静态资源需要独立前端部署

## 7. 监控与健康检查

- `GET /api/status` → `{"ok":true,"db":"up","schema":17,"pending":{...},"ts":"..."}`，可接入拨测：
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
- [ ] `/api/live` 无缓冲（否则实时刷新失效）
- [ ] 备份 cron 已配置且试跑过一次恢复
- [ ] `/api/status` 的 `schema` 版本与本次发布一致
- [ ] 发布前跑 `scripts/check-clean.sh`（仓库不含第三方素材）
