# GoBBS

现代化的开源论坛系统：Go + PostgreSQL，单二进制部署，内置实时刷新、Markdown 编辑器、
全文搜索与完整的站点治理能力。

## 功能总览

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

```bash
# 0. 前置：Go 1.25+ 与 PostgreSQL 16+（推荐 18）
# 1. 建库（在 PostgreSQL 上执行）
CREATE DATABASE forum OWNER "youruser";

# 2. 编译
go build -o bin/forumd ./cmd/forumd

# 3. 启动（首次加 -seed 灌入演示数据；schema 启动时自动迁移）
FORUM_DSN="postgres://user:pass@127.0.0.1:5432/forum" \
FORUM_ADDR="127.0.0.1:8090" \
./bin/forumd -seed

# 4. 打开 http://127.0.0.1:8090 ，默认管理员：admin / admin123456（务必改密）
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
| `FORUM_DEV` | `0` | 置 1 时模板热重载（开发用） |
| `FORUM_PROD` | `0` | 置 1 启用 Secure Cookie（HTTPS 部署时） |
| `FORUM_SMTP_HOST` | 空 | SMTP 服务器；**为空则禁用邮件通知** |
| `FORUM_SMTP_PORT` | `25` | SMTP 端口 |
| `FORUM_SMTP_USER` / `FORUM_SMTP_PASS` | 空 | SMTP 认证（可选） |
| `FORUM_SMTP_FROM` | `noreply@gobbs.local` | 发件人地址 |

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
**升级**：替换二进制重启即可（schema 自动增量迁移；分词器升级会自动补齐搜索索引）。

**反向代理（nginx）**：

```nginx
location / { proxy_pass http://127.0.0.1:8090; }
location /api/live { proxy_pass http://127.0.0.1:8090; proxy_buffering off; }
```

生产环境务必：改默认管理员密码、`FORUM_PROD=1`（HTTPS 下 Secure Cookie）。

## 开发

```
cmd/forumd/        入口（-seed / -import-smileys / -version）
cmd/gobbsctl/      发布升级与健康检查工具
assets/            embed 打包：templates 模板、static 静态资源、db schema
internal/
  config/          环境变量配置
  db/              连接池与 schema 迁移
  store/           数据访问（查询/事务/缓存），全部 SQL 集中于此
  markdown/        goldmark 渲染 + 表情内联扩展
  smiley/          表情系统（内置 emoji + 自定义图片包）
  live/            SSE 推送中枢（按主题分组发布订阅）
  mail/            异步邮件（net/smtp）
  avatar/          确定性字母头像 SVG
  web/             路由、伪静态、中间件、handlers、模板渲染
scripts/           辅助脚本（素材导出、发布自检）
```

- 模板与静态资源修改后需重新编译（或 `FORUM_DEV=1` 热重载模板）
- 发布版本由 GitHub Actions 在 `v*` 标签推送时构建并附带 SHA256；生产升级使用 `gobbsctl`
- API 简要说明见 [docs/API.md](docs/API.md)
- 表结构见 `assets/db/schema.sql`（含逐表注释）
- 测试：`go vet ./... && go test ./...`
  - 纯函数测试（markdown/smiley/web 分页与伪静态）无需数据库
  - 集成与整站冒烟测试默认使用 `forum_test` / `forum_test_web` 两个独立库
    （可用 `FORUM_TEST_DSN` 覆盖），库不可达时自动跳过；需预先创建：
    `CREATE DATABASE forum_test OWNER "你的用户"; CREATE DATABASE forum_test_web OWNER "你的用户";`
  - 冒烟测试逐页断言完整渲染（`</html>` 收尾）与无错误文案，是"模板字段缺失
    截断"与"列清单与 Scan 不同步"两类事故的回归防线；列清单另有与
    information_schema 的一致性校验

## 已知边界与取舍

- **单实例**：整页缓存、会话缓存、SSE Hub、浏览计数均为进程内状态，不支持多实例/滚动升级；
  这是"零外部基础设施"定位的自觉取舍。缓存失效采用写突发合并（1s）+ 60s TTL 兜底。
- **搜索天花板**：中文 bigram 分词对单字查询不敏感；命中聚合取 top-400 内存去重。
  中小规模完全够用，更大规模建议外接搜索引擎。
- **迁移策略**：启动时先幂等重放 `schema.sql`（保持当前完整形态，加表/加列友好），
  再按序执行 `assets/db/migrations/NNN_*.sql` 编号迁移并在 `schema_migrations` 登记
  （基线为版本 1）；破坏性变更（改约束/改类型）一律走编号迁移，只对存量库执行一次，
  迁移文件在全新库上重复执行必须幂等。上线前可运行 `./bin/forumd -check-backup`
  自检（DSN 可写、pg_dump 在 PATH、数据目录可写）。
- **登录会话**：Cookie 中为原始 token，库中仅存 SHA-256。

## 许可

本项目以 **AGPL-3.0-or-later** 分发（见 [LICENSE](LICENSE)）。
第三方组件许可见 [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md)。
