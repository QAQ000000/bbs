# 部署运维手册

面向生产部署的完整流程。基础安装见 [README](../README.md#快速开始)。

## 1. 目录规划（推荐）

```
/opt/gobbs/
├── bin/forumd          # 二进制
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
```

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

schema 由程序启动时自动增量迁移（幂等），无需手工执行 SQL。

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
DSN="${FORUM_DSN:-postgres://gobbs:pass@127.0.0.1:5432/forum}"
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

```bash
cd /opt/gobbs-src && go build -o /opt/gobbs/bin/forumd.new ./cmd/forumd
systemctl stop gobbs
mv /opt/gobbs/bin/forumd.new /opt/gobbs/bin/forumd
systemctl start gobbs
journalctl -u gobbs -n 20        # 确认"论坛服务已启动"与迁移/索引日志
```

- schema 迁移全部幂等，随启动自动执行，可安全回滚到旧二进制
- 分词器/渲染逻辑升级时，启动日志会显示"已补齐搜索索引 N 条"自动重建
- 模板与静态资源已 embed 进二进制，无需同步文件

## 7. 监控与健康检查

- `GET /api/status` → `{"ok":true,"subs":12,"ts":"..."}`（subs 为 SSE 实时连接数），可接入拨测
- 日志：stdout（systemd → journald），访问日志含路径与耗时；`panic` 会被中间件捕获并记 ERROR

## 8. 上线检查单

- [ ] 默认管理员 `admin/admin123456` 已改密
- [ ] `FORUM_PROD=1`（HTTPS）
- [ ] `client_max_body_size` 与后台"上传限额"匹配
- [ ] `/api/live` 无缓冲（否则实时刷新失效）
- [ ] 备份 cron 已配置且试跑过一次恢复
- [ ] 发布前跑 `scripts/check-clean.sh`（仓库不含第三方素材）
