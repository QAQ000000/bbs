import Link from 'next/link';
import type { ThreadSummary } from '@/lib/api/types';
import { formatCount, formatRelative } from '@/lib/format';
import { threadFlags } from '@/lib/thread';
import { Avatar } from '../ui/Avatar';
import { LevelBadge } from '../ui/LevelBadge';
import { IconComment, IconEye } from '../ui/Icons';
import { CoverImage } from './CoverImage';
import styles from './ThreadCard.module.css';

export interface ThreadCardProps {
  thread: ThreadSummary;
  forumName?: string;
  /** 列表摘要（纯文本，后端按可见首楼投影）。 */
  excerpt?: string;
  /** 可选封面；加载失败时自动隐藏。 */
  coverUrl?: string;
  showForum?: boolean;
  variant?: 'default' | 'home' | 'browse';
}

export function ThreadCard({ thread, forumName, excerpt, coverUrl, showForum = true, variant = 'default' }: ThreadCardProps) {
  const flags = threadFlags(thread);
  return (
    <article className={[styles.card, variant !== 'default' ? styles.home : '', variant === 'browse' ? styles.browse : ''].join(' ')}>
      <div className={styles.main}>
        <div className={variant !== 'default' ? styles.titleRow : undefined}>
        {flags.length > 0 ? (
          <div className={styles.flags}>
            {flags.map((flag) => (
              <span key={flag.key} className={[styles.flag, styles[flag.tone]].join(' ')}>
                {flag.label}
              </span>
            ))}
          </div>
        ) : null}
        <h2 className={styles.title}>
          <Link href={`/threads/${thread.id}`}>{thread.title}</Link>
        </h2>
        </div>
        {excerpt ? <p className={styles.excerpt}>{excerpt}</p> : null}
        <div className={styles.meta}>
          <Avatar userId={thread.authorId} name={thread.authorName} size={24} href={`/users/${thread.authorId}`} />
          <Link className={styles.author} href={`/users/${thread.authorId}`}>
            {thread.authorName}
          </Link>
          <span className={styles.level}>
            <LevelBadge level={thread.authorLevel} size="sm" />
          </span>
          {showForum && forumName ? (
            <Link className={styles.forum} href={`/forums/${thread.forumId}`}>
              {forumName}
            </Link>
          ) : null}
          <span className={styles.time}>{formatRelative(thread.lastPostAt || thread.createdAt)}</span>
          <span className={styles.counts}>
            <span className={styles.count} title="回复数">
              <IconComment />
              {formatCount(thread.postCount)}
            </span>
            <span className={styles.count} title="查看数">
              <IconEye />
              {formatCount(thread.viewCount)}
            </span>
          </span>
        </div>
      </div>
      {coverUrl ? (
        <div className={styles.cover}>
          <CoverImage src={coverUrl} />
        </div>
      ) : null}
    </article>
  );
}
