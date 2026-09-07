# GoBBS

Go + PostgreSQL 论坛后端，正在迁移到 Nuxt SSR + Go API 的前后端分离架构。

**当前源码已移除 Go 页面模板、CSS/JS、页面路由和 Markdown HTML 渲染。** 新二进制提供 JSON API、SSE 与受控媒体，不再直接显示论坛页面。Nuxt 前端尚未开发；本轮源码变更未部署到现有网站。旧版 Releases 可能仍为包含页面的版本，部署前须确认版本说明。

开发入口：[任务称号与采纳](docs/TITLES.md) · [会员等级与权限](docs/MEMBERSHIP.md) · [当前 API](docs/API.md) · [分离方案与实施记录](docs/FRONTEND_BACKEND_SEPARATION.md)。

后续社区扩展功能的规则和开发顺序见 [社区扩展功能方案](docs/COMMUNITY_FEATURES_PLAN.md)。

schema 9 已实现标签管理与筛选、关注/粉丝、主题/版块/标签订阅投递和一对一私信。私信支持首条等待回复、屏蔽、分页与已读；接口与当前限制见 [社区 API](docs/COMMUNITY_API.md)。

会员后端现已支持可配置等级与徽章、成长经验、自动升级、等级权限和额度、版块访问限制、后台配置预览与人工调整。管理页面仍待 Nuxt 实现。

任务称号支持发帖、回复、精华、点赞和作者采纳等条件，含历史补发、限时有效、人工授予/撤销、佩戴及审计 API。称号不授予权限，也不要求先获得经验；当前 schema 为 9。

通知分页与偏好、审核/采纳/称号/升级结果通知、草稿标题、本人内容状态、具体楼层回复及定位已接入 API。契约见 [论坛基础流程](docs/FORUM_WORKFLOWS.md)，后续待办见 [功能核查](docs/FORUM_FEATURE_AUDIT.md)。

会员等级采用五级经验成长体系，默认门槛为 0 / 100 / 500 / 1500 / 5000。旧信任等级字段和映射已移除；现有账号首次接入从 LV0 开始，后台可修改等级及门槛。

## 功能范围（前端迁移中）

以下清单记录原论坛的功能范围。账号、内容、互动和治理业务已保留在后端；页面、编辑器、SEO 及 HTML/Markdown 文档输出将由 Nuxt 实现，不能将下列 UI 能力视为当前纯 API 二进制已经提供。

- **内容**：Markdown 编辑器（工具栏/表情/粘贴传图/云端草稿/预览）、版块与分类、伪静态地址
  （`forum-2-1.html`、`thread-68845-1-1.html`，与传统论坛习惯一致）、首页最新回复 +
  `/latest` 全站时间线、个人资料（签名/邮箱/改密并撤销其他设备会话）、回复历史
- **实时**：基于 SSE 的局部刷新 —— 新回复原位追加、编辑楼层即时替换、点赞计数同步、
  通知实时提醒（无需 WebSocket，自带断线重连）
- **互动**：点赞（含点赞名单浮层）、@提及与回帖通知（站内铃铛 + 邮件）、信任等级（新用户 → 正式成员 → 资深成员自动成长）、
  自定义头像、附件挂楼层、主题筛选（最新/精华/热门）、收藏与未读提醒、草稿箱、编辑历史、个人声望
- **配置**：可配置权限矩阵（后台逐格开关，即时生效）、Logo/页脚文案、服务条款与隐私政策在线编辑、
  安装向导（空库浏览器初始化）、自助注销账号
- **搜索**：PostgreSQL tsvector 全文搜索，中文 bigram 分词，零外部依赖
- **治理**：发帖审核队列（含原因码）、用户举报（队列处理/驳回，版主限管辖）、回收站、敏感词替换、
  批量删帖、防 CSRF / bcrypt / XSS 加固
- **合规**：服务条款与隐私政策页（后台可编辑）、注册同意勾选、个人数据导出（JSON）
- **后台**：仪表盘、版块管理、内容管理、用户管理（禁言/删号/改组）、站点设置（含上传限额与
  仅外链模式）、审计日志、公告管理
- **部署与传播**：单二进制（模板与静态资源 embed）、游客整页缓存、透明 gzip、
  SEO 基础（meta description / Open Graph / sitemap.xml / RSS）

## 权限模型

