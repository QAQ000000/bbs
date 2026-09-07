# TOTP 二次验证

2026-09-07，schema 14。后端 API 已完成专项修复，前端绑定、扫码、登录挑战和恢复码保存页面仍待开发。schema 12 的旧实现不可作为安全验收依据。

## 配置与迁移

使用 `openssl rand -hex 32` 单独生成 `FORUM_MFA_KEY`，通过环境变量或权限为 600 的环境文件配置。密钥为 64 位十六进制文本，用 AES-256-GCM 加密 TOTP secret；所有实例及每次重启必须使用同一密钥，密钥与数据库备份分开保管。不要复用邮件加密密钥。

未配置密钥时普通账户仍可登录，但 2FA 管理和已启用账户的登录返回 503。错误密钥无法解密时同样拒绝验证，不会跳过 2FA，也不接受恢复码绕过密钥配置故障。密钥格式错误导致启动失败。没有自动密钥轮换接口；丢失密钥须恢复原密钥，重新生成环境密钥不能恢复旧账户。

升级至 schema 14 会作废旧版登录挑战，清除旧格式恢复码，保留已启用状态、加密 secret 和最后使用时间步。旧版尚未完成的绑定需重新 setup；旧版已启用用户可继续使用验证器，并通过恢复码更新接口生成新码。新挑战、剩余恢复码及尝试次数在后续迁移重放和重启中保留。

## 接口

全部写接口接受 JSON 或 URL 编码表单，携带 Cookie 与 `X-CSRF-Token`。所有响应使用 `Cache-Control: no-store`。前端不要把 secret、验证码、挑战或恢复码放入 URL、日志、埋点和持久化浏览器存储。

| 方法与路径 | 输入 | `data` 返回 |
| --- | --- | --- |
| `GET /api/v1/me/2fa` | 登录态 | `enabled`、`recoveryCodesRemaining`，不返回 secret 或恢复码 |
| `POST /api/v1/me/2fa/setup` | `password` 当前密码 | `secret`、`setupId`、`issuer`、`account`、`otpauthUrl`、`expiresIn:600` |
| `POST /api/v1/me/2fa/enable` | `password`、`setupId`、`code` | `enabled:true`、`recoveryCodes` |
| `POST /api/v1/me/2fa/disable` | `password`，以及 `code` / `recovery` 二选一 | `enabled:false` |
| `POST /api/v1/me/2fa/recovery-codes` | `password`，以及 `code` / `recovery` 二选一 | `enabled:true`、新的 `recoveryCodes` |
| `POST /api/v1/auth/2fa` | `challenge`，以及 `code` / `recovery` 二选一 | 本人资料，并设置登录 Cookie |

setup 必须重新验证密码，且不能覆盖已启用的 secret。绑定有效期为 10 分钟，与发起绑定的会话、当时密码及 setupId 绑定；重新 setup 会使之前的绑定失效。启用时验证 TOTP 后才生成 10 个恢复码，每个 128 位随机熵、26 个 Base32 字符，仅存 SHA-256 哈希。恢复码大小写不敏感，每个只能使用一次，不带分隔符。

启用和更新恢复码的响应只展示一次明文。前端应在用户确认保存前保留当前页面；若响应丢失，用户可在下一 TOTP 时间步重新验证并更新整组恢复码。更新操作立即替换旧码，不提供重新读取旧码的接口。

启用、关闭和更新恢复码会撤销其他设备会话、作废所有未完成登录挑战，当前执行操作的会话保留。关闭必须同时验证当前密码和第二因素；仅持有登录 Cookie 不足以关闭保护。密码修改或邮件重置不会关闭 2FA；挑战中的密码快照失效后不能继续登录。封禁后再解封也不会恢复旧挑战；退出其他/全部设备同时作废待完成挑战。单设备退出只撤销该会话。

## 前端登录流程

