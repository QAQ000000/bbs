import { getSession } from '@/lib/auth/server';
import { getSite } from '@/lib/site.server';
import { getNotificationUnread } from '@/lib/notifications.server';
import { SiteHeader } from './SiteHeader';
import { SiteFooter } from './SiteFooter';
import { MobileTabBar } from './MobileTabBar';
import styles from './PublicShell.module.css';

/** 前台公共外壳：顶部导航 + 内容区 + 页脚 + 移动端底部导航。 */
export async function PublicShell({ children }: { children: React.ReactNode }) {
  const [session, site] = await Promise.all([getSession(), getSite()]);
  const notificationUnread = session.user ? await getNotificationUnread() : 0;
  return (
    <div className={styles.shell}>
      <SiteHeader siteName={site?.name ?? 'GoBBS 社区'} notificationUnread={notificationUnread} />
      <main className={styles.main}>{children}</main>
      <SiteFooter footerText={site?.footerText} />
      <MobileTabBar />
    </div>
  );
}
