# GoBBS 前端实现状态

更新日期：2026-09-29。本文区分“**已设计**（Ardot 有稿）/ **已实现**（代码完成）/ **已联调**（真实 API + SSR 验证）”，避免把设计稿或构建通过当成功能完成。

- 工程：frontend/（Next.js 14.2 + React 18 + TypeScript 5.9 + Arco Design React 2.66，CSS Modules + 主题变量）。
- 后端：第三批未改动后端；第四批按需补齐关注聚合流、列表摘要 / 封面投影与公开积分榜读取，并同步 OpenAPI 与专题文档。
- 设计稿：第三、四批实施期间 Ardot MCP 暂时不可达（fetch failed），未能重新读取在线文件；按本地 FRONTEND_PAGE_DESIGN.md / ARDOT_DESIGN_REVIEW.md 的页面说明与既有组件风格实现，恢复可达后应按画板复核。
- 联调环境：隔离数据库 forum_dev（由演示库克隆，未改动原 forum 库），Go API 127.0.0.1:8090，前端 127.0.0.1:3000。

## 第二批修复与新增（本轮）

| 项 | 处理 |
| --- | --- |
| 关注 / 订阅状态可能误判 | 后端在论坛、主题、标签、用户详情 DTO 中返回当前用户的 subscribed / following；关注列表每行返回 following。前端不再翻关系列表第一页；FollowButton / SubscribeButton 用 boolean 或 null 区分“确定未关注”与“状态未确认”。 |
| 用户主页把接口故障当 404 | 新增三态取数 Loaded（ok / notfound / error）：仅真实 404 调用 notFound()，其余渲染可重试的失败状态。 |
| 排行榜部分实现 | 页面与状态表均标注“仅统计快照，公开榜单接口待补”，不再表述为完整排行榜。 |
| CONTENT-03 编辑 / 删除 | 新增 /posts/[pid]/edit：标题与正文、版本号、409 冲突保留本地内容并提供“查看服务端最新内容”、草稿；主题页与编辑页按 capabilities.canEdit / canDelete 显示编辑与删除，删除首楼即删除主题。 |
| AUTH-05 邮箱验证与换绑 | 新增 /verify、/reset、/settings/email/confirm；/me/security 增加邮箱状态、重发验证邮件、两阶段换绑（当前密码 + 新邮箱 + 2FA）。 |
| 后端缺陷修复 | recomputeThreadLastPost 在主题无公开楼层时写入 NULL，触发 threads.last_post_at NOT NULL 约束，导致删除最后一楼返回 500；改为回退到主题创建时间与作者。 |

## 第三批新增（本轮）

| 模块 | 处理 |
| --- | --- |
| 投票 CONTENT-04 | 主题详情首楼后内嵌投票卡：创建（问题 / 选项 / 每人可选项 / 有效小时数）、单选与多选、投票、结果条与票数、提前结束；状态 pending/published/closed/rejected 与权限（canCreatePoll / canVote / canClosePoll）按能力展示；结果匿名。重复或改投由后端返回 409，界面原样提示，不做改投。 |
| 悬赏 CONTENT-05 | 主题详情内嵌悬赏卡：发布（金额 / 有效小时数，展示可用积分）、冻结与可用余额提示、取消退款、已结算与收款人展示、到期未退款提示；楼层按 canAccept / canUnaccept 显示采纳（采纳即结算悬赏）。金额、资格与状态转换以后端返回为准；提交超时不自动重试。 |
| 发帖衔接 | /new 增加“附加投票 / 附加悬赏”折叠区。主题与附加活动是两次请求：主题创建成功后附加失败时**保留主题**，给出失败原因与主题页重试入口，并禁用重复提交。 |
| 后台标签 ADMIN-04 | 列表与搜索、创建、编辑、启用 / 禁用；保存携带 version，409 冲突保留输入并提示重新读取。别名与绑定主题数的接口字段缺失时如实不展示。 |
| 后台用户 ADMIN-05 | 搜索、详情弹层、禁言 / 解禁、封禁登录 / 解封（踢会话）、用户组调整；危险操作二次确认；权限点被撤销时原样展示 403，不自行推断能力。删除账号不在本批。 |
| 站点设置 ADMIN-11 | 按后端 schema 生成分组表单（站点信息 / 注册与合规 / 内容与审核 / 上传 / 报表与保留），前后端双重校验、版本冲突保留输入、status 展示有效性与 SMTP 依赖；标记为无需重启。本批 schema 不含密钥字段，SMTP 凭据只来自部署环境变量。 |

## 第四批新增（本轮）

| 模块 | 处理 |
| --- | --- |
| 关注版块流 | 新增 `GET /me/feed/forums`：按当前账号的版块订阅关系，在数据库层过滤待审、删除与版块可见性后，按主题 `created_at`、`id` 倒序游标分页。通知关闭或静音仍算订阅，不因此移出流。 |
| 关注的人流 | 新增 `GET /me/feed/users`：只收录被关注者作为**主题作者**发布的主题，不把回复当新主题；过滤与排序同上。 |
| 签名游标 | 两类流使用 HMAC 游标，载荷绑定账号、流类型（forums/users）与排序（created）并校验签名；跨流、跨账号或篡改返回 422 `INVALID_CURSOR`。每页有 lookahead，只用 `hasMore`/`nextCursor`，不计算 total。 |
| 首页三标签 | 综合流保持分页；关注版块 / 关注的人接通真实接口，区分**未登录、尚无关注**（`meta.followingCount=0`）、**暂无内容**与**请求失败**四种状态；首屏 SSR，之后“加载更多”按游标追加并按主题 ID 去重；账号切换时组件按用户 ID 重新挂载，清空已加载分页。 |
| 列表摘要与封面 | 新增 `ThreadPreviews` 批量投影：一次查询取每页可见首楼正文前缀，Go 侧生成 ≤160 字纯文本摘要与首个站内 `/uploads/` 图片封面；SQL 内过滤主题 / 楼层待审与删除，读取时计算、无缓存过期问题。列表 DTO 新增 `excerpt`、`coverUrl`，封面加载失败自动隐藏。 |
| 公开积分余额榜 | 新增 `GET /leaderboard/points`：只读 Worker 的 `points` 快照，返回 `status`(ready/stale/unavailable)、`generatedAt`、`stale`、`ageSeconds` 与 `entries`（名次、字符串 ID、用户名、头像、余额）；生成与读取都排除封禁账号，旧快照同样过滤，名次紧凑重排；替换原先只有站点统计的过渡页。 |
| 聚合流查询 | `FollowingForumsFeed` / `FollowingUsersFeed` 复用主题投影与版块可见性过滤；`followingCount` 由专门计数查询提供。 |
| 流的组件身份（切换收尾） | `FollowFeedList` 的 `key` 与内部状态身份 = **账号 + 流类型 + 首屏游标 + 实际分页大小**：切换标签即重新挂载并重置 `items`/`cursor`/`hasMore`，属性变化时渲染期同步重置兜底。卸载时 abort 在途请求并用请求序号丢弃迟到结果，避免旧流内容 / 旧游标串到新流（否则会被判为 `INVALID_CURSOR`）。 |

