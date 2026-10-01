'use client';

import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { useSession } from '@/lib/auth/session';
import { Avatar } from '@/components/ui/Avatar';
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
  const router = useRouter();
  const { user } = useSession();
  return (
    <nav className={styles.nav} aria-label="用户中心导航">
      {user ? (
        <Link href="/me" className={styles.identity}>
          <Avatar userId={user.id} name={user.username} size={64} />
          <strong>{user.username}</strong>
          <span>{user.signature || '我的会员中心'}</span>
        </Link>
      ) : null}
      <label className={styles.mobileSelect}>
        <span>会员中心</span>
        <select
          aria-label="切换会员中心页面"
          value={ITEMS.some(item => item.href === pathname) ? pathname : '/me/security'}
          onChange={event => router.push(event.target.value)}
        >
          {ITEMS.map(item => <option key={item.href} value={item.href}>{item.label}</option>)}
        </select>
      </label>
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
