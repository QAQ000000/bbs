# GoBBS

Go + PostgreSQL 论坛 API 后端，当前数据库 schema 21。Go 提供 JSON API、SSE 与受控媒体；浏览器页面、HTML/Markdown 出口及 SEO 由独立前端承担，Nuxt 工程尚未实现。旧版 Release 可能仍包含整站页面，部署前须核对版本说明。

开发入口：[当前功能状态](docs/FEATURE_STATUS.md) · [API 契约](docs/API.md) · [OpenAPI](docs/openapi.json) · [迁移与维护](docs/MIGRATIONS.md) · [测试门禁](docs/TEST_GATE.md) · [前后端分离方案](docs/FRONTEND_BACKEND_SEPARATION.md)。

## 已实现的后端能力

- 账号：注册/登录、邮箱验证与恢复、安全换绑、设备会话、TOTP/恢复码、资料与受限数据导出/删除。
- 内容：分类版块、主题与楼层回复、草稿、版本化编辑、附件、全文搜索、作者采纳、收藏和点赞。
- 社区：标签、关注/粉丝、三类订阅、通知偏好与已读、私信首条限制及屏蔽。
- 互动：单选/多选投票及审核、积分悬赏冻结/采纳支付/退款、每日签到与连续天数，见 [互动 API](docs/ENGAGEMENT_FEATURES.md)。
- 成长：可配置会员等级、经验、徽章、权限/额度；独立任务称号；积分账户、奖励、冲回、调账和对账。
- 治理：角色权限、版主管辖、审核、举报、回收站、敏感词、封禁、公告、站点配置和审计 API。
- 异步与运行：PostgreSQL 持久任务、邮件重试、搜索/版块统计校准、后台积分榜及站点报表快照、数据库/队列诊断和隔离回归脚本。

投票、悬赏、签到第一版后端已实现；补签、连续额外奖励和悬赏分摊未实现。排行榜目前只有后台积分 Top 100 快照，没有公开榜单和多维排名。后台配置页面、编辑器、SSR 和爬虫页面也不属于当前 API 二进制。完整边界见 [功能矩阵](docs/FEATURE_STATUS.md)。

## 权限与成长模型

采用固定角色的可配置权限矩阵、版主管辖范围、会员等级权限/额度及版块访问规则共同判定。角色权限不等于等级权限；前端使用 API 返回的 capabilities 展示可执行操作，后端写入时仍独立校验。

旧信任等级 TL 及其映射已经移除。会员默认五级经验门槛为 0 / 100 / 500 / 1500 / 5000，后台可调整；经验、独立积分、任务称号分别记账。称号仅用于展示，不授予权限。详见 [会员](docs/MEMBERSHIP.md)、[称号](docs/TITLES.md)、[积分](docs/POINTS.md)。

## 快速开始

