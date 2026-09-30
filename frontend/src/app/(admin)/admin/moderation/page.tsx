import type { Metadata } from 'next';
import Link from 'next/link';
import { adminGet } from '@/lib/admin.server';
import type { AdminModerateQueue } from '@/lib/api/types';
import { formatDateTime } from '@/lib/format';
import { ModerationActions } from '@/components/admin/ModerationActions';
import styles from './moderation.module.css';

export const metadata: Metadata = {
  title: '审核与举报',
  robots: { index: false, follow: false },
};

export default async function ModerationPage() {
  const result = await adminGet<AdminModerateQueue>('/api/v1/admin/moderate');

  if (!result.ok) {
    return (
      <div className={styles.forbidden}>
        <h1>无访问权限</h1>
        <p>审核队列要求 content.moderate 与 moderate.queue；版主还需管辖对应版块，并完成初始密码修改。</p>
        <Link href="/admin">返回概览</Link>
      </div>
    );
  }

  const { threads, posts, reports } = result.data;
  const total = threads.length + posts.length + reports.length;

  return (
    <div>
      <div className={styles.pageHead}>
        <h1 className={styles.pageTitle}>审核与举报</h1>
        <span className={styles.pageMeta}>各队列最多返回 50 条，不含分页总数；当前共 {total} 条</span>
      </div>

      <section className={['panel', styles.section].join(' ')}>
        <h2 className={styles.sectionTitle}>待审主题（{threads.length}）</h2>
        {threads.length > 0 ? (
          <ul className={styles.list}>
            {threads.map((thread) => (
              <li key={thread.id} className={styles.item}>
                <div className={styles.itemBody}>
                  <Link className={styles.itemTitle} href={'/threads/' + thread.id}>
                    {thread.title}
                  </Link>
                  <span className={styles.itemMeta}>
                    {thread.authorName} · {thread.forumId} 版 · {formatDateTime(thread.createdAt)}
                    {thread.pendingReason ? ' · 原因 ' + thread.pendingReason : ''}
                  </span>
                </div>
                <ModerationActions kind="thread" id={thread.id} />
              </li>
            ))}
          </ul>
        ) : (
          <p className={styles.empty}>没有待审主题。</p>
        )}
      </section>

      <section className={['panel', styles.section].join(' ')}>
        <h2 className={styles.sectionTitle}>待审回复（{posts.length}）</h2>
        {posts.length > 0 ? (
          <ul className={styles.list}>
            {posts.map((post) => (
              <li key={post.id} className={styles.item}>
                <div className={styles.itemBody}>
                  <p className={styles.excerpt}>{post.content.slice(0, 160)}</p>
                  <span className={styles.itemMeta}>
                    {post.authorName} · {post.floor} 楼 · {formatDateTime(post.createdAt)}
                  </span>
                </div>
                <ModerationActions kind="post" id={post.id} />
              </li>
            ))}
          </ul>
        ) : (
          <p className={styles.empty}>没有待审回复。</p>
        )}
      </section>

      <section className={['panel', styles.section].join(' ')}>
        <h2 className={styles.sectionTitle}>举报（{reports.length}）</h2>
        {reports.length > 0 ? (
          <ul className={styles.list}>
            {reports.map((report) => (
              <li key={report.id} className={styles.item}>
                <div className={styles.itemBody}>
                  <p className={styles.excerpt}>{report.reason || report.excerpt || '（无理由）'}</p>
                  <span className={styles.itemMeta}>
                    举报人 {report.reporter} · 楼层 {report.floor} · 主题 {report.threadTtl || report.tId} ·{' '}
                    {formatDateTime(report.createdAt)}
                  </span>
                </div>
                <ModerationActions kind="report" id={report.id} />
              </li>
            ))}
          </ul>
        ) : (
          <p className={styles.empty}>没有待处理举报。</p>
        )}
      </section>
    </div>
  );
}