## 第五批 5A 新增（本轮）

| 模块 | 处理 |
| --- | --- |
| 会员等级 ADMIN-06 | 等级（名称 / ID / 顺序 / 门槛 / 徽章 / 17 项权限 / 9 项额度）、游客权限、5 类成长规则、版块访问覆盖；保存走**先预览拿令牌再发布**的两段式，版本或令牌不符返回 409 且保留输入；用户会员状态查询、经验流水与人工调整（等级 / 锁定 / 经验增减 + 原因 + 幂等键，提交前弹确认并显示影响范围）。经验与积分明确分开。 |
| 称号与条件 ADMIN-07 | 称号列表、创建 / 编辑（名称、说明、徽章、状态、发放方式、条件组、有效期、排序）、预览（符合人数 / 新增发放）、补发任务与审计日志查看；条件事件取自后端固定 10 项指标（发帖 / 回复 / 获赞 / 精华 / 采纳 / 单帖最高赞 / 经验 / 活跃天数 / 注册天数 / 邮箱验证），支持全部满足或任一满足，自动称号至少 1 条、手动称号 0 条。用户获得记录与授予 / 撤销单独一块，授予 / 撤销携带称号定义当前版本、原因与幂等键。 |
| 互动配置 ADMIN-08 | 积分规则（固定 6 类 + 采纳经验，开关 / 分值 / 每日上限 / 可冲回）与互动配置（投票开关与上限、悬赏积分区间与天数、签到开关 / 经验 / 积分 / 时区）分区编辑；整对象提交带版本，冲突保留输入并给出中文提示。 |
| 投票管理 | 待审队列（游标 50/页、加载更多去重）、详情弹层（问题 / 选项 / 票数 / 状态）、通过 / 驳回 / 关闭，均带对象、后果与原因说明；不提供改票 / 改选项 / 改计票。 |
| 悬赏管理 | 按状态筛选（进行中 / 已结算 / 已取消并退款 / 已过期并退款 / 全部）与「只看退款失败」、退款诊断汇总、详情弹层、重试退款（仅进行中且退款失败）与取消并退款（仅进行中），均带对象、后果与必填原因；已支付悬赏不显示可操作入口，服务端返回 409 时原样提示，不自动重试。 |
| 导航与守卫 | 后台导航新增会员等级 / 称号与条件 / 互动配置 / 投票管理 / 悬赏管理，均标记为已实现；五个页面按后端权限点渲染入口，权限被撤销时原样展示 403 / 无权限状态。 |

## 第五批 5B 新增（本轮）

| 模块 | 处理 |
| --- | --- |
| 报表 ADMIN-09 | `/admin/analytics`：积分快照（points）与站点报表（site）分卡展示存储桶起点 / 统计日边界、生成时间、距今秒数、刷新与过期阈值，并标注正常 / 数据已过期；积分快照渲染名次表，站点报表渲染标量字段 + 完整快照 JSON。另含异步版块统计与搜索索引队列状态。查询失败、无权限、尚未生成（404）三种情况分别提示，不用零值掩盖。 |
| 邮件队列 ADMIN-10 | `/admin/notifications`：状态筛选（全部 / 待发送 / 发送中 / 已发送 / 已失败 / 已取消）+ 队列计数 + 游标加载更多 + 详情弹层；仅“已失败”任务显示重试（后端也只允许 dead 且未过期），SMTP 未配置时显示横幅并禁用重试；重试失败或超时只提示并引导“重新读取”，不自动重复操作。收件人、关联楼层与令牌字段由接口隐藏，页面不展示也不写日志。 |
| 权限 ADMIN-12 | `/admin/permissions`：41 个权限点 × 会员 / 版主 / 管理员矩阵，表单键 `allow.<role>.<point>` 提交；管理员对“进入管理后台 / 编辑权限矩阵”硬保护，界面锁定。页面明确区分权限点、用户组与会员等级权限三者独立，失败保留输入。 |
| 设备会话（小范围后端补充） | 管理员侧原本没有设备会话接口。新增权限点 `sessions.manage` 与 `GET|DELETE /admin/users/{uid}/sessions[/{sessionId}]`（复用 `sessions` 表与既有撤销语义），页面只读列表 + 撤销单个 / 全部，确认框展示目标账号、设备、影响范围，并提示撤销自己会被下线。 |
| 系统与会员诊断 | `/admin/diagnostics`：连接池 / 锁等待 / 事务与缓存命中率 / 版块统计与搜索队列，按阈值给出“后端明确报告的异常”清单；会员权限判定诊断按 userId + action（+ 可选版块）回放后端 `memberDecision`，展示允许 / 拒绝、原因与额度；会员审计日志只读最近 30 条。自动刷新 30 秒、页面隐藏或离开即停止。 |
| 导航与守卫 | 后台导航新增排行榜与报表 / 邮件队列 / 权限与会话 / 系统诊断并标记已实现；四个页面按后端权限点渲染，权限被撤销时显示无权限状态。 |

## 第五批 5C 新增（本轮）

