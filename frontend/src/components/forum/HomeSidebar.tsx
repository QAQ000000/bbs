import Link from 'next/link';
import type { Announcement, SiteStats } from '@/lib/api/types';
import { formatCount, formatRelative } from '@/lib/format';
import { IconCalendar, IconChart, IconComment, IconHeart, IconMegaphone, IconPen } from './sidebarIcons';
import styles from './HomeSidebar.module.css';

export interface HomeSidebarProps {
  announcements: Announcement[];
  stats: SiteStats | null;
}

const QUICK_LINKS = [
  { href: '/checkin', label: '每日签到', icon: IconCalendar, hint: '赚取积分' },
  { href: '/new', label: '发布主题', icon: IconPen, hint: '' },
  { href: '/me/favorites', label: '我的收藏', icon: IconHeart, hint: '' },
  { href: '/help', label: '帮助中心', icon: IconComment, hint: '' },
];

// 设计稿还有“热门话题 / 热门作者”，当前后端没有对应公开聚合接口，
// 这里展示真实社区统计，避免用前端静态排序冒充全站榜单。
export function HomeSidebar({ announcements, stats }: HomeSidebarProps) {
  return (
    <div className={[styles.column, styles.mobileHidden].join(' ')}>
      <section className={['panel', styles.card].join(' ')} aria-label="社区公告">
        <header className={styles.header}>
          <h2 className={styles.title}>
            <IconMegaphone />
            社区公告
          </h2>
        </header>
        {announcements.length > 0 ? (
          <ul className={styles.announcements}>
            {announcements.map((item) => (
              <li key={item.id} className={styles.announcement}>
                <p className={styles.announcementText}>{item.content}</p>
                <span className={styles.announcementTime}>{formatRelative(item.createdAt)}</span>
              </li>
            ))}
          </ul>
        ) : (
          <p className={styles.empty}>暂无公告</p>
        )}
      </section>

      <section className={['panel', styles.card].join(' ')} aria-label="快捷入口">
        <header className={styles.header}>
          <h2 className={styles.title}>快捷入口</h2>
        </header>
        <ul className={styles.quick}>
          {QUICK_LINKS.map((link) => {
            const Icon = link.icon;
            return (
              <li key={link.href}>
                <Link href={link.href} className={styles.quickItem}>
                  <Icon />
                  <span className={styles.quickLabel}>{link.label}</span>
                  {link.hint ? <span className={styles.quickHint}>{link.hint}</span> : null}
                </Link>
              </li>
            );
          })}
        </ul>
      </section>

      <section className={['panel', styles.card].join(' ')} aria-label="社区统计">
        <header className={styles.header}>
          <h2 className={styles.title}>
            <IconChart />
            社区统计
          </h2>
        </header>
        {stats ? (
          <dl className={styles.stats}>
            <div>
              <dt>主题</dt>
              <dd>{formatCount(stats.totalThreads)}</dd>
            </div>
            <div>
              <dt>回复</dt>
              <dd>{formatCount(stats.totalPosts)}</dd>
            </div>
            <div>
              <dt>会员</dt>
              <dd>{formatCount(stats.members)}</dd>
            </div>
            <div>
              <dt>今日</dt>
              <dd>{formatCount(stats.todayPosts)}</dd>
            </div>
          </dl>
        ) : (
          <p className={styles.empty}>统计暂不可用</p>
        )}
      </section>
    </div>
  );
}
