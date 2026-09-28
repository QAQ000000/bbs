# 内容浏览、互动与审核接入

适用于独立前端对接当前 Go API。本文描述已有后端流程，不代表页面已经实现；字段以 [OpenAPI](openapi.json) 的已审核定义为准。

## 接入顺序

| 页面 / 动作 | 接口 | 前端处理 |
| --- | --- | --- |
| 启动、导航与首页 | GET /api/v1/session、/site、/home、/forums | 会话提供 CSRF；站点接口提供公开设置；首页、版块目录按当前用户可见范围返回 |
| 版块与主题 | GET /api/v1/forums/{fid}、/threads、/threads/{tid}、/threads/{tid}/posts | 列表与楼层分页分别处理；以 capabilities 控制按钮，提交时后端仍重新检查权限 |
| 发帖、回复和编辑 | POST /api/v1/threads、/threads/{tid}/posts；PATCH /api/v1/posts/{pid} | 保留编辑 version；pending=true 展示待审状态，不将其当作公开发布成功 |
| 点赞与收藏 | POST /api/v1/posts/{pid}/like、/threads/{tid}/favorite | 都是切换操作，禁止自动重试；用返回状态更新界面，超时先重新读取状态 |
| 点赞名单、收藏列表 | GET /api/v1/posts/{pid}/likes、/me/favorites | 名单仅提供公开用户 ID 和名称；收藏列表有分页与 hasNew，不等于订阅列表 |
| 定位与编辑历史 | GET /api/v1/posts/{pid}/position、/history | 定位返回当前用户可见口径的 page，不能用 floor 直接计算；历史内容仍须安全渲染 |
| 删除内容 | DELETE /api/v1/posts/{pid} | 删除首楼会删除主题；删除回复成功时 data 可以是空对象，不依赖 message 存在 |
| 举报 | POST /api/v1/posts/{pid}/reports | 提交 reason；不可举报自己；提交成功不代表已经处理 |
| 后台待处理队列 | GET /api/v1/admin/moderate | 分别展示 threads、posts、reports；各最多 50 条，不含分页总数 |
| 审核主题 / 回复 | POST /api/v1/admin/moderate/thread、/moderate/post | 发送 tid 或 pid、op=approve/delete、可选 note；首楼必须走主题接口 |
| 处理举报 | POST /api/v1/admin/report/handle | 发送 id、op=delete/dismiss；完成后重新读取队列和受影响内容 |

## 请求和状态约定

- 主题、楼层、版块、用户等资源 ID 使用十进制字符串；不要将其统一转换为 JavaScript Number。关联 ID 为 "0" 的情况见具体字段。
- 当前内容动作支持 application/json，兼容表单；写请求附 X-CSRF-Token。无业务字段时发送 {}，不要发送 JSON null 或空 JSON 请求体。
- 成功一般为 {data: ...}；分页另外含 meta。不要把 HTTP 200、message 文案或空数组解释成业务权限。
- 401 重新登录；403 根据 error.code 区分 CSRF、账号状态和权限；404 也可能表示当前用户不可见；409 编辑冲突保留本地内容并重新获取版本；429 等待后再操作。
- capabilities 是渲染时的操作提示，不保证稍后提交仍成功。审核、删除、封禁和会员配置都可能使其变化。
- 点赞与收藏是 toggle，不可用请求重放实现恢复。点赞超时后读取楼层 viewerHasLiked；收藏超时后读取主题 favorite。
- 内容创建、编辑、审核和通知存在异步衍生处理；提交响应成功后，列表计数、搜索和通知可能稍后更新。不要通过反复提交来催促更新。
- SSE 用于提示刷新，不提供断线后的持久化重放；重连或收到 subscription.reset 后重新验证内容访问。

## 后台的权限与字段边界

审核队列和审核动作要求 content.moderate 与 moderate.queue，版主还受管辖版块限制；举报处理要求 content.moderate 并检查管辖范围。这些入口均要求完成初始密码修改。不要仅按 groupId 显示全部管理能力，也不要把 /admin 路径一概当作仅超级管理员可用。

审核队列沿用基础主题 / 楼层投影，不保证带有前台详情的 capabilities、附件或互动摘要。举报行当前关联主题字段为 tId、threadTtl；回复审核行则使用 threadTitle。前端应按相应 DTO 读取，不擅自统一字段名。

审核 note 最多 500 字。举报 reason 允许空理由；去除首尾空白后，超长内容保留前 200 字并追加省略号（因此存储结果可为 201 字）。举报结案后再次处理返回 404；不要以自动重试掩盖已结案状态。

## 交付与运维边界

论坛提供业务 API、权限控制、状态诊断及必要部署说明。备份周期、备份存储、定时任务和恢复执行由部署者通过宝塔、数据库服务或自己的脚本管理；本阶段不要求程序实现备份调度或后台恢复中心。

后续优先按真实页面补充字段契约和联调发现的问题。定向验证覆盖接口返回与关键权限，不用完整前端工程或长时间压测替代本批接入工作。