| 模块 | 处理 |
| --- | --- |
| 安装向导 AUTH-06 | `/setup`：SSR 读取 `GET /api/v1/setup`，只在 `required=true` 时渲染表单；已安装站点直接 307 到 `/login`。字段与真实契约一致（`site_name / username / email / password / confirm_password`），匿名 CSRF 走 `/api/v1/session`，**不收集数据库凭据**。区分请求失败（后端不可用，不给表单）、提交中、成功、已初始化、网络异常；网络异常先重读安装状态再决定是否允许重试；成功后刷新会话并按实际登录结果进入 `/admin` 或 `/login`。 |
| 公开 Markdown | `/content/threads/{id}.md?page=N`：输出标题、规范 HTML 地址、Markdown 地址、作者、发布时间、最后更新、页码、正文、当前页回复与下一页链接；站内相对链接与图片转成可信基址的绝对地址。**固定游客身份**（`forwardCookies:false`，不转发请求者 Cookie），只调用公开读接口，不触发阅读进度、经验或已读写入。`Content-Type: text/markdown; charset=utf-8`、`Cache-Control: no-store`、`Link: <...>; rel="canonical"`；不存在/不可见 404，后端故障 503，不返回成功空文档。 |
| 内容协商 | `middleware.ts` 只匹配 `/threads/{id}`（恰好一段）与 `/content/threads/{id}.md`：显式 `.md` 重写到 Markdown 处理器；`Accept` 明确优先 `text/markdown` 时切换到同一表示的 Markdown。按 q 权重解析：`q=0` 视为拒绝，通配符（all-types / text-any）不视为要求 Markdown，等权按出现顺序。登录、后台、API、附件、Next 内部请求都不参与切换。 |
| robots / sitemap / RSS | Next.js 生成 `/robots.txt`（允许公开 Markdown 目录，私有页面限制与 noindex 策略一致，明确不替代访问控制）、`/sitemap.xml`（**sitemap 索引**，分片见下）、`/rss.xml`（全站最近发布的公开主题，稳定 `guid`、真实 `pubDate`、XML 转义）。三者与 canonical 共用可信基址 `SITE_URL`（`FORUM_SITE_URL`），不从请求 Host 推导。 |
| 最小公开索引接口 | 新增只读 `GET /api/v1/index/threads?sort=created|updated&page=&size=≤100`：**后端固定游客可见范围**（未删除、未待审、按匿名版块可见性过滤，不跟随调用账号），返回 `createdAt / lastPostAt / updatedAt`，给 sitemap / RSS 用，有界分页。OpenAPI 由 204 增至 **205** 条操作。 |

## 页面状态

