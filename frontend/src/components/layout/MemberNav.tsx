'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import styles from './MemberNav.module.css';

const ITEMS = [
  { href: '/me', label: '我的主页' },
  { href: '/me/favorites', label: '我的收藏' },
  { href: '/me/drafts', label: '草稿箱' },
  { href: '/me/notifications', label: '通知中心' },
  { href: '/me/subscriptions', label: '我的订阅' },
  { href: '/me/following', label: '关注与粉丝' },
  { href: '/me/messages', label: '私信' },
  { href: '/me/security', label: '安全设置' },
  { href: '/me/settings', label: '资料设置' },
];

export function MemberNav() {
  const pathname = usePathname() || '/me';
  return (
    <nav className={['panel', styles.nav].join(' ')} aria-label="用户中心导航">
      {ITEMS.map((item) => {
        const active = pathname === item.href;
        return (
          <Link
            key={item.href}
            href={item.href}
            className={active ? styles.active : styles.item}
            aria-current={active ? 'page' : undefined}
          >
            {item.label}
          </Link>
        );
      })}
    </nav>
  );
}
