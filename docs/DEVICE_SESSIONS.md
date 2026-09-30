# 设备会话管理

2026-09-07，设备会话 API 在 schema 11 实现，TOTP 二次验证在 schema 14 完成专项修复，见 [二次验证](MFA.md)。前端设备管理页面仍待开发。

## 设备记录

每次登录、注册或安装自动登录生成一条独立会话。设备 ID 为独立自增编号，不是 Cookie token 或其哈希。记录设备名称、登录时的 User-Agent、脱敏 IP、创建时间、最近活动时间、过期时间和撤销时间。

User-Agent 清除控制字符、最多保留 512 字符，仅供展示，不作为身份凭证。默认设备名称为空，由前端展示 User-Agent 或“未命名设备”；可由用户重命名为 1 至 80 字符。IPv4 保存 /24 网段，IPv6 保存 /48 网段；前端不能直接指定设备地址。IP 来源沿用现有请求地址解析规则，未新增代理信任策略。

会话有效期仍为 30 天，不因活跃而延长。普通鉴权请求至多每分钟更新一次活动时间和脱敏 IP。撤销、过期后记录保留 30 天，定时清理；旧会话迁移保留登录有效性，缺少的 User-Agent/IP/名称为空，活动时间取创建时间。

## API

全部接口只允许本人访问，写入必须提供登录 Cookie 和 `X-CSRF-Token`。

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | `/api/v1/me/sessions?before=123` | 按设备 ID 降序分页，每页最多 50 条，包含有效、已撤销及已过期记录 |
| PATCH | `/api/v1/me/sessions/{sessionId}` | `{"name":"办公电脑"}`，重命名仍有效的设备 |
| DELETE | `/api/v1/me/sessions/{sessionId}` | 撤销一个有效会话，允许撤销当前设备 |
| POST | `/api/v1/me/sessions/revoke-others` | 撤销除当前设备外的全部有效会话 |
| DELETE | `/api/v1/me/sessions` | 撤销包含当前设备在内的全部有效会话 |

列表返回 `items`、`currentSessionId`、`nextBefore`，ID 和游标均为字符串。每条包含 `id`、`name`、`userAgent`、`maskedIp`、`createdAt`、`lastSeenAt`、`expiresAt`、`revokedAt`、`current` 和 `status`（`active` / `revoked` / `expired`）。不返回 token、token 哈希或 CSRF 密钥。

写入返回 `affected` 和 `signedOut`。撤销当前会话时 `signedOut=true`，同时清除 Cookie；前端随后进入退出状态。其他用户的设备、已失效的操作方会话、已撤销或过期的单设备目标返回 404。无效名称或游标返回 422，缺少 CSRF 返回 403，写操作每分钟限 30 次。批量操作无有效目标时成功返回 `affected=0`。

### 管理员查看与撤销（5B 新增）

管理员可以按用户查看和撤销设备会话；与上面的本人接口相互独立，需要新的权限点 `sessions.manage`（默认仅管理员拥有，会出现在权限矩阵里）。

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | `/api/v1/admin/users/{uid}/sessions?before=123` | 按设备 ID 降序，每页最多 50 条；返回 `items`、`nextBefore`、`userId` |
| DELETE | `/api/v1/admin/users/{uid}/sessions/{sessionId}` | 撤销该用户的一个有效会话，返回 `{affected, action:"revoke"}` |
| DELETE | `/api/v1/admin/users/{uid}/sessions` | 撤销该用户的全部有效会话，返回 `{affected, action:"all"}` |

管理员接口不需要传“当前会话”，因此可以撤销他人的会话；写操作受 `sessions.manage` 与 CSRF 保护，并写入 `admin_logs`（`user.sessions.revoke` / `user.sessions.all`）。撤销另一个用户的会话不会封禁账号、不改密码、不改权限。

路由语义必须严格区分：**只有不带 `{sessionId}` 的集合路由才是“全部撤销”**。单个路由的 `sessionId` 必须是正整数，`abc`、`0`、负数、溢出等输入一律返回 422，绝不会退化成“撤销全部”。单个目标不存在、已撤销、已过期或不属于该用户时返回 404，与上面的本人接口一致。

“撤销全部”在单个事务里完成：更新 `sessions` 与清理该用户的 `mfa_challenges` 要么一起成功，要么一起回滚，不会出现“接口报错但会话已被撤销”的部分成功。事务串行化沿用与本人接口相同的行锁路径。

**权限点升级说明**：`perm.Load` 先铺编译期默认矩阵，再用数据库里显式存在的角色 / 权限点覆盖。因此升级后新增的 `sessions.manage` 会自动按默认值生效（管理员可用），**不需要先在后台保存一次权限矩阵**；只有数据库曾明确写入 `false` 的项会继续保持关闭。

## 撤销语义

鉴权每次从 PostgreSQL 检查有效期和撤销状态，取消原来的 30 秒进程内会话缓存。撤销提交后的新请求在所有实例立即失效；已在执行的普通请求不会被强制终止。设备管理写入在事务内再次检查操作方会话，避免已撤销请求继续管理其他设备。

已有 SSE 连接在下一次事件或 25 秒心跳时重新鉴权，撤销后发送 `subscription.reset` 并断开；不向失效会话交付新的业务事件。

单设备退出、全部退出、重命名写入 `session.revoke`、`session.others`、`session.all`、`session.rename` 审计记录。改密事务同时撤销其他会话及旧重置令牌；密码重置撤销全部会话。封禁与会话撤销共同提交，解封不会复活旧会话。登录创建会话时复查已验证的密码哈希，防止并发改密后旧密码继续产生新会话。

此功能管理的是登录会话，不进行物理设备指纹识别，也没有“可信设备”或跳过 2FA 机制。启用、关闭或更新 2FA 恢复码会撤销其他设备会话；退出其他/全部设备会同时作废待完成的 2FA 登录挑战。