生产环境请从 [GitHub Releases](https://github.com/QAQ000000/bbs/releases) 下载对应平台的二进制压缩包，并校验 `SHA256SUMS`。生产部署不需要下载源码。

```bash
# 0. 前置：PostgreSQL 16+（推荐 18）
# 1. 下载并解压 Release 中的 forumd 和 gobbsctl
# 2. 建库（在 PostgreSQL 上执行）
CREATE DATABASE forum OWNER "youruser";

# 3. 启动（首次加 -seed 灌入演示数据；schema 启动时自动迁移）
FORUM_DSN="postgres://user:pass@127.0.0.1:5432/forum" \
FORUM_ADDR="127.0.0.1:8090" \
./bin/forumd -seed

# 4. 检查 API；该版本不提供浏览器页面
curl http://127.0.0.1:8090/api/status
curl http://127.0.0.1:8090/api/v1/home
# -seed 演示管理员：admin / admin123456（通过 API 登录后须改密）
```

## 配置（环境变量）

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `FORUM_ADDR` | `127.0.0.1:8080` | HTTP 监听地址 |
| `FORUM_DSN` | 无默认值（连接数据库时必填） | PostgreSQL 连接串 |
| `FORUM_SITE_NAME` | `GoBBS 社区` | 站点名称（首次启动写入设置，之后后台可改） |
| `FORUM_SITE_LOGO` | `Go!BBS` | 头部 Logo 文案 |
| `FORUM_SITE_URL` | `http://127.0.0.1:8090` | 站点外部地址（邮件中的链接） |
| `FORUM_UPLOAD_DIR` | `data/uploads` | 图片/附件存储目录（运行时数据） |
| `FORUM_SMILEY_DIR` | `data/smiley` | 自定义图片表情包目录 |
| `FORUM_THREADS_PER_PAGE` | `20` | 版块页每页主题数（首次启动写入设置） |
| `FORUM_POSTS_PER_PAGE` | `10` | 帖子页每页楼层数（首次启动写入设置） |
| `FORUM_PROD` | `0` | 置 1 启用 Secure Cookie（HTTPS 部署时） |
| `FORUM_SMTP_HOST` | 空 | SMTP 服务器；**为空则禁用邮件通知** |
| `FORUM_SMTP_PORT` | `25` | SMTP 端口 |
| `FORUM_SMTP_USER` / `FORUM_SMTP_PASS` | 空 | SMTP 认证（可选） |
| `FORUM_SMTP_FROM` | `noreply@gobbs.local` | 发件人地址 |
| `FORUM_MAIL_KEY` | 空 | 启用 SMTP 时必填；64 位十六进制随机密钥，加密持久邮件令牌；所有实例和重启保持一致 |
| `FORUM_MFA_KEY` | 空 | TOTP 2FA 独立加密密钥，64 位十六进制；所有实例和重启保持一致，详见 [二次验证](docs/MFA.md) |

> 站点级配置（站名、每页条数、注册开关、发帖审核、**上传限额与仅外链模式**、关站）
> 存于数据库，在 后台 → 站点设置 修改，环境变量仅作首次启动的初始值。

## 运行时数据与自定义表情

运行时数据都在 `data/`（默认），**不属于仓库、不参与编译**，请纳入备份：

```
data/
├── uploads/    # 用户上传的图片与附件（FORUM_UPLOAD_DIR）
└── smiley/     # 自定义图片表情包（FORUM_SMILEY_DIR）
```

默认表情为 Unicode Emoji 短代码（`:smile:` `:joy:` …），渲染为字符本身，零图片资产。
要使用图片表情包（例如从你此前的论坛迁移素材）：

```bash
# 你的素材目录结构：<素材目录>/<包名>/<图片文件>
./bin/forumd -import-smileys /path/to/my-smileys
# 可选：提供原始表情代码映射（结构见 scripts/gen_smileys.php 的输出）
./bin/forumd -import-smileys /path/to/my-smileys -codes codes.json
```

导入后重启生效，表情 API 返回对应分组；编辑器面板需前端接入。素材的版权与授权由导入者自行确认。

## 部署运维

完整的生产部署手册（systemd、备份/恢复脚本、nginx 配置、升级流程、上线检查单）
见 [docs/DEPLOY.md](docs/DEPLOY.md)。功能演进计划见 [docs/ROADMAP.md](docs/ROADMAP.md)。要点：

**systemd 单元**（`/etc/systemd/system/gobbs.service`）：

```ini
[Unit]
Description=GoBBS forum
After=network.target postgresql.service

[Service]
WorkingDirectory=/opt/gobbs
Environment=FORUM_DSN=postgres://user:pass@127.0.0.1:5432/forum
Environment=FORUM_ADDR=127.0.0.1:8090
ExecStart=/opt/gobbs/bin/forumd
Restart=on-failure
User=gobbs

[Install]
WantedBy=multi-user.target
```

**备份**：PostgreSQL `pg_dump forum` + `data/` 目录。
**升级**：下载 GitHub Release 二进制，校验 `SHA256SUMS` 后使用 `gobbsctl upgrade`；生产环境无需源码。

**反向代理（nginx）**：

```nginx
location /api/ { proxy_pass http://127.0.0.1:8090; }
location /api/v1/events { proxy_pass http://127.0.0.1:8090; proxy_buffering off; proxy_read_timeout 60s; }
# 页面路由须在 Nuxt 上线后转发给其独立服务；媒体路由见部署文档。
```

生产环境务必：改默认管理员密码、`FORUM_PROD=1`（HTTPS 下 Secure Cookie）。

## 开发

只有需要修改代码、运行测试或自行构建时才需要下载源码。生产部署直接使用 GitHub Releases 中的二进制。

```
cmd/forumd/        入口（服务 / -migrate / -enqueue-derived-repair / -seed / -version）
cmd/gobbsctl/      发布升级与健康检查工具
assets/            仅 embed 数据库 schema 和迁移
internal/
  config/          环境变量配置
  db/              连接池与 schema 迁移
  store/           数据访问（查询/事务/缓存），全部 SQL 集中于此
  smiley/          表情系统（内置 emoji + 自定义图片包）
  live/            SSE 推送中枢（按主题分组发布订阅）
  mail/            邮件模板、SMTP 传输及认证令牌加密（持久队列见 store/email_queue.go）
  avatar/          确定性字母头像 SVG
  api/             JSON 路由、DTO、认证、业务 handlers 和媒体端点
scripts/           辅助脚本（素材导出、发布自检）
```

- 后端不再打包页面模板或前端静态资源；Nuxt 项目尚未创建
- 发布版本由 GitHub Actions 在 `v*` 标签推送时构建并附带 SHA256；生产升级使用 `gobbsctl`
- API 简要说明见 [docs/API.md](docs/API.md)
- 后续前后端分离开发遵循 [docs/FRONTEND_BACKEND_SEPARATION.md](docs/FRONTEND_BACKEND_SEPARATION.md)（已完成后端展示层剥离；Nuxt 和 Markdown 出口待开发）
- 表结构见 `assets/db/schema.sql`（含逐表注释）
- 检查：`go vet ./...`、`go build ./...`。
- 后端 API 测试：`go test ./internal/api`；不设置 `FORUM_TEST_DSN` 时，仅运行无需数据库的测试，集成测试明确跳过。
- 完整集成：为 API 和 store **分别**准备独立测试库，并分别设置 `FORUM_TEST_DSN` 执行 `go test ./internal/api` 与 `go test ./internal/store`。API 测试要求库名以 `gobbs_test_` 开头，上传文件使用临时目录。
- 发布门禁：`bash scripts/verify-local-backend.sh test` 自动启动独立 PostgreSQL 集群，执行与 CI 相同的严格回归。已有专用测试库时可设置 `FORUM_STORE_TEST_DSN`、`FORUM_API_TEST_DSN`、`FORUM_MIGRATION_TEST_DSN` 后执行 `bash scripts/test-backend.sh`。三个库名必须不同且以 `gobbs_test_` 开头；任何跳过、失败或数据库缺失均拒绝通过。详见 [测试门禁](docs/TEST_GATE.md)。
- 数据库测试会清空目标库 schema；禁止使用业务库，禁止两个包共用同一测试库并行执行。`store` 已移除默认数据库连接，同样要求显式指定 `gobbs_test_` 测试库。
- API 回归覆盖 JSON 数据、字段隐私、登录/CSRF、发帖/编辑/删除、权限、审核、附件及 SSE；原 HTML 展示断言已移除，后续由 Nuxt 测试接替。

## 已知边界与取舍

- **单实例**：SSE Hub、限流和浏览计数仍为进程内状态，不支持完整的多实例/滚动升级。会话鉴权已改为数据库校验，撤销不依赖实例缓存；Go 整页 HTML 缓存已删除。
- **搜索天花板**：中文 bigram 分词对单字查询不敏感；命中聚合取 top-400 内存去重。
  容量需按实际查询分布验证，不能只凭论坛规模推断可用吞吐。
- **迁移策略**：迁移锁串行化同库迁移，DDL 与版本登记在同一事务提交；新库初始化 schema，已有版本账本的库只运行缺失编号迁移。正常重启不重放业务 DDL。支持 `-migrate` 独立迁移、`-enqueue-derived-repair` 显式排入搜索/版块统计修复；详见 [迁移与维护](docs/MIGRATIONS.md)。升级前仍需备份和维护窗口，旧二进制不保证兼容新数据库。
- **登录会话**：Cookie 中为原始 token，库中仅存 SHA-256。
- **设备管理**：支持设备列表、重命名、单设备/其他设备/全部退出，接口见 [设备会话管理](docs/DEVICE_SESSIONS.md)。
- **独立积分**：账户、行为奖励、冲回流水、后台配置/调账及只读对账，见 [积分账本](docs/POINTS.md)；悬赏结算/退款、每日签到奖励已接入；积分榜已生成后台快照，公开排行尚未接入。

## 许可

本项目以 **AGPL-3.0-or-later** 分发（见 [LICENSE](LICENSE)）。
第三方组件许可见 [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md)。
