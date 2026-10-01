'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useState } from 'react';
import { Dropdown, Menu } from '@arco-design/web-react';
import {
  IconApps,
  IconBug,
  IconCheckCircle,
  IconDashboard,
  IconEmail,
  IconExclamationCircle,
  IconGift,
  IconIdcard,
  IconMenu,
  IconMenuFold,
  IconMenuUnfold,
  IconSafe,
  IconSettings,
  IconStorage,
  IconTag,
  IconThunderbolt,
  IconTrophy,
  IconUser,
} from '@arco-design/web-react/icon';
import styles from './AdminShell.module.css';

interface NavItem {
  href: string;
  label: string;
  icon: React.ComponentType;
  ready: boolean;
}

const NAV: NavItem[] = [
  { href: '/admin', label: '概览', icon: IconDashboard, ready: true },
  { href: '/admin/moderation', label: '审核与举报', icon: IconExclamationCircle, ready: true },
  { href: '/admin/forums', label: '分类与版块', icon: IconApps, ready: true },
  { href: '/admin/users', label: '用户与封禁', icon: IconUser, ready: true },
  { href: '/admin/tags', label: '标签管理', icon: IconTag, ready: true },
  { href: '/admin/membership', label: '会员等级', icon: IconIdcard, ready: true },
  { href: '/admin/titles', label: '称号与条件', icon: IconTrophy, ready: true },
  { href: '/admin/engagement', label: '互动配置', icon: IconThunderbolt, ready: true },
  { href: '/admin/polls', label: '投票管理', icon: IconCheckCircle, ready: true },
  { href: '/admin/bounties', label: '悬赏管理', icon: IconGift, ready: true },
  { href: '/admin/analytics', label: '排行榜与报表', icon: IconStorage, ready: true },
  { href: '/admin/notifications', label: '邮件队列', icon: IconEmail, ready: true },
  { href: '/admin/permissions', label: '权限与会话', icon: IconSafe, ready: true },
  { href: '/admin/diagnostics', label: '系统诊断', icon: IconBug, ready: true },
  { href: '/admin/settings', label: '站点设置', icon: IconSettings, ready: true },
];

export function AdminShell({
  username,
  children,
}: {
  username: string;
  children: React.ReactNode;
}) {
  const pathname = usePathname() || '/admin';
  const [collapsed, setCollapsed] = useState(false);
  const current = NAV.find((item) => item.ready && (item.href === '/admin' ? pathname === item.href : pathname === item.href || pathname.startsWith(`${item.href}/`)));

  return (
    <div className={[styles.shell, collapsed ? styles.collapsed : ''].join(' ')}>
      <aside className={styles.sidebar}>
        <Link href="/admin" className={styles.brand}>
          <span className={styles.logo}>G</span>
          <span>GoBBS Admin</span>
        </Link>
        <nav className={styles.nav} aria-label="后台导航">
          {NAV.map((item) => {
            const Icon = item.icon;
            if (!item.ready) {
              return (
                <span
                  key={item.href}
                  className={[styles.item, styles.disabled].join(' ')}
                  title="该模块将在后续批次实现"
                  aria-disabled="true"
                >
                  <Icon />
                  <span>{item.label}</span>
                  <span className={styles.soon}>待实现</span>
                </span>
              );
            }
            const active = item.href === '/admin' ? pathname === '/admin' : pathname === item.href || pathname.startsWith(`${item.href}/`);
            return (
              <Link
                key={item.href}
                href={item.href}
                title={collapsed ? item.label : undefined}
                className={active ? styles.active : styles.item}
                aria-current={active ? 'page' : undefined}
              >
                <Icon />
                <span>{item.label}</span>
              </Link>
            );
          })}
        </nav>
        <button
          type="button"
          className={styles.collapseButton}
          onClick={() => setCollapsed((value) => !value)}
          aria-label={collapsed ? '展开后台侧栏' : '收起后台侧栏'}
          title={collapsed ? '展开后台侧栏' : '收起后台侧栏'}
          aria-expanded={!collapsed}
        >
          {collapsed ? <IconMenuUnfold /> : <IconMenuFold />}
        </button>
      </aside>
      <div className={styles.body}>
        <header className={styles.topbar}>
          <Dropdown
            trigger="click"
            position="bl"
            droplist={
              <Menu selectedKeys={[current?.href ?? '/admin']}>
                {NAV.filter((item) => item.ready).map((item) => (
                  <Menu.Item key={item.href}>
                    <Link href={item.href}>{item.label}</Link>
                  </Menu.Item>
                ))}
              </Menu>
            }
          >
            <button type="button" className={styles.mobileMenu} aria-label="后台导航菜单" title="后台导航菜单">
              <IconMenu />
            </button>
          </Dropdown>
          <span className={styles.crumb}>管理后台 / {current?.label ?? '概览'}</span>
          <div className={styles.topRight}>
            <Link href="/" className={styles.frontLink}>
              返回前台
            </Link>
            <span className={styles.user}>{username} · 管理员</span>
          </div>
        </header>
        <main className={styles.content}>{children}</main>
      </div>
    </div>
  );
}
