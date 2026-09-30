import type { Metadata } from 'next';
import Link from 'next/link';
import { safeRequest } from '@/lib/api/server';
import type { FollowUserView } from '@/lib/api/types';
import { formatRelative } from '@/lib/format';
import { Avatar } from '@/components/ui/Avatar';
import { FollowButton } from '@/components/forum/FollowButton';
import { Pagination } from '@/components/ui/Pagination';
import { EmptyState, ErrorState } from '@/components/ui/StateView';
import { requireMember } from '../../guard';
import styles from '../member-page.module.css';
import list from './following.module.css';

export const metadata: Metadata = {
  title: '关注与粉丝',
  robots: { index: false, follow: false },
};

function parsePage(value?: string): number {
  const n = Number.parseInt(value ?? '1', 10);
  return Number.isFinite(n) && n > 0 ? n : 1;
}

export default async function FollowingPage({
  searchParams,
}: {
  searchParams: { page?: string; tab?: string };
}) {
  const session = await requireMember('/me/following');
  const page = parsePage(searchParams.page);
  const tab = searchParams.tab === 'followers' ? 'followers' : 'following';
  const envelope = await safeRequest<FollowUserView[]>(
    tab === 'followers' ? '/api/v1/me/followers' : '/api/v1/me/following',
    { query: { page } },
  );
  const items = envelope?.data ?? [];

  return (
    <div>
      <div className={styles.header}>
        <h1 className={styles.title}>关注与粉丝</h1>
        <nav className={styles.filters} aria-label="关注列表切换">
          <Link
            href="/me/following"
            className={tab === 'following' ? styles.filterActive : styles.filter}
            aria-current={tab === 'following' ? 'page' : undefined}
          >
            我关注的
          </Link>
          <Link
            href="/me/following?tab=followers"
            className={tab === 'followers' ? styles.filterActive : styles.filter}
            aria-current={tab === 'followers' ? 'page' : undefined}
          >
            关注我的
          </Link>
        </nav>
      </div>
      {envelope ? (
        items.length > 0 ? (
          <div className="panel panel-flush">
            <ul className={list.items}>
              {items.map((user) => (
                <li key={user.id} className={list.item}>
                  <Avatar userId={user.id} name={user.username} size={40} href={'/users/' + user.id} />
                  <div className={list.body}>
                    <Link className={list.name} href={'/users/' + user.id}>
                      {user.username}
                    </Link>
                    <span className={list.meta}>关注于 {formatRelative(user.followedAt)}</span>
                  </div>
                  {user.id !== session.user.id ? (
                    <FollowButton
                      userId={user.id}
                      size="small"
                      initialFollowing={user.following ?? null}
                    />
                  ) : null}
                </li>
              ))}
            </ul>
          </div>
        ) : (
          <div className="panel">
            <EmptyState
              title={tab === 'followers' ? '还没有粉丝' : '还没有关注任何人'}
              description="在主题或用户主页点击「加关注」。"
            />
          </div>
        )
      ) : (
        <div className="panel">
          <ErrorState title="列表加载失败" description="无法读取关注列表，请稍后重试。" retryHref="/me/following" />
        </div>
      )}
      <div className={styles.pagination}>
        <Pagination
          page={envelope?.meta?.page ?? 1}
          totalPages={envelope?.meta?.totalPages ?? 0}
          basePath="/me/following"
          query={{ tab: tab === 'followers' ? 'followers' : undefined }}
          ariaLabel="关注分页"
        />
      </div>
    </div>
  );
}
