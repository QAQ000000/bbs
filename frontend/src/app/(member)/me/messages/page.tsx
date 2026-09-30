import type { Metadata } from 'next';
import Link from 'next/link';
import { safeRequest } from '@/lib/api/server';
import type { ConversationView, MessageView } from '@/lib/api/types';
import { formatDateTime, formatRelative } from '@/lib/format';
import { Avatar } from '@/components/ui/Avatar';
import { Pagination } from '@/components/ui/Pagination';
import { EmptyState } from '@/components/ui/StateView';
import { MessageComposer } from '@/components/forum/MessageComposer';
import { requireMember } from '../../guard';
import styles from './messages.module.css';

export const metadata: Metadata = {
  title: '私信',
  robots: { index: false, follow: false },
};

function parsePage(value?: string): number {
  const n = Number.parseInt(value ?? '1', 10);
  return Number.isFinite(n) && n > 0 ? n : 1;
}

function idParam(value?: string): string {
  return value && /^[0-9]+$/.test(value) ? value : '';
}

export default async function MessagesPage({
  searchParams,
}: {
  searchParams: { page?: string; cid?: string; before?: string };
}) {
  const session = await requireMember('/me/messages');
  const page = parsePage(searchParams.page);
  const cid = idParam(searchParams.cid);
  const before = idParam(searchParams.before);

  const conversationsEnvelope = await safeRequest<ConversationView[]>('/api/v1/me/conversations', {
    query: { page },
  });
  const conversations = conversationsEnvelope?.data ?? [];
  const selected = cid ? conversations.find((item) => item.id === cid) ?? null : null;

  const messagesEnvelope =
    cid && selected
      ? await safeRequest<MessageView[]>('/api/v1/conversations/' + cid + '/messages', {
          query: { before: before || undefined },
        })
      : null;
  const ordered = (messagesEnvelope?.data ?? []).slice().reverse();
  const latestMessageId = messagesEnvelope?.data?.[0]?.id;
  const oldestId = ordered[0]?.id;

  return (
    <div>
      <h1 className={styles.title}>私信</h1>
      <div className={styles.layout}>
        <section className={['panel', styles.listPanel].join(' ')} aria-label="会话列表">
          {conversations.length > 0 ? (
            <ul className={styles.conversations}>
              {conversations.map((item) => {
                const active = item.id === cid;
                return (
                  <li key={item.id}>
                    <Link
                      href={'/me/messages?cid=' + item.id + '&page=' + page}
                      className={active ? styles.convActive : styles.conv}
                      aria-current={active ? 'true' : undefined}
                    >
                      <Avatar userId={item.otherId} name={item.otherName} size={36} />
                      <div className={styles.convBody}>
                        <p className={styles.convName}>
                          {item.otherName}
                          {item.blocked ? <span className={styles.badgeBlocked}>已屏蔽</span> : null}
                          {!item.blocked && item.waitingForReply ? <span className={styles.badgeWait}>等待回复</span> : null}
                        </p>
                        <span className={styles.convTime}>{formatRelative(item.lastMessageAt)}</span>
                      </div>
                      {item.unread > 0 ? <span className={styles.unread}>{item.unread}</span> : null}
                    </Link>
                  </li>
                );
              })}
            </ul>
          ) : (
            <EmptyState
              title="暂无私信"
              description="你可以从用户主页发起私信，无需互相关注。"
              action={
                <Link className={styles.action} href="/forums">
                  去版块找人
                </Link>
              }
            />
          )}
          <div className={styles.listPagination}>
            <Pagination
              page={conversationsEnvelope?.meta?.page ?? 1}
              totalPages={conversationsEnvelope?.meta?.totalPages ?? 0}
              basePath="/me/messages"
              query={{ cid }}
              ariaLabel="会话分页"
            />
          </div>
        </section>

        <section className={['panel', styles.threadPanel].join(' ')} aria-label="消息历史">
          {selected ? (
            <>
              <header className={styles.threadHeader}>
                <Link href={'/users/' + selected.otherId} className={styles.threadPeer}>
                  <Avatar userId={selected.otherId} name={selected.otherName} size={28} />
                  {selected.otherName}
                </Link>
                {before ? (
                  <Link className={styles.olderLink} href={'/me/messages?cid=' + selected.id + '&page=' + page}>
                    返回最新消息
                  </Link>
                ) : null}
              </header>

              <div className={styles.history}>
                {ordered.length > 0 ? (
                  ordered.map((message) => {
                    const mine = message.senderId === session.user.id;
                    return (
                      <article key={message.id} className={mine ? styles.mine : styles.theirs}>
                        <div className={styles.bubble}>
                          <p className={styles.messageBody}>{message.body}</p>
                          <span className={styles.messageTime}>{formatDateTime(message.createdAt)}</span>
                        </div>
                      </article>
                    );
                  })
                ) : (
                  <p className={styles.historyEmpty}>还没有消息，发送第一条吧。</p>
                )}
              </div>

              {!before && oldestId && ordered.length >= 50 ? (
                <div className={styles.older}>
                  <Link
                    className={styles.olderLink}
                    href={'/me/messages?cid=' + selected.id + '&page=' + page + '&before=' + oldestId}
                  >
                    加载更早的消息
                  </Link>
                </div>
              ) : null}

              <MessageComposer conversation={selected} latestMessageId={latestMessageId} />
            </>
          ) : cid && !selected ? (
            <EmptyState title="会话不在当前页" description="请从左侧会话列表选择，或返回第一页。" />
          ) : (
            <EmptyState title="选择一个会话" description="从左侧选择会话查看历史消息。" />
          )}
        </section>
      </div>
    </div>
  );
}
