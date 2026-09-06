# HTTP API 简要说明

页面路由（HTML）不在本文档范围，这里只列面向前端 JS 的 JSON/流式接口。
所有 POST 均要求会话 Cookie 与 CSRF（`_csrf` 表单字段或 JSON `csrf` 字段）。

## 实时事件流

```
GET /api/live?thread={tid}          订阅单个帖子
GET /api/live?forums={fid,fid,...}  订阅多个版块（版块页/首页）
GET /api/live?user={uid}            订阅个人通知（页面自动带上）
```

`text/event-stream`，每条消息为一行 `data: {JSON}`，25s 心跳注释。

事件 `type` 与载荷：

| type | 载荷 | 客户端行为 |
| --- | --- | --- |
| `post.new` | `pid, floor, postCount, postHtml` | 楼层在当前页则原位追加，否则显示"有新回复"胶囊 |
| `post.edit` | `pid, floor, postHtml` | 原位替换楼层 |
| `post.delete` | `pid` | 移除楼层 |
| `post.like` | `pid, likeCount` | 更新点赞计数 |
| `thread.new` / `thread.update` | `tid, threadRow, forumRow` | 版块页/首页替换对应行 |
| `thread.delete` / `thread.deleted` | `tid` | 移除行 / 提示跳转 |
| `notify` | `fromName, notifyCount` | 铃铛更新 + 浮动提醒 |

## Markdown 预览

```
POST /api/preview      {"content": "...markdown..."}   需登录；30/min
→ {"html": "..."}      服务端统一渲染（与发帖结果一致），HTML 已消毒
```

## 点赞

```
POST /api/like/{pid}          切换点赞（不能赞自己）
→ {"liked": true, "count": 3}
GET  /api/likes/{pid}
→ [{"uid":1,"name":"..."}]    点赞名单（浮层）
```

## 上传（受"仅外链模式"开关控制）

```
POST /api/upload    multipart: file, kind=image|file, _csrf
→ {"url": "/uploads/2026/09/xx.png", "name": "原图.png", "kind": "image", "mime": "image/png"}
```

限制：图片/附件大小上限由后台设置决定（默认 8MB / 20MB）；类型白名单
image: JPG/PNG/GIF/WebP，file: PDF/TXT/ZIP（按内容嗅探校验）。

## 草稿

```
GET  /api/draft?context={new:fid|reply:tid|edit:pid}
→ {"content": "...", "updatedAt": 1699999999}
POST /api/draft   {"context": "...", "content": "...", "csrf": "..."}
```

## 状态

```
GET /api/status
→ {"ok": true, "ts": "...", "db": "up", "schema": 5,
   "pending": {"threads": 0, "posts": 0, "reports": 0}}
db=up/down 为数据库可达性，schema 为 schema_migrations 迁移版本，
pending 为治理队列积压（待审主题/回复/待处理举报）。不含内部连接数等细节。
```

## 头像

```
GET /avatar/{uid}    确定性字母头像 SVG（24h 缓存）
```