三个固定用户组 + 信任等级 + 版主管辖，全部权限判定收口在 `internal/perm`
（角色 → 权限点唯一映射表，`Allowed()` 单点查询，矩阵有单测锁定）：

| 权限点 | 管理员 | 版主 | 会员 | 说明 |
| --- | :-: | :-: | :-: | --- |
| `admin.panel` 进入后台 | ✅ | — | — | |
| `forum.manage` 版块/分类管理 | ✅ | — | — | |
| `content.moderate` 内容治理 | ✅ | ✅* | — | *版主限其管辖版块（`forum_moderators`） |
| `thread.sticky/digest/lock/delete` | ✅ | ✅* | — | 同上 |
| `moderate.queue` 审核队列 | ✅ | ✅* | — | |
| `recycle.bin` / `prune.run` | ✅ | ✅* | — | 版主不选版块时按管辖 IN 过滤，禁止全站 |
| `content.edit.own` / `delete.own` | ✅ | ✅ | ✅ | 自己的内容 |
| `content.edit.any` | ✅ | — | — | 任何人的内容 |
| `content.delete.any` | ✅ | ✅* | — | 版主限管辖版块 |
| `user.ban/delete/group` | ✅ | — | — | |
| `settings.edit` / `censor.manage` / `announce.manage` / `logs.view` | ✅ | — | — | |
| `upload.use` 本站上传 | ✅ | ✅ | ✅ | 另受站点开关与限额约束 |
| `post.link.direct` 直接发链接 | ✅ | ✅ | 按信任等级 | 新用户（TL0）发链接进审核队列 |

信任等级（自动成长，无需人工干预）：
新用户（TL0）→ 正式成员（访问 ≥3 天且读帖 ≥20，可直接发链接）→ 资深成员（访问 ≥14 天、读帖 ≥100 且发帖 ≥10，全站审核开启时免审核）。
扩展路径：需要自定义角色时，把 `rolePerms` 映射改为 DB 读取并提供矩阵界面即可，调用方零改动。
所有权限判定只走 `perm.Allowed` + 版块范围函数；编辑/删除表单与提交共用同一函数。

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
| `FORUM_DSN` | 无默认值（生产必须设置） | PostgreSQL 连接串 |
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

导入后重启生效，编辑器表情面板会出现对应分组。素材的版权与授权由导入者自行确认。

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
cmd/forumd/        入口（-seed / -import-smileys / -version）
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
- 数据库测试会清空目标库 schema；禁止使用业务库，禁止两个包共用同一测试库并行执行。`store` 已移除默认数据库连接，同样要求显式指定 `gobbs_test_` 测试库。
- API 回归覆盖 JSON 数据、字段隐私、登录/CSRF、发帖/编辑/删除、权限、审核、附件及 SSE；原 HTML 展示断言已移除，后续由 Nuxt 测试接替。

## 已知边界与取舍

- **单实例**：SSE Hub、限流和浏览计数仍为进程内状态，不支持完整的多实例/滚动升级。会话鉴权已改为数据库校验，撤销不依赖实例缓存；Go 整页 HTML 缓存已删除。
- **搜索天花板**：中文 bigram 分词对单字查询不敏感；命中聚合取 top-400 内存去重。
  中小规模完全够用，更大规模建议外接搜索引擎。
- **迁移策略**：启动时先幂等重放 `schema.sql`（保持当前完整形态，加表/加列友好），
  再按序执行 `assets/db/migrations/NNN_*.sql` 编号迁移并在 `schema_migrations` 登记
  （基线为版本 1）；破坏性变更（改约束/改类型）一律走编号迁移，只对存量库执行一次，
  迁移文件在全新库上重复执行必须幂等。上线前可运行 `./bin/forumd -check-backup`
  自检（DSN 可写、pg_dump 在 PATH、数据目录可写）。
- **登录会话**：Cookie 中为原始 token，库中仅存 SHA-256。
- **设备管理**：支持设备列表、重命名、单设备/其他设备/全部退出，接口见 [设备会话管理](docs/DEVICE_SESSIONS.md)。
- **独立积分**：账户、行为奖励、冲回流水、后台配置/调账及只读对账，见 [积分账本](docs/POINTS.md)；悬赏、签到和排行榜尚未接入。

## 许可

本项目以 **AGPL-3.0-or-later** 分发（见 [LICENSE](LICENSE)）。
第三方组件许可见 [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md)。
