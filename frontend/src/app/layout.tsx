import type { Metadata } from 'next';
// 先加载 Arco 基础样式，再加载项目主题变量与覆盖，保证主题变量生效。
import '@arco-design/web-react/dist/css/arco.css';
import './globals.css';
import { SessionProvider } from '@/lib/auth/session';
import { getSession } from '@/lib/auth/server';
import { SITE_URL } from '@/lib/site-url';

export const metadata: Metadata = {
  // canonical / OG / sitemap / RSS 统一使用可信配置里的公开站点基址，不从请求 Host 推导。
  metadataBase: new URL(SITE_URL),
  title: { default: 'GoBBS 社区', template: '%s - GoBBS 社区' },
  description: 'GoBBS 开发者社区：技术交流、经验分享、问题求助与生活闲聊。',
};

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  const session = await getSession();
  // CSRF 交给浏览器自行初始化，避免 SSR 渲染出与浏览器会话不匹配的令牌。
  const initial = { user: session.user, csrfToken: '', setupRequired: session.setupRequired };
  return (
    <html lang="zh-CN">
      <body>
        <SessionProvider initial={initial}>{children}</SessionProvider>
      </body>
    </html>
  );
}
