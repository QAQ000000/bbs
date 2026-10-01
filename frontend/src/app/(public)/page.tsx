import Link from 'next/link';
import type { Metadata } from 'next';
import { safeGet, safeRequest } from '@/lib/api/server';
import { getSession } from '@/lib/auth/server';
import { getSite } from '@/lib/site.server';
import { formatCount } from '@/lib/format';
import { forumNameMap } from '@/lib/thread';
import type { FeedMeta, FollowFeedData, HomeView, TagView, ThreadListData } from '@/lib/api/types';
import { ForumNav } from '@/components/forum/ForumNav';
import { HomeSidebar } from '@/components/forum/HomeSidebar';
import { ThreadList } from '@/components/forum/ThreadList';
import { FollowFeedList } from '@/components/forum/FollowFeedList';
import { FeedTabs } from '@/components/forum/FeedTabs';
import { Pagination } from '@/components/ui/Pagination';
import { EmptyState, ErrorState } from '@/components/ui/StateView';
import styles from './home.module.css';

export const metadata: Metadata = {
  title: '首页',
  description: 'GoBBS 开发者社区综合信息流：技术交流、经验分享与问题求助。',
  alternates: { canonical: '/' },
};

function parsePage(value?: string): number {
  const n = Number.parseInt(value ?? '1', 10);
  return Number.isFinite(n) && n > 0 ? Math.min(n, 10000) : 1;
}

/** 关注流的每页条数，与后端一致限制在 1-50。 */
function parseLimit(value?: string): number {
  const n = Number.parseInt(value ?? '20', 10);
  return Number.isFinite(n) && n > 0 ? Math.min(n, 50) : 20;
}

const FEED_STREAM: Record<string, 'forums' | 'users'> = { forums: 'forums', people: 'users' };

export default async function HomePage({
  searchParams,
}: {
  searchParams: { page?: string; feed?: string; cursor?: string; limit?: string };
}) {
  const page = parsePage(searchParams.page);
  const feed = searchParams.feed === 'forums' || searchParams.feed === 'people' ? searchParams.feed : 'all';
  const stream = FEED_STREAM[feed];
  const cursor = (searchParams.cursor ?? '').trim();
  const followLimit = parseLimit(searchParams.limit);

  const session = await getSession();
  const loggedIn = Boolean(session.user);

  const [site, home, tagPage, feedPage, followPage] = await Promise.all([
    getSite(),
    safeGet<HomeView>('/api/v1/home'),
    safeRequest<TagView[]>('/api/v1/tags', { query: { page: 1 } }),
    feed === 'all' ? safeRequest<ThreadListData>('/api/v1/threads', { query: { page } }) : Promise.resolve(null),
    // 关注流由后端在数据库层过滤与排序，前端不做浏览器侧拼接。
    feed !== 'all' && loggedIn
      ? safeRequest<FollowFeedData>('/api/v1/me/feed/' + stream, {
          query: { cursor: cursor || undefined, limit: followLimit },
        })
      : Promise.resolve(null),
  ]);

  const categories = home?.categories ?? [];
  const forumNames = forumNameMap(categories);
  const feedData = feedPage?.data;
  const meta = feedPage?.meta;
  const followThreads = followPage?.data?.threads ?? [];
  const followMeta: FeedMeta | null = followPage?.meta
    ? {
        pagination: followPage.meta.pagination ?? 'cursor',
        stream: followPage.meta.stream ?? stream,
        pageSize: followPage.meta.pageSize ?? 20,
        hasMore: Boolean(followPage.meta.hasMore),
        nextCursor: followPage.meta.nextCursor ?? '',
        followingCount: followPage.meta.followingCount ?? 0,
      }
    : null;
  const followMetaSafe: FeedMeta =
    followMeta ?? {
      pagination: 'cursor',
      stream: stream ?? 'forums',
      pageSize: 20,
      hasMore: false,
      nextCursor: '',
      followingCount: -1,
    };

  return (
    <div className={styles.layout}>
      <div className={styles.grid}>
        <ForumNav categories={categories} totalThreads={home?.stats.totalThreads} tags={tagPage?.data ?? []} />

        <div className={styles.center}>
          <section className={styles.hero}>
            <h1 className={styles.heroTitle}>{site?.name ?? 'GoBBS 社区'}</h1>
            <p className={styles.heroSub}>
              分享、交流、共同成长
              {home ? `，已有 ${formatCount(home.stats.members)} 位开发者在这里相遇` : ''}
            </p>
          </section>

          <FeedTabs active={feed} sortNote={feed === 'all' ? '按最近活跃排序' : '按主题发布时间倒序'} />

          <div className={styles.feedBody}>
            {feed !== 'all' ? (
              !loggedIn ? (
                <div className="panel">
                  <EmptyState
                    title="登录后查看关注信息流"
                    description="登录后可查看你关注的版块与用户发布的最新主题。"
                    action={
                      <Link href="/login" className={styles.heroLink}>
                        去登录
                      </Link>
                    }
                  />
                </div>
              ) : !followPage ? (
                <div className="panel">
                  <ErrorState
                    title="关注信息流加载失败"
                    description="无法读取关注信息流，请稍后重试；综合流与版块导航仍可浏览。"
                    retryHref={'/?feed=' + feed}
                  />
                </div>
              ) : followMeta && followMeta.followingCount === 0 ? (
                <div className="panel">
                  <EmptyState
                    title={feed === 'forums' ? '还没有关注的版块' : '还没有关注的用户'}
                    description={
                      feed === 'forums'
                        ? '在版块页点击关注后，这里会汇总该版块的最新主题。'
                        : '在用户主页点击关注后，这里会汇总 TA 作为作者发布的主题（不含回复）。'
                    }
                    action={
                      <Link href={feed === 'forums' ? '/forums' : '/'} className={styles.heroLink}>
                        {feed === 'forums' ? '去版块列表' : '去综合流发现'}
                      </Link>
                    }
                  />
                </div>
              ) : (
                <FollowFeedList
                  // 身份 = 账号 + 流类型 + 首屏游标 + 实际分页大小；任一变化即重新挂载。
                  key={[session.user?.id ?? 'anon', stream ?? 'forums', cursor || '-', followLimit].join(':')}
                  stream={stream ?? 'forums'}
                  threads={followThreads}
                  meta={followMetaSafe}
                  forumNames={forumNames}
                />
              )
            ) : feedData ? (
              <ThreadList
                threads={feedData.threads}
                stickies={feedData.stickies}
                forumNames={forumNames}
                emptyTitle="暂时没有可读主题"
                emptyDescription="还没有公开主题，登录后可以发布第一个主题。"
                variant="home"
              />
            ) : (
              <div className="panel">
                <ErrorState
                  title="信息流加载失败"
                  description="无法读取主题列表，请稍后重试；版块导航与公告仍可浏览。"
                  retryHref="/"
                />
              </div>
            )}
          </div>

          {feed === 'all' && meta ? (
            <div className={styles.paginationWrap}>
              <Pagination page={meta.page} totalPages={meta.totalPages} basePath="/" ariaLabel="信息流分页" />
            </div>
          ) : null}
        </div>

        <HomeSidebar
          announcements={home?.announcements ?? []}
          stats={home?.stats ?? null}
        />
      </div>
    </div>
  );
}
