import type { Metadata } from 'next';
import Link from 'next/link';
import { adminGetState } from '@/lib/admin.server';
import type {
  AnalyticsSnapshotView,
  DatabasePoolStats,
  ForumStatsQueueStatus,
  SearchIndexQueueStatus,
  SiteSettingsAdmin,
} from '@/lib/api/types';
import { formatCount, formatDateTime } from '@/lib/format';
import styles from './analytics-admin.module.css';

export const metadata: Metadata = {
  title: '排行榜与报表',
  robots: { index: false, follow: false },
};

interface PointEntry {
  userId?: string;
  username?: string;
  score?: number;
}

function stateBadge(stale: boolean): { text: string; tone: string } {
  return stale ? { text: '数据已过期', tone: styles.warn } : { text: '正常', tone: styles.ok };
}

function stateLabel(state: 'notfound' | 'error' | 'forbidden'): string {
  if (state === 'notfound') return '尚未生成';
  if (state === 'forbidden') return '无权限';
  return '读取失败';
}

function SnapshotState({ state }: { state: 'notfound' | 'error' | 'forbidden' }) {
  const map = {
    notfound: { title: '快照尚未生成', tone: styles.warn, text: '后台 Worker 还没有生成这一类快照；不会用零值冒充结果。' },
    error: { title: '查询失败', tone: styles.bad, text: '读取快照接口失败，请稍后重新读取；这不同于“没有数据”。' },
    forbidden: { title: '无访问权限', tone: styles.bad, text: '当前账号缺少读取该快照的权限。' },
  } as const;
  const item = map[state];
  return (
    <div className={['panel', styles.card].join(' ')}>
      <p className={[styles.stateTitle, item.tone].join(' ')}>{item.title}</p>
      <p className={styles.hint}>{item.text}</p>
    </div>
  );
}

