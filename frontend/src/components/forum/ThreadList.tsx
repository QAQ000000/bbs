import Link from 'next/link';
import type { ThreadSummary } from '@/lib/api/types';
import { ThreadCard } from './ThreadCard';
import { EmptyState } from '../ui/StateView';
import { IconPin } from '../ui/Icons';
import styles from './ThreadList.module.css';

export interface ThreadListProps {
  threads: ThreadSummary[];
  stickies?: ThreadSummary[];
  forumNames?: Record<string, string>;
  showForum?: boolean;
  emptyTitle?: string;
  emptyDescription?: string;
  variant?: 'default' | 'home' | 'browse';
}

export function ThreadList({
  threads,
  stickies = [],
  forumNames = {},
  showForum = true,
  emptyTitle = '暂时没有主题',
  emptyDescription = '成为第一个在这里发起讨论的人。',
  variant = 'default',
}: ThreadListProps) {
  const hasStickies = stickies.length > 0;
  if (!hasStickies && threads.length === 0) {
    return (
      <div className="panel">
        <EmptyState
          title={emptyTitle}
          description={emptyDescription}
          action={
            <Link className={styles.action} href="/new">
              发布主题
            </Link>
          }
        />
      </div>
    );
  }
  return (
    <div className={variant === 'browse' ? styles.browse : undefined}>
      {hasStickies ? (
        <section className={[styles.stickySection, variant !== 'default' ? styles.homeSticky : ''].join(' ')} aria-label="置顶主题">
          <p className={styles.sectionTitle}>
            <IconPin />
            置顶主题
          </p>
          <div className={[styles.list, variant !== 'default' ? styles.homeList : ''].join(' ')}>
            {stickies.map((thread) => (
              <ThreadCard
                key={thread.id}
                thread={thread}
                forumName={forumNames[thread.forumId]}
                showForum={showForum}
                excerpt={thread.excerpt}
                coverUrl={thread.coverUrl}
                variant={variant}
              />
            ))}
          </div>
        </section>
      ) : null}
      <div className={[styles.list, variant !== 'default' ? styles.homeList : ''].join(' ')}>
        {threads.map((thread) => (
          <ThreadCard
            key={thread.id}
            thread={thread}
            forumName={forumNames[thread.forumId]}
            showForum={showForum}
            excerpt={thread.excerpt}
            coverUrl={thread.coverUrl}
            variant={variant}
          />
        ))}
      </div>
    </div>
  );
}
