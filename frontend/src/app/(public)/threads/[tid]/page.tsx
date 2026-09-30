import type { Metadata } from 'next';
import { Fragment } from 'react';
import Link from 'next/link';
import { notFound } from 'next/navigation';
import {
  getBounty,
  getEngagementRules,
  getForum,
  getForumThreads,
  getPointsAccount,
  getPoll,
  getPost,
  getPosts,
  getThread,
  getUserProfile,
} from '@/lib/data.server';
import { getSession } from '@/lib/auth/server';
import { getSite } from '@/lib/site.server';
import { getSmileyMap } from '@/lib/markdown/smiley.server';
import { threadFlags } from '@/lib/thread';
import { formatCount, formatDateTime, formatRelative } from '@/lib/format';
import { Breadcrumb } from '@/components/ui/Breadcrumb';
import { Avatar } from '@/components/ui/Avatar';
import { LevelBadge, TitleBadge } from '@/components/ui/LevelBadge';
import { PostFloor } from '@/components/forum/PostFloor';
import { Pagination } from '@/components/ui/Pagination';
import { ErrorState } from '@/components/ui/StateView';
import { FavoriteButton } from '@/components/forum/FavoriteButton';
import { ShareButton } from '@/components/forum/ShareButton';
import { FollowButton } from '@/components/forum/FollowButton';
import { SubscribeButton } from '@/components/forum/SubscribeButton';
import { ReplyComposer } from '@/components/forum/ReplyComposer';
import { PollCard } from '@/components/forum/PollCard';
import { BountyCard } from '@/components/forum/BountyCard';
import styles from './thread.module.css';

function parsePage(value?: string): number {
  const n = Number.parseInt(value ?? '1', 10);
  return Number.isFinite(n) && n > 0 ? Math.min(n, 10000) : 1;
}

