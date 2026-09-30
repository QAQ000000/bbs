import Link from 'next/link';
import type { PostView } from '@/lib/api/types';
import { formatDateTime, formatSize } from '@/lib/format';
import Markdown from '@/lib/markdown/Markdown';
import type { SmileyMap } from '@/lib/markdown/smiley';
import { Avatar } from '../ui/Avatar';
import { LevelBadge, TitleBadge } from '../ui/LevelBadge';
import { IconCheck, IconDownload } from '../ui/Icons';
import { LikeButton } from './LikeButton';
import { ReportButton } from './ReportButton';
import { DeletePostButton } from './DeletePostButton';
import { AcceptButton } from './AcceptButton';
import styles from './PostFloor.module.css';

export interface PostFloorProps {
  post: PostView;
  threadId: string;
  forumId: string;
  smileys: SmileyMap;
  loggedIn: boolean;
  canReply: boolean;
  quoteHref: string;
}

/** 楼层：主栏正文 + 左侧作者信息，非聊天气泡布局。 */
export function PostFloor({ post, threadId, forumId, smileys, loggedIn, canReply, quoteHref }: PostFloorProps) {
  return (
    <article id={'p' + post.id} className={styles.floor}>
      <div className={styles.author}>
        <Avatar userId={post.authorId} name={post.authorName} size={44} href={'/users/' + post.authorId} />
        <Link className={styles.name} href={'/users/' + post.authorId}>
          {post.authorName}
        </Link>
        <LevelBadge level={post.authorLevel} size="sm" />
        <TitleBadge title={post.equippedTitle} />
      </div>

      <div className={styles.body}>
        {post.accepted ? (
          <p className={styles.accepted}>
            <IconCheck />
            已采纳答复
          </p>
        ) : null}
        {post.pending ? (
          <p className={styles.pending}>待审核：{post.pendingReason || '审核通过后公开显示'}</p>
        ) : null}
        {post.replyTo ? (
          <p className={styles.quote}>
            {post.replyTo.available
              ? '引用 ' + (post.replyTo.floor ?? '') + ' 楼 ' + (post.replyTo.authorName ?? '') + '：内容见对应楼层'
              : '引用的内容已不可见'}
          </p>
        ) : null}

        <Markdown content={post.content} smileys={smileys} />

        {post.attachments.length > 0 ? (
          <ul className={styles.attachments}>
            {post.attachments.map((file) => (
              <li key={file.id}>
                <a className={styles.attachment} href={file.url} download={file.name}>
                  <IconDownload />
                  <span className={styles.attName}>{file.name}</span>
                  <span className={styles.attSize}>{formatSize(file.size)}</span>
                </a>
              </li>
            ))}
          </ul>
        ) : null}

        <div className={styles.footer}>
          <span className={styles.meta}>
            {post.floor} 楼 · {formatDateTime(post.createdAt)}
            {post.hasEdited ? ' · 已编辑' : ''}
          </span>
          <div className={styles.actions}>
            {post.capabilities.canLike ? (
              <LikeButton postId={post.id} initialLiked={post.viewerHasLiked} initialCount={post.likeCount} />
            ) : (
              <span className={styles.likeStatic}>{post.likeCount > 0 ? post.likeCount + ' 赞' : ''}</span>
            )}
            {loggedIn && canReply ? (
              <>
                <Link className={styles.action} href={quoteHref}>
                  引用
                </Link>
                <Link className={styles.action} href={quoteHref}>
                  回复
                </Link>
              </>
            ) : null}
            <AcceptButton
              postId={post.id}
              accepted={post.accepted}
              canAccept={Boolean(post.capabilities.canAccept)}
              canUnaccept={Boolean(post.capabilities.canUnaccept)}
            />
            {post.capabilities.canEdit ? (
              <Link className={styles.action} href={'/posts/' + post.id + '/edit'}>
                编辑
              </Link>
            ) : null}
            {post.capabilities.canReport ? <ReportButton postId={post.id} /> : null}
            {post.capabilities.canDelete ? (
              <DeletePostButton postId={post.id} floor={post.floor} threadId={threadId} forumId={forumId} />
            ) : null}
          </div>
        </div>
      </div>
    </article>
  );
}
