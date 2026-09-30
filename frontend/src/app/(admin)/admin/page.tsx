import type { Metadata } from 'next';
import Link from 'next/link';
import { adminGet } from '@/lib/admin.server';
import type { AdminDashboard, AdminDiagnostics, AdminModerateQueue } from '@/lib/api/types';
import { formatCount, formatSize } from '@/lib/format';
import styles from './admin.module.css';

export const metadata: Metadata = {
  title: '后台概览',
  robots: { index: false, follow: false },
};

function formatUptime(seconds: number): string {
  const day = Math.floor(seconds / 86400);
  const hour = Math.floor((seconds % 86400) / 3600);
  if (day > 0) return day + ' 天 ' + hour + ' 小时';
  const minute = Math.floor((seconds % 3600) / 60);
  return hour + ' 小时 ' + minute + ' 分';
}

export default async function AdminOverviewPage() {
  const [dash, diagnostics, queue] = await Promise.all([
    adminGet<AdminDashboard>('/api/v1/admin'),
    adminGet<AdminDiagnostics>('/api/v1/admin/diagnostics'),
    adminGet<AdminModerateQueue>('/api/v1/admin/moderate'),
  ]);

  if (!dash.ok) {
    return (
      <div className={styles.forbidden}>
        <h1>无访问权限</h1>
        <p>当前账号没有后台面板权限，或尚未完成初始密码修改。</p>
        <Link href="/">返回首页</Link>
      </div>
    );
  }

  const data = dash.data;
  const diag = diagnostics.ok ? diagnostics.data : null;
  const pending = queue.ok
    ? queue.data.threads.length + queue.data.posts.length + queue.data.reports.length
    : null;

  const cards = [
    { label: '主题总数', value: formatCount(data.stats.totalThreads), hint: '含公开主题' },
    {
      label: '待审核内容',
      value: pending === null ? '—' : String(pending),
      hint: '队列积压，需尽快处理',
      tone: pending && pending > 0 ? 'warning' : undefined,
    },
    { label: '注册用户', value: formatCount(data.stats.members), hint: '被封禁 ' + data.banned + ' 人' },
    { label: '今日帖子', value: formatCount(data.stats.todayPosts), hint: '昨日 ' + data.stats.yesterdayPosts },
  ];

  return (
    <div>
      <div className={styles.pageHead}>
        <h1 className={styles.pageTitle}>概览</h1>
        <span className={styles.pageMeta}>来源：站点统计服务 · 运行 {formatUptime(data.uptimeSeconds)}</span>
      </div>

      <div className={styles.cards}>
        {cards.map((card) => (
          <section key={card.label} className={['panel', styles.card].join(' ')}>
            <p className={styles.cardLabel}>{card.label}</p>
            <p className={card.tone === 'warning' ? styles.cardValueWarning : styles.cardValue}>{card.value}</p>
            <p className={styles.cardHint}>{card.hint}</p>
          </section>
        ))}
      </div>

      <section className={['panel', styles.section].join(' ')}>
        <header className={styles.sectionHead}>
          <h2 className={styles.sectionTitle}>服务健康</h2>
          <span className={styles.sectionMeta}>
            {diag ? '数据库连接 ' + diag.databasePool.total + ' / ' + diag.databasePool.max : '诊断信息不可用'}
          </span>
        </header>
        {diag ? (
          <ul className={styles.health}>
            <li>
              <span className={styles.dotOk} aria-hidden="true" />
              数据库
              <em>缓存命中 {(diag.databaseWorkload.cacheHitRatio * 100).toFixed(2)}%</em>
            </li>
            <li>
              <span className={diag.forumStats.pending > 0 ? styles.dotWarn : styles.dotOk} aria-hidden="true" />
              论坛统计任务
              <em>待处理 {diag.forumStats.pending} · 重试 {diag.forumStats.retrying}</em>
            </li>
            <li>
              <span className={diag.searchIndex.pending > 0 ? styles.dotWarn : styles.dotOk} aria-hidden="true" />
              搜索索引
              <em>待处理 {diag.searchIndex.pending} · 重试 {diag.searchIndex.retrying}</em>
            </li>
            <li>
              <span className={diag.lockWaits > 0 ? styles.dotWarn : styles.dotOk} aria-hidden="true" />
              锁等待
              <em>{diag.lockWaits} 个</em>
            </li>
          </ul>
        ) : (
          <p className={styles.empty}>诊断接口不可用。</p>
        )}
      </section>

      <div className={styles.twoCol}>
        <section className={['panel', styles.section].join(' ')}>
          <header className={styles.sectionHead}>
            <h2 className={styles.sectionTitle}>最新待处理</h2>
            <Link className={styles.sectionLink} href="/admin/moderation">
              进入审核台
            </Link>
          </header>
          {queue.ok && pending ? (
            <ul className={styles.queue}>
              {queue.data.threads.slice(0, 3).map((thread) => (
                <li key={'t' + thread.id} className={styles.queueItem}>
                  <span className={styles.tagThread}>主题</span>
                  <span className={styles.queueTitle}>{thread.title}</span>
                  <span className={styles.queueMeta}>{thread.authorName}</span>
                </li>
              ))}
              {queue.data.posts.slice(0, 3).map((post) => (
                <li key={'p' + post.id} className={styles.queueItem}>
                  <span className={styles.tagPost}>回复</span>
                  <span className={styles.queueTitle}>{post.content.slice(0, 40)}</span>
                  <span className={styles.queueMeta}>{post.authorName}</span>
                </li>
              ))}
              {queue.data.reports.slice(0, 3).map((report) => (
                <li key={'r' + report.id} className={styles.queueItem}>
                  <span className={styles.tagReport}>举报</span>
                  <span className={styles.queueTitle}>{report.reason || report.excerpt.slice(0, 40)}</span>
                  <span className={styles.queueMeta}>{report.reporter}</span>
                </li>
              ))}
              {pending === 0 ? <li className={styles.emptyQueue}>当前没有待处理内容。</li> : null}
            </ul>
          ) : (
            <p className={styles.empty}>审核队列不可用或暂无权限。</p>
          )}
        </section>

        <section className={['panel', styles.section].join(' ')}>
          <header className={styles.sectionHead}>
            <h2 className={styles.sectionTitle}>运行信息</h2>
          </header>
          <dl className={styles.info}>
            <div>
              <dt>数据库大小</dt>
              <dd>{data.dbSize}</dd>
            </div>
            <div>
              <dt>运行时长</dt>
              <dd>{formatUptime(data.uptimeSeconds)}</dd>
            </div>
            <div>
              <dt>上传占用</dt>
              <dd>
                {formatSize(data.uploadBytes)} / {data.uploadLimitGB} GB
              </dd>
            </div>
            <div>
              <dt>订阅关系</dt>
              <dd>{formatCount(data.subscriptions)}</dd>
            </div>
            <div>
              <dt>回收站</dt>
              <dd>{formatCount(data.recycle)}</dd>
            </div>
            <div>
              <dt>Go 版本</dt>
              <dd>{data.goVersion}</dd>
            </div>
          </dl>
        </section>
      </div>
    </div>
  );
}
