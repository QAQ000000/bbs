import type { Metadata } from 'next';
import Link from 'next/link';
import { getPointsLeaderboard } from '@/lib/data.server';
import { safeGet } from '@/lib/api/server';
import type { HomeView, LeaderboardView } from '@/lib/api/types';
import { formatCount, formatDateTime } from '@/lib/format';
import { Breadcrumb } from '@/components/ui/Breadcrumb';
import { Avatar } from '@/components/ui/Avatar';
import { EmptyState, ErrorState } from '@/components/ui/StateView';
import styles from './leaderboard.module.css';

export const metadata: Metadata = {
  title: '排行榜',
  description: 'GoBBS 社区积分余额与日、周、月积分变化排行榜。',
  alternates: { canonical: '/leaderboard' },
};

function statusLabel(board: LeaderboardView): { text: string; tone: string } {
  if (board.status === 'unavailable') return { text: '快照尚未生成', tone: styles.warn };
  if (board.status === 'stale' || board.stale) return { text: '快照已过期', tone: styles.warn };
  return { text: '快照正常', tone: styles.ok };
}

const PERIODS = [{ id: 'balance', label: '余额榜' }, { id: 'day', label: '日榜' }, { id: 'week', label: '周榜' }, { id: 'month', label: '月榜' }];

export default async function LeaderboardPage({ searchParams }: { searchParams: { period?: string; date?: string } }) {
  const period = PERIODS.some((item) => item.id === searchParams.period) ? searchParams.period! : 'balance';
  const date = period !== 'balance' && /^\d{4}-\d{2}-\d{2}$/.test(searchParams.date ?? '') ? searchParams.date : undefined;
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai' }).format(new Date());
  const path = '/leaderboard?period=' + period + (date ? '&date=' + date : '');
  const [board, home] = await Promise.all([getPointsLeaderboard(period, date), safeGet<HomeView>('/api/v1/home')]);
  const stats = home?.stats;

  return (
    <div className={styles.page}>
      <Breadcrumb items={[{ label: '首页', href: '/' }, { label: '排行榜' }]} />
      <h1 className={styles.title}>排行榜</h1>
      <p className={styles.subtitle}>
        {period === 'balance' ? '积分余额榜按当前账户余额排名。' : '周期榜按上海时区的自然日、周一至周日、自然月统计积分净变化，包含扣减、退款和管理员调整，不含初始余额。'}
        每榜展示前 100 位公开用户。
      </p>
      <nav className={styles.tabs} aria-label="排行榜周期">
        {PERIODS.map((item) => <Link key={item.id} href={'/leaderboard?period=' + item.id} aria-current={period === item.id ? 'page' : undefined}>{item.label}</Link>)}
      </nav>
      {period !== 'balance' ? <form action="/leaderboard" className={styles.periodForm}>
        <input type="hidden" name="period" value={period} />
        <label htmlFor="board-date">周期日期</label><input id="board-date" name="date" type="date" defaultValue={date} max={today} required />
        <button type="submit">查询</button><Link href={'/leaderboard?period=' + period}>当前周期</Link>
      </form> : null}

      {!board ? (
        <section className="panel">
          <ErrorState
            title="排行榜加载失败"
            description="无法读取排行榜接口，请稍后重试。"
            retryHref={path}
          />
        </section>
      ) : board.status === 'unavailable' ? (
        <section className="panel">
          <EmptyState
            title="榜单快照尚未生成"
            description="所选周期尚无保留快照。后台定时生成当前和上一周期的数据，历史快照受保留策略限制。"
            action={
              <Link href={path} className={styles.action}>
                重新读取
              </Link>
            }
          />
        </section>
      ) : (
        <>
          <section className={['panel', styles.metaCard].join(' ')}>
            <div className={styles.metaRow}>
              <span className={styles.metaLabel}>数据口径</span>
              <span className={styles.metaValue}>{period === 'balance' ? '当前积分余额' : '周期积分净变化（不含初始余额）'}</span>
            </div>
            {board.periodStart && board.periodEnd ? <div className={styles.metaRow}><span className={styles.metaLabel}>统计区间</span><span className={styles.metaValue}>{formatDateTime(board.periodStart)} 至 {formatDateTime(board.periodEnd)}（不含结束时刻）</span></div> : null}
            {board.asOf ? <div className={styles.metaRow}><span className={styles.metaLabel}>统计截至</span><span className={styles.metaValue}>{formatDateTime(board.asOf)} · {board.complete ? '周期已结束' : '周期进行中'}</span></div> : null}
            <div className={styles.metaRow}>
              <span className={styles.metaLabel}>生成时间</span>
              <span className={styles.metaValue}>
                {board.generatedAt ? formatDateTime(board.generatedAt) : '未知'}
              </span>
            </div>
            <div className={styles.metaRow}>
              <span className={styles.metaLabel}>快照状态</span>
              <span className={statusLabel(board).tone}>{statusLabel(board).text}</span>
            </div>
            <div className={styles.metaRow}>
              <span className={styles.metaLabel}>刷新周期</span>
              <span className={styles.metaValue}>
                {board.complete ? '周期已结束，保留历史快照' : <>约 {Math.round(board.refreshIntervalSeconds / 60)} 分钟；超过{' '}{Math.round(board.staleAfterSeconds / 60)} 分钟未刷新视为过期</>}
              </span>
            </div>
            {board.stale ? (
              <p className={styles.staleNote}>
                快照已过期（生成于 {board.generatedAt ? formatDateTime(board.generatedAt) : '未知'}），后台正在重试刷新；
                榜单内容可能不是最新。
              </p>
            ) : null}
          </section>

          {board.entries.length === 0 ? (
            <section className="panel">
              <EmptyState
                title="榜单暂无公开用户"
                description="快照已生成，但没有符合公开规则的积分记录。"
              />
            </section>
          ) : (
            <section className={['panel', styles.board].join(' ')} aria-label={PERIODS.find((item) => item.id === period)?.label}>
              <ol className={styles.list}>
                {board.entries.map((entry) => (
                  <li key={entry.userId} className={[styles.item, entry.rank <= 3 ? styles.top : ''].join(' ')}>
                    <span className={styles.rank}>{entry.rank}</span>
                    <Avatar userId={entry.userId} name={entry.username} size={36} href={'/users/' + entry.userId} />
                    <Link className={styles.name} href={'/users/' + entry.userId}>
                      {entry.username}
                    </Link>
                    <span className={styles.points}>{formatCount(entry.points)}</span>
                    <span className={styles.unit}>积分</span>
                  </li>
                ))}
              </ol>
              <p className={styles.footNote}>
                封禁（禁止登录）账号在生成与读取时都会被排除；禁言用户的公开资料仍会展示。
              </p>
            </section>
          )}
        </>
      )}

      {stats ? (
        <section className={['panel', styles.statsCard].join(' ')}>
          <h2 className={styles.cardTitle}>站点概览</h2>
          <dl className={styles.stats}>
            <div>
              <dt>主题总数</dt>
              <dd>{formatCount(stats.totalThreads)}</dd>
            </div>
            <div>
              <dt>帖子总数</dt>
              <dd>{formatCount(stats.totalPosts)}</dd>
            </div>
            <div>
              <dt>注册会员</dt>
              <dd>{formatCount(stats.members)}</dd>
            </div>
            <div>
              <dt>今日帖子</dt>
              <dd>{formatCount(stats.todayPosts)}</dd>
            </div>
          </dl>
        </section>
      ) : null}

      <p className={styles.back}>
        <Link href="/">返回首页信息流</Link>
      </p>
    </div>
  );
}
