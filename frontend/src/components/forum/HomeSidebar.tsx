import Link from 'next/link';
import type { Announcement, PopularView, SiteStats } from '@/lib/api/types';
import { Avatar } from '@/components/ui/Avatar';
import { formatCount, formatRelative } from '@/lib/format';
import { IconCalendar, IconChart, IconComment, IconHeart, IconMegaphone, IconPen } from './sidebarIcons';
import styles from './HomeSidebar.module.css';

export interface HomeSidebarProps {
  announcements: Announcement[];
  stats: SiteStats | null;
  unavailable?: boolean;
  popular: PopularView | null;
}

const QUICK_LINKS = [
  { href: '/checkin', label: '每日签到', icon: IconCalendar, hint: '赚取积分' },
  { href: '/new', label: '发布主题', icon: IconPen, hint: '' },
  { href: '/me/favorites', label: '我的收藏', icon: IconHeart, hint: '' },
  { href: '/help', label: '帮助中心', icon: IconComment, hint: '' },
];

export function HomeSidebar({ announcements, stats, unavailable, popular }: HomeSidebarProps) {
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
        ) : unavailable ? (
          <p className={styles.empty}>公告暂不可用，请稍后重试。</p>
        ) : (
          <p className={styles.empty}>暂无公告</p>
        )}
      </section>

      <section className={['panel', styles.card].join(' ')} aria-label="热门话题">
        <header className={styles.header}><h2 className={styles.title}>热门话题</h2></header>
        <p className={styles.popularHint}>近七天主题，按回复数、浏览数排序</p>
        {popular?.threads.length ? <ol className={styles.popularList}>{popular.threads.map((item) => <li key={item.id}><Link href={'/threads/' + item.id}>{item.title}</Link><span>{formatCount(item.replies)} 回复</span></li>)}</ol> : <p className={styles.empty}>{popular ? '近七天暂无可读主题' : '热门话题暂不可用'}</p>}
      </section>
      <section className={['panel', styles.card].join(' ')} aria-label="热门作者">
        <header className={styles.header}><h2 className={styles.title}>热门作者</h2></header>
        <p className={styles.popularHint}>近七天发帖的获赞数、发帖数排序</p>
        {popular?.authors.length ? <ul className={styles.authorList}>{popular.authors.map((item) => <li key={item.userId}><Avatar userId={item.userId} name={item.username} size={32} href={'/users/' + item.userId} /><div><Link href={'/users/' + item.userId}>{item.username}</Link><span>{formatCount(item.likes)} 获赞 · {formatCount(item.posts)} 发帖</span></div></li>)}</ul> : <p className={styles.empty}>{popular ? '近七天暂无公开作者' : '热门作者暂不可用'}</p>}
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
