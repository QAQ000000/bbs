# 站点配置 API

当前 19 项业务设置共用类型、范围和依赖校验（原 18 项加分析快照保留期）。配置版本保存在现有 `settings` KV 表的内部键 `site_settings_version`，不新增数据库表，当前 schema 17，保留期复用既有 KV 不增加迁移。没有版本键的旧库按版本 1 读取，首次初始化或保存时事务内建立版本键；普通配置接口不允许直接修改此键。

## 接口

以下路径统一以 `/api/v1` 开头，均要求管理员入口权限及 `settings.edit`。写接口还要求有效 CSRF；初始密码未修改的管理员不能操作。

| 方法 | 路径 | 含义 |
| --- | --- | --- |
| GET | `/admin/settings` | 返回 19 项 camelCase 设置及当前 `version` |
| PUT | `/admin/settings` | JSON 完整替换，必须提供全部业务字段及 `version` |
| PATCH | `/admin/settings` | JSON 局部修改，必须提供 `version` 和至少一个业务字段；未传字段保持原值 |
| POST | `/admin/settings` | 保留的 snake_case 完整保存入口，支持 JSON 或表单；同样必须提供全部业务字段及 `version` |
| GET | `/admin/settings/schema` | 字段名称、旧字段名称、类型、范围、默认值、依赖、所需权限及是否需要重启 |
| GET | `/admin/settings/status` | 当前版本、配置是否合法、问题字段、SMTP 可用状态及实际生效状态 |

成功响应仍使用统一 `{ "data": ... }` 包装。三个保存入口均返回保存后的完整 camelCase 配置和新版本。POST 的旧路径及字段名保留，但不再接受缺少版本或缺字段的请求；旧客户端必须先完成适配，不能将“路径保留”理解为请求契约完全不变。

局部修改示例，版本号必须取自刚读取的配置：

```json
{
  "version": 3,
  "siteName": "我的社区",
  "threadsPerPage": 30
}
```

完整替换现包含新增 `analyticsRetentionDays`；旧客户端不能继续只提交原 18 项。建议先 GET 后修改，或使用带版本号的 PATCH。

完整替换示例：

```json
{
  "version": 3,
  "siteName": "我的社区",
  "threadsPerPage": 20,
  "postsPerPage": 10,
  "registerEnabled": true,
  "siteClosed": false,
  "siteClosedReason": "站点维护中，请稍后再访。",
  "moderateEnabled": false,
  "uploadEnabled": true,
  "maxImageMB": 8,
  "maxFileMB": 20,
  "uploadMaxDiskGB": 10,
  "captchaEnabled": false,
  "emailVerifyEnabled": false,
  "requireConsent": true,
  "termsContent": "本站服务条款正文",
  "privacyContent": "本站隐私政策正文",
  "siteLogo": "",
  "footerText": "",
  "analyticsRetentionDays": 30
}
```

PUT/PATCH 只接受真实 JSON 布尔、整数或字符串，拒绝 null、数组、错误类型、未知字段、重复 JSON 字段和多个 JSON 对象。请求体上限 1 MiB。POST 表单布尔使用 1/0，POST JSON 布尔可以使用 true/false，经兼容适配后同样校验。

## 字段约束

字符串长度以 Unicode 字符数计算。站点名和 Logo 不能含控制字符；所有字符串不能含 NUL。条款和隐私正文保留原格式，其他文本去除首尾空白后校验。

| JSON 字段 | POST 旧字段 | 范围 / 默认值 |
| --- | --- | --- |
| `siteName` | `site_name` | 1-100 字；默认 GoBBS 社区 |
| `threadsPerPage` | `threads_per_page` | 整数 5-100；默认 20 |
| `postsPerPage` | `posts_per_page` | 整数 5-100；默认 10 |
| `registerEnabled` | `register_enabled` | 布尔；默认 true |
| `siteClosed` | `site_closed` | 布尔；默认 false |
| `siteClosedReason` | `site_closed_reason` | 1-1000 字；有默认维护提示 |
| `moderateEnabled` | `moderate_enabled` | 布尔；默认 false |
| `uploadEnabled` | `upload_enabled` | 布尔；默认 true |
| `maxImageMB` | `max_image_mb` | 整数 1-1024；默认 8 MB |
| `maxFileMB` | `max_file_mb` | 整数 1-1024；默认 20 MB |
| `uploadMaxDiskGB` | `upload_max_disk_gb` | 整数 1-1024；默认 10 GB |
| `captchaEnabled` | `captcha_enabled` | 布尔；默认 false |
| `emailVerifyEnabled` | `email_verify_enabled` | 布尔；默认 false，显式开启前必须配置 SMTP |
| `requireConsent` | `require_consent` | 布尔；默认 true；为 true 时条款及隐私正文均不能为空白 |
| `termsContent` | `terms_content` | 0-30000 字；默认正文从 schema 接口获取 |
| `privacyContent` | `privacy_content` | 0-30000 字；默认正文从 schema 接口获取 |
| `siteLogo` | `site_logo` | 0-200 字；默认空，公共展示仍保留环境 Logo 回退 |
| `footerText` | `footer_text` | 0-2000 字；默认空 |
| `analyticsRetentionDays` | `analytics_retention_days` | 整数 0-3650 天；默认 30，0 关闭快照清理；始终保留每类最新快照 |

