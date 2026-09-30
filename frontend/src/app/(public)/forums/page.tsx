import type { Metadata } from 'next';
import Link from 'next/link';
import { serverGet } from '@/lib/api/server';
import type { CategoryWithForums } from '@/lib/api/types';
import { formatCount, formatRelative, isEmptyTime } from '@/lib/format';
import { Breadcrumb } from '@/components/ui/Breadcrumb';
import styles from './forums.module.css';

export const metadata: Metadata = {
  title: '版块目录',
  description: 'GoBBS 社区全部版块：按分类浏览技术交流、经验分享与休闲讨论区。',
  alternates: { canonical: '/forums' },
};

export default async function ForumsPage() {
  const categories = await serverGet<CategoryWithForums[]>('/api/v1/forums');
  return (
    <div className="container page">
      <Breadcrumb items={[{ label: '首页', href: '/' }, { label: '版块目录' }]} />
      <h1 className={styles.title}>版块目录</h1>
      <p className={styles.subtitle}>按分类浏览全部可读版块，进入版块查看置顶与主题列表。</p>

      {categories.length === 0 ? (
        <div className="panel">
          <p className={styles.empty}>当前没有可见版块。</p>
        </div>
      ) : (
        <div className={styles.groups}>
          {categories.map((category) => (
            <section key={category.id} className={styles.group} aria-label={category.name}>
              <h2 className={styles.groupTitle}>{category.name}</h2>
              <div className="panel panel-flush">
                <ul className={styles.forums}>
                  {category.forums.map((forum) => (
                    <li key={forum.id} className={styles.forum}>
                      <div className={styles.forumMain}>
                        <Link href={'/forums/' + forum.id} className={styles.forumName}>
                          {forum.name}
                        </Link>
                        <p className={styles.forumDesc}>{forum.description || '暂无版块说明'}</p>
                      </div>
                      <div className={styles.forumStats}>
                        <span>
                          <strong>{formatCount(forum.threadCount)}</strong> 主题
                        </span>
                        <span>
                          <strong>{formatCount(forum.postCount)}</strong> 回复
                        </span>
                        {forum.todayCount > 0 ? <span className={styles.today}>今日 {forum.todayCount}</span> : null}
                      </div>
                      <div className={styles.forumLast}>
                        {!isEmptyTime(forum.lastPostAt) && forum.lastThreadId !== '0' ? (
                          <>
                            <Link href={'/threads/' + forum.lastThreadId} className={styles.lastThread}>
                              {forum.lastThreadTitle || '查看最新主题'}
                            </Link>
                            <span className={styles.lastMeta}>
                              {forum.lastPostAuthor || '未知'} · {formatRelative(forum.lastPostAt)}
                            </span>
                          </>
                        ) : (
                          <span className={styles.lastMeta}>暂无主题</span>
                        )}
                      </div>
                    </li>
                  ))}
                </ul>
              </div>
            </section>
          ))}
        </div>
      )}
    </div>
  );
}
