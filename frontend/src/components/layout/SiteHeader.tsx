'use client';

import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { useState } from 'react';
import { Dropdown, Input, Menu } from '@arco-design/web-react';
import {
  IconDown,
  IconEmail,
  IconNotification,
  IconPlus,
  IconSearch,
} from '@arco-design/web-react/icon';
import { useSession } from '@/lib/auth/session';
import { Avatar } from '../ui/Avatar';
import { Logo } from './Logo';
import styles from './SiteHeader.module.css';

const NAV = [
  { href: '/', label: '首页', match: (p: string) => p === '/' },
  { href: '/forums', label: '版块', match: (p: string) => p.startsWith('/forums') || p.startsWith('/threads') },
  { href: '/tags', label: '标签', match: (p: string) => p.startsWith('/tags') },
  { href: '/leaderboard', label: '排行榜', match: (p: string) => p.startsWith('/leaderboard') },
];

export interface SiteHeaderProps {
  siteName: string;
  notificationUnread?: number;
}

export function SiteHeader({ siteName, notificationUnread = 0 }: SiteHeaderProps) {
  const pathname = usePathname() || '/';
  const router = useRouter();
  const { user, logout } = useSession();
  const [keyword, setKeyword] = useState('');

  function submitSearch(event: React.FormEvent) {
    event.preventDefault();
    const value = keyword.trim();
    router.push(value ? `/search?q=${encodeURIComponent(value)}` : '/search');
  }

  async function handleMenu(key: string) {
    switch (key) {
      case 'home':
        router.push(user ? '/me' : '/login');
        break;
      case 'favorites':
        router.push('/me/favorites');
        break;
      case 'subscriptions':
        router.push('/me/subscriptions');
        break;
      case 'following':
        router.push('/me/following');
        break;
      case 'notification':
        router.push('/me/notifications');
        break;
      case 'messages':
        router.push('/me/messages');
        break;
      case 'security':
        router.push('/me/security');
        break;
      case 'settings':
        router.push('/me/settings');
        break;
      case 'logout':
        await logout();
        router.push('/');
        router.refresh();
        break;
      default:
        break;
    }
  }

  return (
    <header className={styles.header}>
      <div className={[styles.inner, 'container'].join(' ')}>
        <Link href="/" className={styles.brand} aria-label="返回首页">
          <Logo size={30} />
          <span className={styles.brandName}>{siteName}</span>
        </Link>

        <nav className={styles.nav} aria-label="主导航">
          {NAV.map((item) => {
            const active = item.match(pathname);
            return (
              <Link
                key={item.href}
                href={item.href}
                className={active ? styles.navActive : styles.navLink}
                aria-current={active ? 'page' : undefined}
              >
                {item.label}
              </Link>
            );
          })}
        </nav>

        <form className={styles.search} role="search" onSubmit={submitSearch}>
          <Input
            allowClear
            value={keyword}
            onChange={setKeyword}
            prefix={<IconSearch />}
            placeholder="搜索主题、用户…"
            aria-label="搜索主题、用户"
          />
        </form>

        <div className={styles.actions}>
          <Link href="/search" className={styles.mobileSearch} aria-label="搜索">
            <IconSearch />
          </Link>
          <Link href="/new" className={styles.publish}>
            <IconPlus />
            <span>发布主题</span>
          </Link>
          {user ? (
            <>
              <Link href="/me/notifications" className={styles.iconBtn} aria-label="通知">
                <IconNotification />
                {notificationUnread > 0 ? (
                  <span className={styles.dot} aria-label={`${notificationUnread} 条未读通知`} />
                ) : null}
              </Link>
              <Link href="/me/messages" className={styles.iconBtn} aria-label="私信">
                <IconEmail />
              </Link>
              <Dropdown
                trigger="click"
                position="br"
                droplist={
                  <Menu onClickMenuItem={(key) => void handleMenu(key)}>
                    <Menu.Item key="home">我的主页</Menu.Item>
                    <Menu.Item key="favorites">我的收藏</Menu.Item>
                    <Menu.Item key="subscriptions">我的订阅</Menu.Item>
                    <Menu.Item key="following">关注与粉丝</Menu.Item>
                    <Menu.Item key="notification">通知中心</Menu.Item>
                    <Menu.Item key="messages">私信</Menu.Item>
                    <Menu.Item key="security">安全设置</Menu.Item>
                    <Menu.Item key="settings">资料设置</Menu.Item>
                    <Menu.Item key="logout">退出登录</Menu.Item>
                  </Menu>
                }
              >
                <button type="button" className={styles.userBtn} aria-label="用户菜单">
                  <Avatar userId={user.id} name={user.username} size={30} />
                  <span className={styles.userName}>{user.username}</span>
                  <IconDown className={styles.caret} />
                </button>
              </Dropdown>
            </>
          ) : (
            <div className={styles.authLinks}>
              <Link href="/login" className={styles.login}>
                登录
              </Link>
              <Link href="/register" className={styles.register}>
                注册
              </Link>
            </div>
          )}
        </div>
      </div>
    </header>
  );
}