function plainText(markdown: string, limit = 120): string {
  return markdown
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/[#>*_`\[\]()!|-]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .slice(0, limit);
}

export async function generateMetadata({
  params,
  searchParams,
}: {
  params: { tid: string };
  searchParams: { page?: string };
}): Promise<Metadata> {
  const thread = await getThread(params.tid);
  if (!thread) return { title: '主题不存在' };
  const posts = await getPosts(params.tid, 1);
  const first = posts?.data?.[0]?.content ?? '';
  const page = parsePage(searchParams.page);
  return {
    title: thread.title,
    description: plainText(first) || thread.title,
    alternates: {
      canonical: '/threads/' + thread.id + (page > 1 ? '?page=' + page : ''),
      // 可发现的机器可读出口：同一页面的 Markdown 文档。
      types: { 'text/markdown': '/content/threads/' + thread.id + '.md' + (page > 1 ? '?page=' + page : '') },
    },
    openGraph: { title: thread.title, description: plainText(first), type: 'article' },
  };
}

export default async function ThreadDetailPage({
  params,
  searchParams,
}: {
  params: { tid: string };
  searchParams: { page?: string; replyTo?: string };
}) {
  const tid = params.tid;
  const page = parsePage(searchParams.page);
  const thread = await getThread(tid);
  if (!thread) notFound();

  const session = await getSession();
  const loggedIn = Boolean(session.user);
  const canCreatePoll = Boolean(thread.capabilities?.canCreatePoll);
  const canCreateBounty = Boolean(thread.capabilities?.canCreateBounty);
  const showPoll = Boolean(thread.poll) || canCreatePoll;
  const showBounty = Boolean(thread.bounty) || canCreateBounty;

  // 互动数据只在需要时读取；规则为公开快照，写操作仍以后端校验为准。
  const [forum, postsPage, smileys, profile, relatedPage, site, rules, poll, bounty, account] = await Promise.all([
    getForum(thread.forumId),
    getPosts(tid, page),
    getSmileyMap(),
    getUserProfile(thread.authorId),
    getForumThreads(thread.forumId, 1, ''),
    getSite(),
    getEngagementRules(),
    showPoll ? getPoll(tid) : Promise.resolve(null),
    showBounty ? getBounty(tid) : Promise.resolve(null),
    loggedIn && showBounty ? getPointsAccount() : Promise.resolve(null),
  ]);

  let quoteTarget: { id: string; floor: number; authorName: string } | null = null;
  const replyToId = searchParams.replyTo;
  if (replyToId && /^[0-9]+$/.test(replyToId)) {
    const quoted = await getPost(replyToId);
    if (quoted && quoted.threadId === thread.id) {
      quoteTarget = { id: quoted.id, floor: quoted.floor, authorName: quoted.authorName };
    }
  }

  const flags = threadFlags(thread);
  const posts = postsPage?.data ?? [];
  const meta = postsPage?.meta;
  const canReply = Boolean(thread.capabilities?.canReply);
  const isAuthor = Boolean(session.user) && session.user?.id === thread.authorId;
  // 关注状态来自用户详情 DTO；undefined（旧后端 / 取数失败）显示“状态未确认”。
  const following = profile?.following ?? null;
  const related = (relatedPage?.data?.threads ?? []).filter((item) => item.id !== thread.id).slice(0, 5);
  const authorLevel = posts[0]?.authorLevel ?? thread.authorLevel;
  const authorTitle = posts[0]?.equippedTitle ?? thread.equippedTitle;

  return (
    <div className="container page">
      <Breadcrumb
        items={[
          { label: '首页', href: '/' },
          forum
            ? { label: forum.name, href: '/forums/' + forum.id }
            : { label: '版块', href: '/forums' },
          { label: thread.title },
        ]}
      />

      <div className={styles.grid}>
        <div className={styles.main}>
          <section className={['panel', styles.header].join(' ')}>
            <div className={styles.headerMain}>
              {flags.length > 0 ? (
                <div className={styles.flags}>
                  {flags.map((flag) => (
                    <span key={flag.key} className={[styles.flag, styles[flag.tone]].join(' ')}>
                      {flag.label}
                    </span>
                  ))}
                </div>
              ) : null}
              <h1 className={styles.title}>{thread.title}</h1>
              <div className={styles.meta}>
                <Avatar userId={thread.authorId} name={thread.authorName} size={22} href={'/users/' + thread.authorId} />
                <Link className={styles.author} href={'/users/' + thread.authorId}>
                  {thread.authorName}
                </Link>
                <LevelBadge level={authorLevel} size="sm" />
                <span>发布于 {formatDateTime(thread.createdAt)}</span>
                <span className={styles.dotSep}>·</span>
                <span>{formatCount(thread.viewCount)} 次浏览</span>
                <span className={styles.dotSep}>·</span>
                <span>{formatCount(thread.postCount)} 条回复</span>
              </div>
            </div>
            <div className={styles.headerActions}>
              {loggedIn ? (
                <FavoriteButton threadId={thread.id} initialFavorite={Boolean(thread.favorite)} />
              ) : null}
              <ShareButton title={thread.title} />
              <a
                className={styles.markdownLink}
                href={'/content/threads/' + thread.id + '.md' + (page > 1 ? '?page=' + page : '')}
                type="text/markdown"
                title="机器可读的 Markdown 版本（固定游客可见范围）"
              >
                Markdown
              </a>
            </div>
          </section>

          {thread.pending ? (
            <p className={styles.pendingBanner}>该主题正在审核中，仅作者与管理者可见。</p>
          ) : null}

          {postsPage ? (
            posts.length > 0 ? (
              <div className={styles.posts}>
                {posts.map((post) => (
                  <Fragment key={post.id}>
                    <PostFloor
                      post={post}
                      threadId={thread.id}
                      forumId={thread.forumId}
                      smileys={smileys}
                      loggedIn={loggedIn}
                      canReply={canReply}
                      quoteHref={'/threads/' + thread.id + '?page=' + page + '&replyTo=' + post.id + '#reply'}
                    />
                    {post.floor === 1 && (showPoll || showBounty) ? (
                      <>
                        {showPoll ? (
                          <PollCard
                            threadId={thread.id}
                            poll={poll}
                            rules={rules?.poll ?? null}
                            canCreate={canCreatePoll}
                            canVote={Boolean(thread.capabilities?.canVote)}
                            canClose={Boolean(thread.capabilities?.canClosePoll)}
                            loggedIn={loggedIn}
                            isAuthor={isAuthor}
                          />
                        ) : null}
                        {showBounty ? (
                          <BountyCard
                            threadId={thread.id}
                            bounty={bounty}
                            rules={rules?.bounty ?? null}
                            account={account}
                            canCreate={canCreateBounty}
                            canCancel={Boolean(thread.capabilities?.canCancelBounty)}
                            loggedIn={loggedIn}
                          />
                        ) : null}
                      </>
                    ) : null}
                  </Fragment>
                ))}
              </div>
            ) : (
              <div className="panel">
                <ErrorState title="本页没有回复" description="该页码超出范围，请返回第一页查看。" retryHref={'/threads/' + thread.id} />
              </div>
            )
          ) : (
            <div className="panel">
              <ErrorState title="楼层加载失败" description="无法读取本页回复，请稍后重试。" retryHref={'/threads/' + thread.id} />
            </div>
          )}

          {meta ? (
            <div className={styles.paginationWrap}>
              <Pagination page={meta.page} totalPages={meta.totalPages} basePath={'/threads/' + thread.id} ariaLabel="楼层分页" />
            </div>
          ) : null}

          <ReplyComposer
            threadId={thread.id}
            smileys={smileys}
            replyTo={quoteTarget}
            canReply={canReply}
            closed={thread.closed}
            loggedIn={loggedIn}
            uploadEnabled={Boolean(site?.uploadEnabled)}
          />
        </div>

        <aside className={styles.side}>
          <section className={['panel', styles.sideCard, styles.authorCard].join(' ')} aria-label="作者信息">
            <Avatar userId={thread.authorId} name={thread.authorName} size={56} href={'/users/' + thread.authorId} />
            <Link className={styles.authorName} href={'/users/' + thread.authorId}>
              {thread.authorName}
            </Link>
            <div className={styles.authorBadges}>
              <LevelBadge level={authorLevel} />
              <TitleBadge title={authorTitle} />
            </div>
            {profile ? (
              <dl className={styles.authorStats}>
                <div>
                  <dt>发帖</dt>
                  <dd>{formatCount(profile.reputation.posts)}</dd>
                </div>
                <div>
                  <dt>获赞</dt>
                  <dd>{formatCount(profile.reputation.likes)}</dd>
                </div>
              </dl>
            ) : null}
            {loggedIn && session.user && session.user.id !== thread.authorId ? (
              <FollowButton userId={thread.authorId} initialFollowing={following} block size="small" />
            ) : null}
          </section>

          {forum ? (
            <section className={['panel', styles.sideCard].join(' ')} aria-label="所属版块">
              <h2 className={styles.sideTitle}>
                <span className={styles.forumDot} aria-hidden="true" />
                {forum.name}
              </h2>
              <p className={styles.forumDesc}>{forum.description || '暂无版块说明'}</p>
              <div className={styles.forumActions}>
                <Link className={styles.forumLink} href={'/forums/' + forum.id}>
                  进入版块
                </Link>
                {loggedIn ? (
                  <SubscribeButton
                    kind="forum"
                    id={forum.id}
                    initialSubscribed={forum.subscribed ?? null}
                    size="small"
                  />
                ) : null}
              </div>
            </section>
          ) : null}

          <section className={['panel', styles.sideCard].join(' ')} aria-label="相关主题">
            <h2 className={styles.sideTitle}>相关主题</h2>
            {related.length > 0 ? (
              <ul className={styles.related}>
                {related.map((item) => (
                  <li key={item.id} className={styles.relatedItem}>
                    <Link className={styles.relatedTitle} href={'/threads/' + item.id}>
                      {item.title}
                    </Link>
                    <span className={styles.relatedMeta}>
                      {formatCount(item.postCount)} 回复 · {formatRelative(item.lastPostAt)}
                    </span>
                  </li>
                ))}
              </ul>
            ) : (
              <p className={styles.sideEmpty}>暂无相关主题</p>
            )}
          </section>
        </aside>
      </div>
    </div>
  );
}
