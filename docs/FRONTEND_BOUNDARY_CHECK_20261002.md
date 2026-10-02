# 前端边界验收记录（2026-10-02）

依据：[前端视觉与交互规范](FRONTEND_GUIDELINES.md)。

## 环境

- 前端：Next.js `14.2.35`，先在开发服务检查，再停止开发服务、构建并用 `pnpm start` 在 `http://127.0.0.1:3000` 复验。
- 浏览器：Playwright CLI + Chromium，无沙箱运行。
- 后端：`127.0.0.1:8090` 未启动。本轮只检查故障降级、匿名页面结构和无需后端的交互；依赖真实数据的项目记为“未验证”，不是 `N/A` 或通过。

## 结果

| 检查项 | 结果 | 证据 / 说明 |
| --- | :---: | --- |
| 6 个路由 × 1440/768/375px 无横向溢出 | PASS | `/`、`/forums`、`/forums/3`、`/threads/1`、`/login`、`/does-not-exist`；18 次均 `scrollWidth === innerWidth`，且只有一个 `main`。内容页检查的是故障态，不能推及真实长内容 |
| 后端不可用时首页显示可重试失败态 | PASS | 显示“信息流加载失败”和“重新加载” |
| 主要内容故障保留 HTTP 语义和错误界面 | PASS | `/forums`、`/forums/3`、`/threads/1` 保持 HTTP 500；局部错误边界显示“页面加载失败”、重试按钮和公共导航 |
| 未知路由 404 | PASS | `/does-not-exist` 返回 HTTP 404，显示“页面不存在”；后端资源 404 尚未验证 |
| 键盘焦点可见 | PASS | 登录页 Tab 后焦点落在链接，`outline-style: solid` |
| `prefers-reduced-motion: reduce` | PASS | 登录页按钮 computed `transitionDuration` / `animationDuration` 约 `1e-06s` |
| 1440px 登录视觉结构 | PASS | 左侧品牌区、右侧表单、按钮和字段完整 |
| 375px 登录视觉结构 | PASS | 品牌标识、表单、密码可见性按钮、注册/找回入口完整 |
| 登录按钮 Hover 的可见性和尺寸稳定 | PASS | 默认/Hover 均白字，尺寸均为 `327 × 46px`；不代表对比度通过 |
| 登录空表单提交 | PASS | 显示 `role="alert"` 的“请输入用户名和密码”，没有提交登录请求 |
| 首页空数据与请求失败区分 | FAIL | `tagPage?.data ?? []`、`home?.announcements ?? []` 将失败转换为空数组，标签/公告显示“暂无” |
| 错误说明的文字对比度 | FAIL | `StateView.module.css` 的 13px 说明文字使用 `#86909c`，对白色约 3.24:1、对 `#f5f6f8` 约 3.00:1，未达到普通正文 WCAG AA 的 4.5:1 |
| 登录按钮 Hover 对比度 | FAIL | 白字与实际背景 `#4080FF` 对比度约 3.65:1；默认 `#165DFF` 时约 5.19:1。该 Hover 背景也不同于项目 `--color-primary-hover` |
| 无浏览器资源错误 | FAIL | 后端停机时 `/api/v1/session` 返回 500；主要内容错误边界出现的 React 错误日志也如实保留，不能报告控制台零错误 |
| 深色主题 | N/A | 当前只实现浅色主题；系统深色偏好不等于应用提供深色主题 |

## 本轮修复

- 新增公共路由组 `error.tsx`，主要内容读取失败保留既有抛错和 5xx 语义，同时保留公共外壳与重试入口。未把后端故障转换成成功 HTTP 200。
- 主题详情的可选版块和用户资料改为容错读取，辅助数据故障不阻断正文；该项已静态核对和构建，尚未用真实主题和单接口故障复验。
- 全局 404 与错误页补充语义 `main`。

## 门禁与产物

- `pnpm lint`、`pnpm typecheck`、最终生产构建内的 lint/typecheck 及 `pnpm build` 通过；`git diff --check` 通过。
- 截图：`output/playwright/boundary-home-375.png`、`boundary-login-1440.png`、`boundary-login-375.png`、`boundary-public-error-375.png`；已逐张查看。
- 开发服务和生产构建不能同时写入 `.next`。本轮曾因此产生模块缺失，停止开发服务后重建并使用正式构建重新验收，最终结果以上面的生产复验为准。

## 仍需补验

启动隔离后端和测试数据库后，补验真实主题/版块内容、长标题/长用户名/代码/表格、匿名与登录切换、403/资源404/409、发帖/回复、Active/Disabled/Loading、弹层焦点返回及会员中心/后台操作。本记录不是全站前端验收通过证明。剩余 FAIL 项本轮未修复。