export default async function AdminAnalyticsPage() {
  const [points, site, forumStats, searchStats, settings] = await Promise.all([
    adminGetState<AnalyticsSnapshotView>('/api/v1/admin/analytics/points'),
    adminGetState<AnalyticsSnapshotView>('/api/v1/admin/analytics/site'),
    adminGetState<ForumStatsQueueStatus>('/api/v1/admin/forum-stats'),
    adminGetState<SearchIndexQueueStatus>('/api/v1/admin/search-stats'),
    adminGetState<SiteSettingsAdmin>('/api/v1/admin/settings'),
  ]);

  const timezone = settings.status === 'ok' ? settings.data.reportTimeZone : '未知（需要 settings.edit 才能读取）';
  const pointEntries =
    points.status === 'ok' && Array.isArray(points.data.payload) ? (points.data.payload as PointEntry[]) : [];
  const siteScalars =
    site.status === 'ok' && site.data.payload && typeof site.data.payload === 'object' && !Array.isArray(site.data.payload)
      ? Object.entries(site.data.payload as Record<string, unknown>).filter(([, value]) => typeof value !== 'object')
      : [];

  return (
    <div>
      <div className={styles.pageHead}>
        <h1 className={styles.pageTitle}>排行榜与报表</h1>
        <span className={styles.pageMeta}>
          数据来自后台快照，页面只读、不触发同步聚合。积分榜按 UTC 小时桶，站点报表按报表时区（当前 {timezone}）划分自然日。
        </span>
      </div>

      <section className={['panel', styles.card].join(' ')}>
        <div className={styles.cardHead}>
          <h2 className={styles.cardTitle}>快照列表</h2>
          <span className={styles.hint}>快照列表与报表相互分离 · 快照非实时 · 过期按保留期自动清理</span>
        </div>
        <table className={styles.table}>
          <thead>
            <tr>
              <th>快照名称</th>
              <th>统计日期</th>
              <th>刷新时间</th>
              <th>状态</th>
              <th>保留期</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {(
              [
                { key: 'points', label: '积分快照（points）', state: points },
                { key: 'site', label: '站点报表（site）', state: site },
              ] as const
            ).map((row) => (
              <tr key={row.key}>
                <td>{row.label}</td>
                <td>{row.state.status === 'ok' ? formatDateTime(row.state.data.periodStart) : '—'}</td>
                <td>{row.state.status === 'ok' ? formatDateTime(row.state.data.generatedAt) : '—'}</td>
                <td>{row.state.status === 'ok' ? stateBadge(row.state.data.stale).text : stateLabel(row.state.status)}</td>
                <td>{settings.status === 'ok' ? settings.data.analyticsRetentionDays + ' 天' : '—'}</td>
                <td>
                  <a href={'#snapshot-' + row.key}>
                    {row.state.status === 'ok' ? '查看' : '查看状态'}
                  </a>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <p className={styles.hint}>
          接口只提供按名称读取的两个快照，没有列表、导出或“重新生成”动作，因此这里不提供这些按钮。
        </p>
      </section>

      <div className={styles.grid}>
        <section id="snapshot-points" className={['panel', styles.card].join(' ')}>
          <div className={styles.cardHead}>
            <h2 className={styles.cardTitle}>积分快照（points）</h2>
            {points.status === 'ok' ? (
              <span className={[styles.badge, stateBadge(points.data.stale).tone].join(' ')}>
                {stateBadge(points.data.stale).text}
              </span>
            ) : null}
          </div>
          {points.status !== 'ok' ? (
            <SnapshotState state={points.status} />
          ) : (
            <>
              <dl className={styles.meta}>
                <div><dt>存储桶起点</dt><dd>{formatDateTime(points.data.periodStart)}</dd></div>
                <div><dt>生成时间</dt><dd>{formatDateTime(points.data.generatedAt)}</dd></div>
                <div><dt>距今</dt><dd>{formatCount(points.data.ageSeconds)} 秒</dd></div>
                <div><dt>刷新 / 过期</dt><dd>{formatCount(points.data.refreshIntervalSeconds)} / {formatCount(points.data.staleAfterSeconds)} 秒</dd></div>
              </dl>
              {pointEntries.length === 0 ? (
                <p className={styles.hint}>快照已生成但内容为空。</p>
              ) : (
                <table className={styles.table}>
                  <thead>
                    <tr><th>名次</th><th>用户</th><th>当前积分余额</th></tr>
                  </thead>
                  <tbody>
                    {pointEntries.map((entry, index) => (
                      <tr key={String(entry.userId ?? index)}>
                        <td>{index + 1}</td>
                        <td>
                          {entry.userId ? (
                            <Link href={'/users/' + entry.userId}>{entry.username ?? '#' + entry.userId}</Link>
                          ) : (
                            entry.username ?? '-'
                          )}
                        </td>
                        <td>{formatCount(entry.score ?? 0)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </>
          )}
        </section>

        <section id="snapshot-site" className={['panel', styles.card].join(' ')}>
          <div className={styles.cardHead}>
            <h2 className={styles.cardTitle}>站点报表（site）</h2>
            {site.status === 'ok' ? (
              <span className={[styles.badge, stateBadge(site.data.stale).tone].join(' ')}>
                {stateBadge(site.data.stale).text}
              </span>
            ) : null}
          </div>
          {site.status !== 'ok' ? (
            <SnapshotState state={site.status} />
          ) : (
            <>
              <dl className={styles.meta}>
                <div><dt>统计日边界</dt><dd>{formatDateTime(site.data.periodStart)}</dd></div>
                <div><dt>生成时间</dt><dd>{formatDateTime(site.data.generatedAt)}</dd></div>
                <div><dt>距今</dt><dd>{formatCount(site.data.ageSeconds)} 秒</dd></div>
                <div><dt>报表时区</dt><dd>{timezone}</dd></div>
              </dl>
              {siteScalars.length > 0 ? (
                <dl className={styles.meta}>
                  {siteScalars.map(([key, value]) => (
                    <div key={key}><dt>{key}</dt><dd>{String(value)}</dd></div>
                  ))}
                </dl>
              ) : null}
              <details className={styles.details}>
                <summary>完整快照内容（只读）</summary>
                <pre className={styles.pre}>{JSON.stringify(site.data.payload, null, 2)}</pre>
              </details>
            </>
          )}
        </section>

        <section className={['panel', styles.card].join(' ')}>
          <div className={styles.cardHead}>
            <h2 className={styles.cardTitle}>异步版块统计队列</h2>
          </div>
          {forumStats.status !== 'ok' ? (
            <SnapshotState state={forumStats.status} />
          ) : (
            <dl className={styles.meta}>
              <div><dt>异步发布</dt><dd>{forumStats.data.asyncPublication ? '开启' : '关闭'}</dd></div>
              <div><dt>待处理</dt><dd>{formatCount(forumStats.data.pending)}</dd></div>
              <div><dt>重试中</dt><dd>{formatCount(forumStats.data.retrying)}</dd></div>
              <div><dt>最旧事件年龄</dt><dd>{Math.round(forumStats.data.oldestAgeSeconds)} 秒</dd></div>
            </dl>
          )}
        </section>

        <section className={['panel', styles.card].join(' ')}>
          <div className={styles.cardHead}>
            <h2 className={styles.cardTitle}>搜索索引队列</h2>
          </div>
          {searchStats.status !== 'ok' ? (
            <SnapshotState state={searchStats.status} />
          ) : (
            <dl className={styles.meta}>
              <div><dt>待处理</dt><dd>{formatCount(searchStats.data.pending)}</dd></div>
              <div><dt>重试中</dt><dd>{formatCount(searchStats.data.retrying)}</dd></div>
              <div><dt>最旧事件年龄</dt><dd>{Math.round(searchStats.data.oldestAgeSeconds)} 秒</dd></div>
            </dl>
          )}
        </section>
      </div>

      <p className={styles.hint}>
        跳转：<Link href="/admin">概览</Link> · <Link href="/admin/diagnostics">系统诊断</Link> ·
        <Link href="/leaderboard">前台公开积分余额榜</Link>
      </p>
    </div>
  );
}