`schema.defaults.version` 表示初始版本，不是当前数据库版本。使用默认配置进行重置时仍须提交当前 GET 返回的版本。整数 min/max 表示取值范围，字符串 min/max 表示字符数，布尔字段忽略 min/max。

## 版本、事务与审计

- 保存时锁定版本记录，再检查请求版本。两个管理员使用相同版本并发保存，只有一个成功，另一个返回 409。禁止前端在冲突后直接替换版本并自动重试，应先重新读取并合并修改。
- 业务值、版本递增和 `admin_logs` 中的 `settings.save` 审计同事务提交；任一步失败全部回滚。成功保存即便没有值变化也递增版本，差异可为空。
- `settings.save` 的 `detail` 是 JSON 字符串，包含 `previousVersion`、`version`、`changes`。每个变更字段包含 `before` 和 `after`，可通过现有 `/admin/logs` 读取；修复非法历史值时 `before` 会记录 `invalidStoredValue`。
- 没有独立的配置版本历史列表或一键回退接口。需要恢复时，读取当前版本后提交明确的目标值，并产生新的审计记录。
- 启动缺省值初始化和安装向导同样锁定版本；新增实际配置时递增版本，重复启动不覆盖已有值。
- 旧二进制不遵守新版本协议，不能与新版本混合写入同一数据库。

## 生效与错误恢复

站点配置不再使用跨请求的 30 秒缓存。普通请求读取数据库快照并在请求内复用；保存后新的请求看到新配置，已经开始的请求仍可使用原快照。SSE 重新验证时重新读取配置；后台邮件在准备发送时读取最新站点名，因此不会只依赖启动时的品牌文案。修改这 19 项设置不要求重启进程。快照清理每批读取保留期，正常情况下在下一次每分钟调度生效；已开始的批次可能继续使用旧值，故障重试会延后生效，详见 [快照保留期](ANALYTICS_SNAPSHOTS.md)。

配置查询失败或历史值非法，不再回退为允许注册、免审核等默认状态。正常站点请求会拒绝继续处理；`GET /site` 返回 503。存活探针保持可用。配置管理接口保留授权后的诊断与修复入口：

1. 已登录且具有权限的管理员调用 `/admin/settings/status`，读取 `version`、`valid`、`issues`。
2. 若 `valid=false`，参考 `/admin/settings/schema` 的类型及范围修复问题字段。存在多个非法字段时，可以 PUT 完整配置一次修复；依赖关系在合并后检查。
3. 保存成功后重新读取配置及状态，确认公共 API 恢复。

数据库本身不可达或版本键损坏时，状态接口也会返回 503，需要部署维护处理。此恢复入口要求已有可用管理员会话；不绕过登录及权限检查。

`status.effective` 包含 `emailVerificationRequired`、`emailVerificationInactiveReason`、`mailSiteName`、`requiresRestart`。例如部署后来移除 SMTP，保存的开关可能仍为 true，但实际邮箱验证为 false，原因是 `SMTP_DISABLED`。开启动作会拒绝这种依赖缺失；编辑其他无关字段不隐式改变已有开关。配置非法时 `effective` 为 null。

| HTTP | 错误码 | 含义 |
| --- | --- | --- |
| 428 | `SETTINGS_VERSION_REQUIRED` | 未提供配置版本 |
| 409 | `SETTINGS_CONFLICT` | 请求版本已过期 |
| 422 | `VALIDATION_FAILED` | 缺字段、未知字段、错误类型、越界或依赖不满足 |
| 403 | `FORBIDDEN` / `CSRF_INVALID` | 权限或 CSRF 校验失败 |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | PUT/PATCH 未使用 JSON |
| 503 | `SETTINGS_UNAVAILABLE` | 配置读取、保存或审计写入失败 |

SMTP 地址、凭据、站点外部 URL、邮件密钥和 MFA 密钥仍属于部署配置，不从本接口返回。本批次不新增私信、订阅、设备或 2FA 的运营策略，角色权限矩阵的版本协议也未改造。

## 验证

新增 `internal/store/settings_test.go` 与 `internal/api/settings_test.go`，覆盖缺字段保护、局部修改、完整及旧入口保存、类型和范围、版本竞争、写入及审计失败回滚、数据库异常、非法历史配置修复、启动重放、权限、CSRF、元数据、SMTP 依赖及真实本机收件。

2026-09-08 03:36（Asia/Shanghai）完成当前工作区全量验收：存储层 72 项（183.773 秒）、API 66 项（62.767 秒）、迁移 9 项（4.274 秒）、其余模块 12 项，合计 159 项顶层测试全部通过。使用独立 PostgreSQL 测试库，所有包运行 `-race -count=1`，无失败、跳过或竞态告警；`go vet ./...`、gofmt 及差异检查通过。回归前后 Go/SQL 源码及依赖文件校验和一致，测试数据库和受限角色均已清理。

本机数据库回归日志为 `/tmp/community-store.log`、`/tmp/community-api.log`、`/tmp/community-migrate.log`。SMTP 仅使用本机夹具，启动重放通过重复调用迁移验证；未验收外部 SMTP、真实进程重启或前端页面，未提交或部署。