| 页面 ID | 路由 | 已设计 | 已实现 | 已联调 | 说明 |
| --- | --- | :--: | :--: | :--: | --- |
| PUB-01 | / | ✓ | ✓ | ✓ | /home + /threads SSR；综合流分页，关注版块 / 关注的人为真实聚合流（SSR + 游标加载更多） |
| PUB-02 | /forums | ✓ | ✓ | ✓ | 分类分组、统计、最新活动 |
| PUB-03 | /forums/[fid] | ✓ | ✓ | ✓ | 置顶/普通分区、排序、分页、订阅（状态来自详情 DTO） |
| PUB-04 | /threads/[tid] | ✓ | ✓ | ✓ | 正文/楼层/附件/引用/点赞/收藏/举报/回复/编辑/删除 |
| PUB-05/06 | /tags、/tags/[slug] | ✓ | ✓ | ✓ | 目录、详情与订阅 |
| PUB-07 | /search | ✓ | ✓ | ✓ | SSR + noindex |
| PUB-08 | /users/[id] | ✓ | ✓ | ✓ | 主题/回复、关注；404 与接口故障已区分 |
| AUTH-01/02 | /login、/register | ✓ | ✓ | ✓ | 含 2FA 挑战、验证码、条款 |
| AUTH-03/04 | /password/forgot、/password/reset | ✓ | ✓ | ✓ | 防枚举统一响应；邮件历史路径 /reset 已兼容 |
| AUTH-05 | /verify、/settings/email/confirm | ✓ | ✓ | ✓ | 邮箱验证与换绑确认；需 SMTP 配置才能真正投递 |
| AUTH-06 | 安装向导 | ✓ | — | — | 后续批次 |
| MEMBER-01～09 | /me/* | ✓ | ✓ | ✓ | 通知、私信、安全（含邮箱）、资料均已接真实接口 |
| CONTENT-01 | /new | ✓ | ✓ | ✓ | Markdown 编辑预览、草稿、图片上传、待审反馈 |
| CONTENT-02 | /threads/[tid]/reply | ✓ | ✓ | ✓ | 跳转到主题页内联回复编辑器 |
| CONTENT-03 | /posts/[pid]/edit | ✓ | ✓ | ✓ | 冲突时区分“本地内容所基于的版本”与“服务端最新版本”，刷新只更新对比数据，明确确认合并/覆盖后才采用新版本提交；删除含首楼删主题 |
| CONTENT-04/05 | 主题内嵌投票 / 悬赏 | ✓ | ✓ | ✓ | 创建、投票、结果、结束、发布、冻结、采纳结算、取消退款 |
| CONTENT-06 | /checkin | ✓ | ✓ | ✓ | 状态、签到、历史 |
| CONTENT-07 | /leaderboard | ✓ | ✓ | ✓ | 公开积分余额榜；只读快照，展示数据口径、生成时间与过期状态 |
| ADMIN-01～03 | /admin、/admin/moderation、/admin/forums | ✓ | ✓ | ✓ | 概览、审核与举报、分类与版块 |
| ADMIN-04 | /admin/tags | ✓ | ✓ | ✓ | 查询、创建、编辑、启停；版本冲突保留输入 |
| ADMIN-05 | /admin/users | ✓ | ✓ | ✓ | 查询、详情、禁言、封禁登录、用户组；删除留后续 |
| ADMIN-11 | /admin/settings | ✓ | ✓ | ✓ | schema 分组表单、分组校验、保存、状态与重启说明 |
| ADMIN-06 | /admin/membership | ✓ | ✓ | ✓ | 等级 / 门槛 / 徽章 / 权限 / 额度 / 成长规则；预览令牌后发布；用户查询与人工调整 |
| ADMIN-07 | /admin/titles | ✓ | ✓ | ✓ | 称号配置、条件组、预览、补发任务与日志；用户获得记录与授予 / 撤销 |
| ADMIN-08 | /admin/engagement | ✓ | ✓ | ✓ | 积分规则与投票 / 悬赏 / 签到配置，整对象带版本保存 |
| 投票管理 | /admin/polls | ✓ | ✓ | ✓ | 待审队列、详情、通过 / 驳回 / 关闭 |
| 悬赏管理 | /admin/bounties | ✓ | ✓ | ✓ | 状态筛选、退款诊断、详情、重试退款、取消退款 |
| ADMIN-09 | /admin/analytics | ✓ | ✓ | ✓ | 快照口径 / 时区 / 生成时间 / 过期状态；队列状态；区分未生成与查询失败 |
| ADMIN-10 | /admin/notifications | ✓ | ✓ | ✓ | 状态筛选、计数、详情、失败任务重试；SMTP 未配置与状态冲突明确提示 |
| ADMIN-12 | /admin/permissions | ✓ | ✓ | ✓ | 权限矩阵保存 + 用户设备会话查看 / 撤销 |
| 系统诊断 | /admin/diagnostics | ✓ | ✓ | ✓ | 数据库 / 队列 / 快照指标 + 会员判定诊断 + 会员审计；只读 |
| AUTH-06 | /setup | ✓ | ✓ | ✓ | 单步表单（无数据库凭据）；区分未安装 / 提交中 / 成功 / 已初始化 / 请求失败 |
| 公开 Markdown | /content/threads/{id}.md | ✓ | ✓ | ✓ | 固定游客身份；标题 / 规范地址 / 正文 / 当前页回复 / 分页 / 下一页；越界页 404 |
| robots·sitemap·RSS | /robots.txt · /sitemap.xml · /rss.xml | ✓ | ✓ | ✓ | 游客可见数据、索引 + 有界分片、真实更新时间、同一可信基址、失败 503 |

## 5C 收尾修复（本轮）

针对复核发现的 7 项问题做的集中修复，全部有定向验证：

| 问题 | 修复 | 验证 |
| --- | --- | --- |
| robots 阻止 Markdown 抓取 | 从 disallow 列表移除 `/content/`，保留私有页面限制 | `/robots.txt` 中 `/content/` 出现 0 次 |
| sitemap 没有真正分片 | `/sitemap.xml` 改为 **sitemap 索引**，列出 `/sitemap/pages.xml`（静态 + 版块）、`/sitemap/threads-{i}.xml` 与 `/sitemap/tags-{i}.xml`；主题每片最多 5 × 100 条，标签每片最多 10 页，索引按上游分页元数据生成全部分片 | 初轮验证目录与主题输出；后续分片边界回归见下方补充记录（新增标签分片与数字越界校验） |
| Markdown 越界页返回成功空文档 | 取接口 `meta.totalPages` 校验页码，`page > totalPages` 返回 404；合法后续页的规范地址、Markdown 地址与上一页链接都保留 `?page=` | `?page=10000` → 404；临时把 `posts_per_page` 调成 5 后 `?page=2` → 200 且 canonical 为 `…/threads/1?page=2`，`?page=3` → 404，HTML `?page=2` 仍 200（设置已还原） |
| Accept 忽略通配符权重 | 按 RFC 9110 先算每种表示的**有效权重**（取最具体匹配范围的 q 值），再比权重、具体程度与出现顺序；通配符参与计算 | 12 组 Accept 全对：`text/markdown;q=0.1, */*;q=1` → HTML；`text/markdown;q=0.5, text/*;q=0.9` → HTML；`text/markdown;q=0` → HTML；其余原有用例不变 |
| 公开索引未在后端固定游客身份 | `indexThreads` 显式调用 `MembershipAccess(ctx, 0)` 并用 `WithVisibleForums` 注入游客可见范围，不再跟随调用账号 | 匿名与管理员 Cookie 请求索引结果 `cmp` 完全一致（total 均为 19）；该修复前只有前端不转发 Cookie 才维持口径 |
| 上游故障被吞成成功结果 | RSS 读取失败返回 503；sitemap 索引、目录分片与主题分片失败均返回 503，只有“成功但为空”才返回空文档 | 把 Next 指向不可用上游：`/rss.xml`、`/sitemap.xml`、`/sitemap/pages.xml` 均 503 且带说明文本；正常实例仍 200 |
| “最后更新”口径不完整 | 新增真实内容更新时间 `updatedAt = max(最后回复时间, 可见楼层最后编辑时间)`：公开索引与主题详情都返回，sitemap `lastmod` 与 Markdown“最后更新”改用它 | 编辑首楼后 `updatedAt` 前进到编辑时刻（`lastPostAt` 不变），索引、sitemap `lastmod`、Markdown 三处同步；待审楼层不计入，编辑触发待审时该主题自然退出公开输出 |

### sitemap 分页边界补充

- 标签移至独立分片；`pages.xml` 只含静态入口和版块。合法数字分片也先核对总量，越界不再返回 200 空文档；超大编号在请求上游前拒绝。
- 新增 `frontend/tests/sitemap.test.cjs`，运行 `cd frontend && node --test tests/sitemap.test.cjs`。5 项定向回归通过，直接执行真实路由处理器，仅替换上游 API 为隔离夹具：301 个标签、501 个主题均可由索引逐片发现且无重复；整片边界越界 404；非法/超大编号不访问上游；空站零号分片有效；索引与分片中途上游故障均返回 503。
- 夹具验证不写数据库，不能替代正式域名和反向代理验收。
- 本地构建后真实 HTTP 验证：索引包含目录、主题、标签 3 个分片；目录 12 条、主题第 0 片 19 条、标签第 0 片为空（当前无公开标签）。`threads-1/999`、`tags-1/999`、超大整数与非法编号均为 404，正常分片为 200 且 `no-store`；首页、RSS、robots 与 API 均正常。
- 本轮 `pnpm lint`、`pnpm typecheck`、`pnpm build` 与 `git diff --check` 通过。仅重启本地 Next 服务加载新构建，Go API 未重启，未修改数据库、未提交或推送。

## 待补接口与已知限制

1. **热门话题 / 热门作者 / 多维排行榜**：目前只有积分余额榜一个公开榜单；热门作者、热门话题、日榜 / 周榜等新排名规则未实现，右栏继续使用真实的公告、标签与统计。
2. **摘要与封面口径**：摘要是去标记的纯文本（会保留图片 alt 文本与代码块内容），不是渲染级还原；封面只取首个站内 `/uploads/` 图片，外链与多图轮播不支持，取不到时展示纯文字信息流。
3. **关注流一致性**：游标按 `created_at,id` 推进；浏览期间被删除或转为待审的主题会从后续页消失，不做位置补偿；已加载的上一页不会自动撤回。
4. **引用正文摘要**：引用关系只含楼层与作者，不显示被引用正文。
5. **私信专用 SSE / 附件 / 撤回**：后端未提供，页面不做假设。
6. **投票后台只有待审队列**：`GET /admin/polls` 只返回 `state='pending'`（游标 50/页），没有按状态筛选的通用列表或统计接口；已发布 / 已结束的投票只能在主题页查看。若需要完整投票台账，需要后端补一个只读列表接口。
7. **称号后台不代用户佩戴**：佩戴由用户自己通过 `PUT /me/title` 完成，后台只有授予 / 撤销；称号停用会清空佩戴，这是后端行为，前端不额外恢复。
8. **会员诊断与审计**：`diagnose` 与 `logs` 已在 5B 接入 `/admin/diagnostics`；审计接口只有全站分页，不支持按用户筛选。
9. **邮件队列没有详情 / 取消接口**：详情来自列表行；重试只对 `dead` 且未过期任务有效；收件人、关联楼层与令牌字段由接口隐藏，页面不展示也不写日志。
10. **报表只有两个快照**：只支持 `points` / `site`，没有时间范围、图表或按维度筛选；页面把“尚未生成（404）”和“查询失败”分开呈现，不用零值掩盖。
11. **诊断指标是实时读取**：不带生成时间，“指标过期”只适用于报表快照；诊断页按阈值提示后端明确报告的异常，不做趋势图。
12. **管理员设备会话是本批新增契约**：新增 `sessions.manage` 权限点与 3 条路由，权限矩阵多一行、OpenAPI 由 201 增至 204 条操作；撤销的是会话，不封禁账号、不改密码或权限。
14. **Accept 协商的 Vary**：Markdown 表示（路由处理器）会输出 `Vary: Accept`；**HTML 页面响应由 App Router 重写 Vary**（只保留 RSC 相关值），middleware 的 append 与 `next.config.mjs` 的 headers 都无法覆盖，已在代码注释与验证记录中说明。两种表示都是 `no-store`（HTML 为 `private, no-store`），共享缓存无法存储，因此不存在格式串用风险；其余协商语义（q 值、q=0、通配符、等权顺序）均已实测正确。
15. **RSS 首版最小**：只有全站最近发布主题的标题、链接、guid、pubDate 与作者，没有正文摘要与分类；稳定标识使用规范 HTML 地址。
16. **sitemap 为索引 + 有界分片**：`/sitemap.xml` 是 sitemap 索引，主题按每片 5 × 100 条分片（`/sitemap/threads-{i}.xml`），静态入口与版块在 `/sitemap/pages.xml`，标签单独按每片最多 10 页组织为 `/sitemap/tags-{i}.xml`。索引按主题总量与标签总页数发现全部分片，不再截断前 300 个标签。主题分片按**真实内容更新时间**倒序；数字越界、负数、非法格式以及编号或页码运算超出安全整数范围均返回 404。空站保留各类型第 0 片（200 空列表），其余编号返回 404。为判断边界，非首分片另读一次第一页元数据，主题最多 6 次、标签最多 11 次上游请求。
17. **安装向导为单步**：设计稿是含数据库凭据的 4 步向导，但真实接口不接受数据库字段且本批明确禁止前端收集数据库凭据，因此只保留站点 + 管理员一步；邮件步骤同样无对应接口。该差异为既定方案（数据库与 SMTP 交由部署配置负责），设计稿同步调整。
18. **`updatedAt` 口径**：等于“最后回复时间”与“**可见**楼层最后编辑时间”的较大值；被删除或待审楼层的编辑不计入。编辑触发待审时，该主题会整体退出公开索引、sitemap 与 RSS，这是审核语义而非时间缺陷。
13. **邮件投递**：本地未配置 SMTP 时，换绑与重发验证邮件返回 EMAIL_UNAVAILABLE，界面给出明确提示；生产需配置 FORUM_SMTP_* 与 FORUM_MAIL_KEY，并把 FORUM_SITE_URL 指向前端域名，邮件里的 /verify、/reset、/settings/email/confirm 才由 Next.js 承接。

## 验证记录（第二批）

前端门禁：

    cd frontend
    pnpm lint       # ✔ No ESLint warnings or errors
    pnpm typecheck  # ✔ 通过
    pnpm build      # ✔ 37 条路由（含 /posts/[pid]/edit、/verify、/reset、/settings/email/confirm）

后端定向测试（隔离库 gobbs_test_store / gobbs_test_api）：

    FORUM_TEST_DSN=.../gobbs_test_store FORUM_REQUIRE_TEST_DB=1 go test ./internal/store \
      -run 'Test(DeletePostConcurrentCounters|RestoreKeepsIndividualDeletes|PrunePostsRecompute|AsyncForumStatsEventuallyReconciles|ForumStatsExcludePendingThread|MoveThreadStatsBothSides|SubscriptionsDefaultsAndDurableBatch)'
    # ok dzforum/internal/store

    FORUM_TEST_DSN=.../gobbs_test_api FORUM_REQUIRE_TEST_DB=1 go test ./internal/api \
      -run 'Test(OpenAPICommunityRelations|OpenAPICommunityTags|TagsAndSubscriptionAPI|FlowReviewReportDeleteAtomic|OpenAPIModerationWorkflow)'
    # ok dzforum/internal/api

真实联调：

- **关系状态**：匿名取详情无 subscribed / following；登录后返回确定布尔值；关注/取关、订阅/取消订阅后详情与列表行状态同步（实测 false → true → false）。
- **删除**：删除最后一楼（首楼待审）成功，楼层 404；删除首楼返回“主题已删除”，主题 404；修复前的 500 不再复现。
- **编辑（单窗口）**：合法版本保存成功并递增版本；使用旧版本再次提交返回 409，提示“当前版本 3，你基于版本 2”，本地内容保留。
- **编辑（双窗口定向验证）**：两个窗口同开同一楼层。A 先保存（v2 → v3）；B 基于 v2 保存 → 冲突面板出现、保存按钮禁用、并排展示“我的内容 / 服务端最新”、首楼显示标题差异；此时服务端内容与版本**未变**（无静默覆盖）。B 点击“我已合并，采用最新版本”后面板关闭、保存恢复；再次保存成功（v3 → v4，内容为 B）。
- **邮箱**：本地 SMTP 收件桩验证注册验证邮件与换绑确认邮件；调用邮箱验证接口后 /me 显示已验证，确认换绑后邮箱变更并保持已验证。
- **页面**：/posts/[pid]/edit 作者 200、匿名 307 跳登录、不存在 404；/verify 无令牌显示“链接无效”、坏令牌显示“操作未完成”；/reset 307 到 /password/reset；/settings/email/confirm 匿名 307 跳登录。

未验证：AUTH-06、Markdown 公开出口、robots/sitemap/RSS；生产 SMTP 真实投递（仅用本地收件桩验证协议与令牌流程）；Ardot 在线设计复核（本轮 MCP 不可达）。

## 验证记录（第三批）

前端门禁：pnpm lint、pnpm typecheck、pnpm build 全部通过（构建含 /admin/tags、/admin/users、/admin/settings 与主题内嵌投票 / 悬赏）。

互动（隔离库 forum_dev；为让新建主题公开，验证期间临时把 moderate_enabled 设为 0，验证后已恢复为 1）：

- **投票**：创建 201 published（3 选项）；另一账号投票后 voters=1、myChoices 正确；改投返回 409 ENGAGEMENT_CONFLICT；作者提前结束成功（state=closed）；主题摘要 poll 正确投影；未投票、已投票、已结束三种展示均已核对。
- **悬赏**：发布 amount=1 → active；另一账号回复后作者采纳 → awarded，recipientId/postId 正确，积分在作者与答主之间转移；另一主题发布 amount=5（balance 54、frozen 5、available 49，balance 不减）后取消 → canceled、settledAt 写入、frozen 归零、balance 不变。
- **积分**：/me/points 返回 balance / frozen / available；后台人工加分为 /admin/points/users/{uid}/adjust（带幂等 key）。

后台：

- **标签**：创建 v1 → 更新 v2 → 用旧版本保存 409 CONFLICT → 禁用 v3；列表与搜索命中。
- **用户**：搜索命中；禁言后 isBanned=true 且理由落库 → 解禁；封禁登录 isBlocked=true → 解封；用户组 0→2→0。
- **设置**：GET version=1；PUT 保存 footerText → v2；旧版本再存 409 SETTINGS_CONFLICT；threadsPerPage=1 返回 422「threadsPerPage: 必须为 5 到 100 的整数」；恢复后 v3；status valid=true、smtpEnabled=false；schema 20 个字段、requiresRestart=false。

页面与响应式：

- /threads/{tid} SSR 含投票卡与悬赏卡（结果条、参与者、状态、采纳按钮），首楼显示编辑 / 删除。
- /admin/tags、/admin/users、/admin/settings 管理员返回 200 且含真实数据；匿名访问 /admin/tags 307 跳登录；非管理员渲染“无访问权限”状态而不是空白页。
- 桌面 1440 与手机 375 截图核对；设置页长表单的分组、字段提示与状态区可用（已移除会遮挡内容的 sticky 操作条）。

浏览器点击回归（Playwright + Chrome）：主题页投票卡选区 → 提交投票 → 显示“你已投过票”与 100% 结果条；悬赏卡发布悬赏 → 填金额 5 → 提交 → 状态变为 active、frozen=5（验证后已取消退款，frozen 归零）；全程无控制台报错。标签弹层与用户操作按钮仍只通过 API + SSR 核对，未逐个点击。

## 验证记录（第四批）

后端定向测试（隔离库 gobbs_test_store，新增用例，走真实 PostgreSQL）：

    FORUM_TEST_DSN=.../gobbs_test_store FORUM_REQUIRE_TEST_DB=1 go test ./internal/store -run 'Test(PlainExcerptAndCover|FollowingFeedsFilterOrderCursorAndPreviews)' -count=1
    # ok dzforum/internal/store

    go run ./scripts/api-contract -check   # PASS: OpenAPI covers 201 registered API operations
    FORUM_TEST_DSN=.../gobbs_test_api FORUM_REQUIRE_TEST_DB=1 go test ./internal/api -run 'TestOpenAPI' -count=1
    # ok dzforum/internal/api

真实联调（forum_dev）：

- **关注版块流**：订阅版块 3 后返回该版块主题，按 created_at 倒序；把订阅置为 `enabled=false, notify_*=false, muted_until=now()+1d` 后仍返回全部主题且 `followingCount` 不变 —— 通知关闭 / 静音不等于取消订阅。
- **关注的人流**：关注 emailtest01 后只返回其作为作者的主题；smoketest01 自己发的主题与其楼下的 emailtest01 回复都**不**进入该流（回复不算新主题）。
- **游标**：合法 nextCursor 翻到下一页；篡改一位、跨流（forums 游标用于 users）、跨账号（smoketest01 游标给 emailtest01）都返回 422；匿名 401。
- **摘要 / 封面**：列表返回 `excerpt`（如「第一段摘要文字，用于验证纯文本摘要。 封面 结尾补充文字。」）与 `coverUrl=/uploads/cover-a.png`；摘要中不含 Markdown URL。
- **公开积分榜**：`status=ready`，entries 为 smoketest01/emailtest01/DBA老王；隐藏快照 → `unavailable` 且 `generatedAt=null`；把 generated_at 改早 2 小时 → `status=stale, stale=true, ageSeconds=7200` 且仍返回上次数据；封禁 DBA老王后旧快照立即不再包含他且名次紧凑重排为 [1,2]，解封后恢复。

前端门禁与页面：

- pnpm lint / typecheck / build 全部通过（新增 `/api/v1/me/feed/*` 与 `/leaderboard/points` 类型、关注流组件与排行榜页）。
- 匿名 /?feed=forums 显示登录提示；emailtest01（无关注）分别显示“还没有关注的版块 / 用户”；smoketest01 的论坛流与人的流标题集合正确（人的流不含自己发的主题）。
- /leaderboard 在快照缺失 / 过期 / 正常三种状态下分别渲染“榜单快照尚未生成 / 快照已过期 / 快照正常”。
- 浏览器点击回归：`?feed=forums&limit=2` 首屏 2 条，连续两次“加载更多”后为 4 → 6 条且**无重复**；控制台仅有一条 404，来自测试内容里故意不存在的 `/uploads/cover-a.png`（封面加载失败已自动隐藏，DOM 中无破图）。
- **标签切换定向回归**（同一账号）：关注版块加载第二页（2 → 4 条）→ 切到关注的人（3 条，**不含**仅版块流可见的“不该出现在关注人流的主题”）→ 关注的人小分页加载更多（2 → 3 条）→ 切回关注版块（11 条，重新包含版块流独有主题）；全程无重复、无 `INVALID_CURSOR` 提示。竞态用例：点“加载更多”后立即切标签，旧流在途请求未把结果追加到新流（新流仍为 3 条且不含旧流内容）。
- 桌面 1440 与手机 375 截图核对首页综合流 / 关注流与排行榜。

## 验证记录（第五批 5A）

后端定向验证（隔离库 forum_dev，真实 PostgreSQL；本轮未改动后端）：

- **会员配置**：预览返回 `{users:8, affectedUsers:0, upgrades:0, token:64}`；用令牌发布后版本 3 → 4；用旧版本再预览 409 `MEMBERSHIP_CONFLICT`；把等级 0 的门槛改成非 0 返回 422「会员配置无效：默认等级 0 必须保留且无升级门槛」。
- **称号**：创建草稿（v1）→ 预览 `{eligible:5,newAwards:0}` → 发布为 active 自动称号（v2）并生成 1 条 pending 补发任务；旧版本再保存 409 `TITLE_CONFLICT`；授予后用户记录 `{status:'earned',source:'manual'}`，撤销成功。
- **积分规则**：整对象保存版本 1 → 2；旧版本 409 `POINTS_CONFLICT`；删掉一类规则 422 `POINTS_INVALID`。
- **互动配置**：保存版本 1 → 2；旧版本 409 `ENGAGEMENT_CONFLICT`。
- **投票**：待审队列读到 2 条；通过后 `published`，队列只剩 1 条。
- **悬赏**：`state=active` 列出进行中悬赏；对没有退款错误的悬赏调用重试返回 409 `ENGAGEMENT_CONFLICT`；取消退款后 `state='canceled'` 且写入 `settledAt/note`，冻结积分归零（balance 57 / frozen 0）。

浏览器点击验收（Playwright + Chrome，管理员会话，每个模块都实际点击写操作）：

| 模块 | 点击路径 | 结果 |
| --- | --- | --- |
| 会员等级 | 预览影响 → 发布配置 | 预览面板出现（扫描用户 / 受影响 / 升级 / 锁定），发布后显示「配置已发布」 |
| 称号与条件 | 新建称号 → 预览 → 保存 | 预览提示「符合条件 N 人」，保存后列表出现「浏览器联调称号」（草稿 v1） |
| 互动配置 | 切换积分规则开关 → 保存积分规则 | 显示「已保存，版本提升到 v3」 |
| 投票管理 | 待审行「通过」→ 填原因 → 确认 | 显示「已处理」，队列由 2 条降为 1 条 |
| 悬赏管理 | 进行中行「取消并退款」→ 填原因 → 确认 | 显示「已取消并退款」，进行中列表归零、冻结积分归零 |

全程无控制台报错。桌面 1440 与手机 375 截图核对五个页面。

## 验证记录（第五批 5B）

后端小范围补充（本轮唯一的后端改动）：新增权限点 `sessions.manage` 与 3 条管理员设备会话路由（复用 `sessions` 表），OpenAPI 从 201 增至 **204** 条操作；权限点总数 40 → **41**。其余模块只用现有接口。

定向 API 验证（隔离库 forum_dev + 本地 SMTP 收件桩 127.0.0.1:2525）：

- **快照状态**：隐藏 `points` 快照 → `/admin/analytics/points` 返回 404（页面显示“快照尚未生成”）；把 `generated_at` 改早 2 小时 → `stale=true`、`ageSeconds=7200` 且仍返回上次内容（页面显示“数据已过期”）；恢复后 `stale=false`。
- **邮件队列**：状态筛选生效；非法状态 422；注入一条 `dead` 任务后重试返回“已重新加入队列”，Worker 处理后写回 `sent`；对非 dead 任务重试返回 `EMAIL_RETRY_CONFLICT`。一条 `email_verify` 任务重试后被后端判定为 `cancelled`（邮箱已验证），说明重试不等于强制投递。**真实投递**：注入一条 `email_changed` 任务并重试，本地收件桩收到新邮件（`mail-0000.txt` 被写入，mtime 00:13），任务状态 `sent` 且写入 `sent_at`。
- **权限**：撤销管理员的 `email.manage` 后立即调用 `/admin/email-jobs` 返回 **403**；`admin.panel` 与 `permissions.edit` 对管理员仍强制为 true（硬保护）；恢复原矩阵后接口回到 200。
- **设备会话**：`GET /admin/users/20/sessions` 返回 6 类字段（含脱敏 IP 与状态）；撤销单会话返回 `{affected:1,action:"revoke"}`，列表中该会话转为 `revoked`；不存在的用户返回 404。
- **诊断**：`/admin/diagnostics` 返回连接池、锁等待、队列与缓存命中率；会员日志可读；`diagnose` 对 LV0 用户 `post.link.direct` / `post.skip.moderate` 返回「当前等级未开放此操作」，非法 userId 返回 422。

浏览器点击验收（Playwright + Chrome，管理员会话）：

| 页面 | 点击路径 | 结果 |
| --- | --- | --- |
| 排行榜与报表 | 打开页面（只读） | 积分快照 3 行真实数据 + 站点报表标量；注入过期后显示「数据已过期」 |
| 邮件队列 | 失败任务「详情」→「重试」 | 详情显示脱敏说明；重试显示「已重新加入队列」 |
| 权限与会话 | 切换权限点 → 保存矩阵 → 还原 | 两次都显示「权限矩阵已保存」 |
| 权限与会话 | 输入用户 20 → 查询 → 撤销 → 确认 | 会话列表 4 行，撤销后提示「已撤销 N 个会话」 |
| 系统诊断 | 执行会员判定 → 打开自动刷新 | 判定显示「允许」；自动刷新开关可切换 |

全程无控制台报错。桌面 1440 与手机 375 截图核对四个页面。

补充修复（源码复核发现的两处缺陷，已修 + 定向验证）：

- **无效会话 ID 不再退化为撤销全部**：改为按路由是否带 `{sessionId}` 通配符区分单个 / 全部；单路由严格校验正整数，`abc` / `0` / 负数 / `1e3` / 溢出一律 422。HTTP 实测：五种非法输入全部 422，目标用户有效会话保持 2 条不变；不存在与跨用户会话返回 404 且不误伤；单个撤销 `{action:"revoke",affected:1}`，重复撤销 404；集合路由 `{action:"all",affected:1}`。
- **撤销全部改为单事务**：`sessions` 更新与 `mfa_challenges` 清理放在同一事务。隔离库用例用触发器强制 `mfa_challenges` 删除失败，断言接口报错且会话**未被撤销**（回滚）；移除触发器后全部撤销成功且只影响目标用户。
- 新增测试：`TestParseAdminSessionTarget`（路由区分与非法输入）、`TestAdminRevokeSessionsScopedAndAtomic`（作用域 + 原子回滚），均通过。
- **权限升级语义**：`perm.Load` 先铺编译期默认值再用数据库显式配置覆盖，所以新增的 `sessions.manage` 自动按默认值生效，不需要先手动保存一次权限矩阵；数据库显式 `false` 才保留关闭。已写入 `perm.go` 注释与 `DEVICE_SESSIONS.md`。

遗留：邮件队列没有详情 / 取消接口（详情来自列表行，且收件人与令牌字段由接口隐藏）；报表只有 `points` / `site` 两个快照，没有时间范围与图表筛选维度；诊断指标是实时读取、不带生成时间，“指标过期”只适用于报表快照。

## 验证记录（第五批 5C）

后端只新增一条只读索引接口（OpenAPI 204 → **205**）；其余复用现有契约。

**空库安装**（一次性空库 `gobbs_test_setup_ui`，不触碰 forum_dev 与原 forum）：

- 未安装：`GET /api/v1/setup` → `{"required":true}`；`/setup` 渲染表单（含“不收集数据库凭据”说明，页面 noindex）。
- **并发初始化**：两个独立匿名会话同时提交 → **200 / 409 ALREADY_INITIALIZED**，只有一次成功。
- **会话衔接**：成功响应的 Cookie 直接可读 `/session`（`groupId=1` 管理员、`setupRequired:false`），`/admin` 返回 200。
- **重复初始化**：已初始化后再次提交 → 409（用旧 CSRF 令牌时先被 CSRF 拦下，属正确行为）。
- **浏览器实测**：Chromium 打开空库 `/setup` → 填表 → 提交（200）→ 自动进入 `/admin`；安装后再次访问 `/setup` → 307 到 `/login`。

**公开 Markdown 与索引**（forum_dev，游客范围固定）：

- `/content/threads/35.md`：200，`content-type: text/markdown`、`cache-control: no-store`、`Link: <http://…/threads/35>; rel="canonical"`，正文与回复、页码、下一页链接齐全。
- **匿名与管理员完全一致**：带管理员 Cookie 与完全不带 Cookie 请求同一 `.md`，字节数相同（407 = 407，`cmp` 一致）。
- **待审 / 已删除不泄漏**：注入待审主题 47 与已删除主题 48 → 两者 `.md` 对游客与管理员**都返回 404**；`sitemap.xml` 与 `rss.xml` 中均无对应 id；索引接口精确比对 `.data.threads[].id` 为 `absent`，且全部 `pending=false`。**主题页 HTML 与索引接口都不含待审内容**。
- **错误状态**：不存在的 id 与非法 id 均 404，没有“200 空文档”。

**内容协商**（对 `/threads/35` 实测 8 组 Accept）：

| Accept | 结果 |
| --- | --- |
| `text/markdown` | Markdown |
| `text/markdown, text/html` | Markdown |
| `text/html, text/markdown` | HTML |
| `text/markdown;q=0` | HTML |
| `*/*` / `text/html` | HTML |
| `text/markdown;q=0.9, text/html;q=0.8` | Markdown |
| `text/html;q=0, text/markdown;q=1` | Markdown |

Markdown 响应带 `Vary: Accept`；HTML 表示输出可读 SSR（标题、正文、楼层），客户端导航不受影响。

**robots / sitemap / RSS**：`/robots.txt` 列出 `/admin/`、`/api/`、`/me/`、`/setup`、`/content/` 等禁止项与 Sitemap/Host；`/sitemap.xml` 31 条（静态入口 + 版块 + 标签 + 有界主题分片，主题带真实 `lastmod`）；`/rss.xml` 19 条，含稳定 `guid`、真实 `pubDate`、XML 转义，全部使用 `FORUM_SITE_URL`（配置项，非请求 Host）。

**Ardot 视觉复核**（MCP 本轮已可达，设计文件 730748088704786）：读取 95 个画板，导出并比对 AUTH-06、ADMIN-06、ADMIN-08、ADMIN-09、ADMIN-10、ADMIN-12 与 AUTH-06 移动端（设计参考图存于 `frontend/screenshots/design/`）。

- **已按设计优化**：ADMIN-09 增加“快照列表”表（快照名称 / 统计日期 / 刷新时间 / 状态 / 保留期 / 操作），与设计一致；保留期取站点设置 `analyticsRetentionDays`。
- **刻意偏离（设计稿不可实施）**：设计稿 AUTH-06 是 4 步向导，第 1 步采集数据库类型 / 主机 / 端口 / 库名 / 账号 / 密码 / 表前缀。真实契约 `POST /api/v1/setup` 只接受站点名与管理员资料，且本批明确要求“不在前端收集数据库凭据、不让 Next.js 直连数据库”，因此实现为**单步表单**，只保留站点与管理员信息；邮件步骤同样没有对应接口（SMTP 只来自环境变量）。
- **契约边界（不是视觉问题）**：设计稿 ADMIN-10 有“收件人摘要”列，但 `email_jobs` 的收件人、楼层与令牌字段由接口标记为不返回，无法展示；ADMIN-12 在设计稿中是合并页，实现拆成 `/admin/permissions` 与 `/admin/diagnostics`，并把“当前管理员会话 / 受信设备”实现为按用户查询（受 `sessions.manage` 约束）。
- 未逐页复核其余历史画板（会员中心、通知、私信等），也未把本地截图验证表述为“设计稿完全对齐”。

## 下一批建议顺序

1. **生产化收尾**：把 `FORUM_SITE_URL` / `API_INTERNAL_URL` 写进部署说明并核对 Nginx 同域分流；确认 robots/sitemap/RSS 在正式域名下的输出。
2. **契约补强（非阻塞）**：邮件任务详情 / 取消接口；投票后台按状态筛选的只读列表；报表时间范围与更多快照类型；sitemap 全量分片（`generateSitemaps`）以覆盖更早主题。
3. **视觉复核**：MCP 可达时继续逐页比对会员中心、通知、私信与内容操作画板；AUTH-06 若要贴近设计稿，需要先决定是否由后端接受数据库凭据（当前明确不做）。
4. 若需要管理员级账号其他能力（如强制改密、导出用户会话），先明确权限点与审计口径再扩展。
4. 若需要更多榜单，先明确排名口径（累计获得 / 周期新增 / 活跃度）与快照策略，再扩展现有 Worker。
