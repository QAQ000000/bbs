'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { IconHome, IconApps, IconPlus, IconEmail, IconUser, IconSend } from '@arco-design/web-react/icon';
import styles from './MobileTabBar.module.css';

const TABS = [
  { href: '/', label: '首页', icon: IconHome, match: (p: string) => p === '/' },
  { href: '/forums', label: '版块', icon: IconApps, match: (p: string) => p === '/forums' || p.startsWith('/forums/') || p.startsWith('/threads/') },
  { href: '/new', label: '发布', icon: IconPlus, match: (p: string) => p === '/new', primary: true },
  { href: '/me/messages', label: '消息', icon: IconEmail, match: (p: string) => p.startsWith('/me/messages') || p.startsWith('/me/notifications') },
  { href: '/me', label: '我的', icon: IconUser, match: (p: string) => p.startsWith('/me') && !p.startsWith('/me/messages') && !p.startsWith('/me/notifications') },
];

export function MobileTabBar() {
  const pathname = usePathname() || '/';
  if (/^\/threads\/[^/]+$/.test(pathname)) {
    return (
      <nav className={[styles.bar, styles.replyBar].join(' ')} aria-label="主题回复入口">
        <a href="#reply" className={styles.replyLink}>
          <span>回复主题…</span>
          <span className={styles.send}><IconSend aria-hidden="true" /></span>
        </a>
      </nav>
    );
  }
  return (
    <nav className={styles.bar} aria-label="底部导航">
      {TABS.map((tab) => {
        const Icon = tab.icon;
        const active = tab.match(pathname);
        return (
          <Link
            key={tab.href}
            href={tab.href}
            aria-label={tab.label}
            className={[styles.tab, tab.primary ? styles.primary : '', active ? styles.active : ''].join(' ')}
            aria-current={active ? 'page' : undefined}
          >
            <Icon />
            <span>{tab.label}</span>
          </Link>
        );
      })}
    </nav>
  );
}