1. `GET /api/v1/session` 获取匿名 CSRF Cookie 和 `data.csrfToken`。
2. `POST /api/v1/auth/login` 提交用户名、密码。未启用 2FA 时沿用普通登录；启用时返回下方 HTTP 401 数据，此时未签发登录 Cookie。
3. 保留同一浏览器 Cookie 和 CSRF，将 challenge 与验证码或恢复码提交 `/api/v1/auth/2fa`。
4. 成功后再次获取 `/api/v1/session`，切换为登录态 CSRF token。

```json
{"data":{"code":"MFA_REQUIRED","challenge":"opaque-token","expiresIn":300}}
```

```json
{"challenge":"opaque-token","code":"123456"}
```

```json
{"challenge":"opaque-token","recovery":"26-character-base32-code"}
```

`MFA_REQUIRED` 是现有登录协议的特殊 401 `data` 响应，前端拦截器需先识别它，不能一律当成普通错误或自动再次提交密码。挑战为 32 随机字节编码的 43 字符不透明 token，数据库仅存哈希；与发起浏览器的 CSRF、当前密码及 MFA 配置版本绑定，有效期 5 分钟。

TOTP 使用 pquerna/otp 的 SHA1、6 位、30 秒配置，允许前后各一个时间步。成功使用的时间步必须大于最后成功时间步，绑定启用也会消费时间步；因此启用后立即登录通常需要等待下一验证码或使用恢复码。同一验证码不能在多设备/多请求间重复使用。

验证码或恢复码消费、挑战作废、设备会话插入以及成功审计均在同一数据库事务中完成。错误验证码不会立即消耗整个挑战，允许在次数限制内重试。请求同时携带 code 和 recovery，或两者均为空，会视为验证失败。

## 限流与错误

| 范围 | 限制 |
| --- | --- |
| 每个挑战 | 最多 5 次错误第二因素，耗尽后须重新提交密码 |
| 每账户挑战创建 | 每 5 分钟最多 5 次 |
| 每账户第二因素验证 | 每 10 分钟最多 10 次，跨挑战累计 |
| 每账户 2FA 管理操作 | 每 10 分钟最多 10 次，包含密码和第二因素验证 |
| 每 IP 验证 / 管理 | 各每 10 分钟最多 30 次，另保留普通登录限流 |

账户计数在 PostgreSQL 中持久化，包含成功和失败的验证尝试；切换服务器实例、重启或申请新挑战不能清零。数据库错误回滚本次操作；随机无效挑战、错误浏览器绑定和已作废挑战不会扣减他人账户次数。IP 限流是进程内辅助限制。窗口从首次尝试开始计算。

常规错误为 `{"error":{"code":"...","message":"..."}}`：

- 401 `MFA_INVALID`：密码或第二因素错误、绑定/挑战过期、次数耗尽、配置变化、会话撤销。
- 403 `FORBIDDEN`：CSRF 不匹配；未登录管理接口为 401 `UNAUTHENTICATED`。
- 429 `MFA_RATE_LIMITED`：账户或 IP 限流。
- 503 `MFA_UNAVAILABLE`：密钥缺失或解密失败。
- 503 `MFA_FAILED`：数据库等内部故障，不返回内部错误和凭据。

审计动作包括 `mfa.setup`、`mfa.enable`、`mfa.disable`、`mfa.recovery.rotate`、`mfa.recovery.used`、`mfa.login`、`mfa.failure`。失败审计针对通过账户和挑战基础检查后的密码/验证码/频率失败，随机无效挑战不写账户审计。审计不包含密码、secret、验证码、恢复码和挑战 token。每小时有带超时的清理任务，单次各清理最多 1000 条过期挑战、过期未完成绑定和过期计数；请求本身始终检查有效期。

## 验证范围

专项测试覆盖 RFC 4226/6238 向量、时间步重放、恢复码、加密失败、绑定密码/会话/过期限制、并发验证、事务故障回滚、数据库持久限流、挑战失效、JSON/表单 API、缺失/错误密钥、MFA 表读取故障，以及 schema 13 升级和重复迁移。真实进程 HTTP 检查使用独立测试数据库，不替换本地常驻服务。

当前不包含 WebAuthn、可信设备、管理员代关 2FA、强制全站启用策略或前端页面。现有邮箱重置密码不构成移除第二因素的恢复渠道。
