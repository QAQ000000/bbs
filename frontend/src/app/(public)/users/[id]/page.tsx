import type { Metadata } from 'next';
import Link from 'next/link';
import { notFound } from 'next/navigation';
import { loadUserProfilePage } from '@/lib/data.server';
import { getSession } from '@/lib/auth/server';
import { formatCount, formatDateTime, formatRelative } from '@/lib/format';
import { Avatar } from '@/components/ui/Avatar';
import { LevelBadge, TitleBadge } from '@/components/ui/LevelBadge';
import { FollowButton } from '@/components/forum/FollowButton';
import { Pagination } from '@/components/ui/Pagination';
import { EmptyState, ErrorState } from '@/components/ui/StateView';
import styles from './user.module.css';

function parsePage(value?: string): number {
  const n = Number.parseInt(value ?? '1', 10);
  return Number.isFinite(n) && n > 0 ? n : 1;
}

export async function generateMetadata({ params }: { params: { id: string } }): Promise<Metadata> {
  const loaded = await loadUserProfilePage(params.id, 'threads', 1);
  if (loaded.status === 'notfound') return { title: '用户不存在' };
  if (loaded.status === 'error') return { title: '用户主页' };
  const username = loaded.value.data.user.username;
  return {
    title: username,
    description: username + ' 在 GoBBS 的公开主页',
    alternates: { canonical: '/users/' + params.id },
  };
}

export default async function UserProfilePage({
  params,
  searchParams,
}: {
  params: { id: string };
  searchParams: { tab?: string; page?: string };
}) {
  const tab = searchParams.tab === 'replies' ? 'replies' : 'threads';
  const page = parsePage(searchParams.page);
  const loaded = await loadUserProfilePage(params.id, tab, page);

  // 只有真实 404 才返回“用户不存在”；接口故障渲染可重试的失败状态。
  if (loaded.status === 'notfound') notFound();
  if (loaded.status === 'error') {
    return (
      <div className="container page">
        <div className="panel">
          <ErrorState
            title="用户主页加载失败"
            description="无法读取该用户的公开资料，请稍后重试；这不代表用户不存在。"
            retryHref={'/users/' + params.id}
          />
        </div>
      </div>
    );
  }

  const profile = loaded.value;
  const session = await getSession();
  const user = profile.data.user;
  const threads = profile.data.threads;
  const meta = profile.meta;
  const isSelf = session.user?.id === user.id;

  return (
    <div className={styles.page}>
      <section className={styles.header}>
        <Avatar userId={user.id} name={user.username} size={72} className={styles.avatar} />
        <div className={styles.body}>
          <h1 className={styles.name}>
            {user.username}
            <LevelBadge level={user.level} />
            <TitleBadge title={user.equippedTitle} />
          </h1>
          <p className={styles.signature}>{user.signature || '还没有填写签名'}</p>
        </div>
        <div className={styles.actions}>
          {isSelf ? (
            <Link className={styles.selfLink} href="/me">
              我的主页
            </Link>
          ) : session.user ? (
            <div className={styles.actionGroup}>
              {!isSelf ? <Link className={styles.selfLink} href={'/me/messages?to=' + encodeURIComponent(user.id)}>发私信</Link> : null}
              <FollowButton
                key={[session.user?.id, user.id, profile.data.following ?? 'unknown'].join(':')}
                userId={user.id}
                initialFollowing={profile.data.following ?? null}
              />
            </div>
          ) : (
            <div className={styles.actionGroup}>
              <Link className={styles.selfLink} href={'/login?next=' + encodeURIComponent('/me/messages?to=' + user.id)}>登录后私信</Link>
              <Link className={styles.selfLink} href="/login">登录后关注</Link>
            </div>
          )}
        </div>
      </section>

      <dl className={styles.stats}>
        <div>
          <dd>{formatCount(profile.data.reputation.posts)}</dd>
          <dt>发帖</dt>
        </div>
        <div>
          <dd>{formatCount(profile.data.reputation.likes)}</dd>
          <dt>获赞</dt>
        </div>
      </dl>

      <div className={styles.columns}>
        <div className={styles.content}>
          <nav className={styles.tabs} aria-label="用户公开内容">
            <Link
              href={'/users/' + user.id}
              className={tab === 'threads' ? styles.tabActive : styles.tab}
              aria-current={tab === 'threads' ? 'page' : undefined}
            >
              最近主题
            </Link>
            <Link
              href={'/users/' + user.id + '?tab=replies'}
              className={tab === 'replies' ? styles.tabActive : styles.tab}
              aria-current={tab === 'replies' ? 'page' : undefined}
            >
              最近回复
            </Link>
          </nav>

          {threads.length > 0 ? (
            <section className={styles.threadSection} aria-label={tab === 'replies' ? '最近回复' : '最近主题'}>
              {threads.map(thread => (
                <article key={thread.id} className={styles.threadRow}>
                  <Link href={'/threads/' + thread.id} className={styles.threadTitle}>
                    {thread.title}
                  </Link>
                  <div className={styles.threadMeta}>
                    <Link href={'/forums/' + thread.forumId}>查看版块</Link>
                    <span>·</span>
                    <time dateTime={thread.createdAt}>{formatRelative(thread.createdAt)}</time>
                    <span>·</span>
                    <span>{formatCount(thread.postCount)} 回复</span>
                  </div>
                </article>
              ))}
            </section>
          ) : (
            <div className="panel">
              <EmptyState title="暂无公开内容" description="该用户还没有发布公开主题或回复。" />
            </div>
          )}

          {meta ? (
            <div className={styles.pagination}>
              <Pagination
                page={meta.page}
                totalPages={meta.totalPages}
                basePath={'/users/' + user.id}
                query={{ tab: tab === 'replies' ? 'replies' : undefined }}
                ariaLabel="用户内容分页"
              />
            </div>
          ) : null}
        </div>
        <aside className={styles.details}>
          <h2>资料</h2>
          <dl>
            <dt>注册时间</dt>
            <dd>{formatDateTime(user.createdAt)}</dd>
          </dl>
          {user.equippedTitle ? (
            <div className={styles.titleDetail}>
              <h3>佩戴称号</h3>
              <TitleBadge title={user.equippedTitle} />
            </div>
          ) : null}
        </aside>
      </div>
    </div>
  );
}
