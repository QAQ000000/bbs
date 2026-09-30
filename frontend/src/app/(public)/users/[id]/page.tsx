import type { Metadata } from 'next';
import Link from 'next/link';
import { notFound } from 'next/navigation';
import { loadUserProfilePage } from '@/lib/data.server';
import { getSession } from '@/lib/auth/server';
import { formatCount, formatDateTime } from '@/lib/format';
import { Avatar } from '@/components/ui/Avatar';
import { LevelBadge, TitleBadge } from '@/components/ui/LevelBadge';
import { ThreadList } from '@/components/forum/ThreadList';
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
    <div className="container page">
      <section className={['panel', styles.header].join(' ')}>
        <Avatar userId={user.id} name={user.username} size={72} />
        <div className={styles.body}>
          <h1 className={styles.name}>
            {user.username}
            <LevelBadge level={user.level} />
            <TitleBadge title={user.equippedTitle} />
          </h1>
          <p className={styles.signature}>{user.signature || '还没有填写签名'}</p>
          <p className={styles.meta}>
            <span>{formatCount(profile.data.reputation.posts)} 帖</span>
            <span className={styles.dotSep}>·</span>
            <span>{formatCount(profile.data.reputation.likes)} 获赞</span>
            <span className={styles.dotSep}>·</span>
            <span>注册于 {formatDateTime(user.createdAt)}</span>
          </p>
        </div>
        <div className={styles.actions}>
          {isSelf ? (
            <Link className={styles.selfLink} href="/me">
              我的主页
            </Link>
          ) : session.user ? (
            <FollowButton userId={user.id} initialFollowing={profile.data.following ?? null} />
          ) : (
            <Link className={styles.selfLink} href="/login">
              登录后关注
            </Link>
          )}
        </div>
      </section>

      <div className={styles.tabs}>
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
      </div>

      {threads.length > 0 ? (
        <ThreadList threads={threads} emptyTitle="暂无内容" />
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
  );
}
