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
  description: 'GoBBS 社区公开积分余额榜，数据来自后台快照。',
  alternates: { canonical: '/leaderboard' },
};

function statusLabel(board: LeaderboardView): { text: string; tone: string } {
  if (board.status === 'unavailable') return { text: '快照尚未生成', tone: styles.warn };
  if (board.status === 'stale' || board.stale) return { text: '快照已过期', tone: styles.warn };
  return { text: '快照正常', tone: styles.ok };
}

export default async function LeaderboardPage() {
  const [board, home] = await Promise.all([getPointsLeaderboard(), safeGet<HomeView>('/api/v1/home')]);
  const stats = home?.stats;

  return (
    <div className={styles.page}>
      <Breadcrumb items={[{ label: '首页', href: '/' }, { label: '排行榜' }]} />
      <h1 className={styles.title}>排行榜</h1>
      <p className={styles.subtitle}>
        当前版本只提供<strong>积分余额榜</strong>：展示账户当前的积分余额，<strong>不是</strong>累计获得积分、活跃度或周期新增。
        数据由后台 Worker 定期生成快照，页面只读快照，不在每次访问时重算全站排名。
      </p>

      {!board ? (
        <section className="panel">
          <ErrorState
            title="排行榜加载失败"
            description="无法读取排行榜接口，请稍后重试。"
            retryHref="/leaderboard"
          />
        </section>
      ) : board.status === 'unavailable' ? (
        <section className="panel">
          <EmptyState
            title="榜单快照尚未生成"
            description="后台 Worker 会按站点刷新周期生成积分余额榜；生成后这里会自动显示。期间不影响其它功能。"
            action={
              <Link href="/leaderboard" className={styles.action}>
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
              <span className={styles.metaValue}>当前积分余额（非累计获得 / 活跃度）</span>
            </div>
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
                约 {Math.round(board.refreshIntervalSeconds / 60)} 分钟；超过{' '}
                {Math.round(board.staleAfterSeconds / 60)} 分钟未刷新视为过期
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
                description="快照已生成，但没有符合公开规则的积分余额记录。"
              />
            </section>
          ) : (
            <section className={['panel', styles.board].join(' ')} aria-label="积分余额榜">
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
