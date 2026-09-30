import type { Metadata } from 'next';
import Link from 'next/link';
import { safeRequest } from '@/lib/api/server';
import type { NotificationView } from '@/lib/api/types';
import { Pagination } from '@/components/ui/Pagination';
import { EmptyState, ErrorState } from '@/components/ui/StateView';
import { NotificationList } from '@/components/forum/NotificationList';
import { requireMember } from '../../guard';
import styles from '../member-page.module.css';

export const metadata: Metadata = {
  title: '通知中心',
  robots: { index: false, follow: false },
};

function parsePage(value?: string): number {
  const n = Number.parseInt(value ?? '1', 10);
  return Number.isFinite(n) && n > 0 ? n : 1;
}

export default async function NotificationsPage({
  searchParams,
}: {
  searchParams: { page?: string; unread?: string };
}) {
  await requireMember('/me/notifications');
  const page = parsePage(searchParams.page);
  const unreadOnly = searchParams.unread === 'true';
  const envelope = await safeRequest<NotificationView[]>('/api/v1/me/notifications', {
    query: { page, unread: unreadOnly ? 'true' : undefined },
  });
  const items = envelope?.data ?? [];
  const meta = envelope?.meta;

  return (
    <div>
      <div className={styles.header}>
        <h1 className={styles.title}>通知中心</h1>
        <nav className={styles.filters} aria-label="通知筛选">
          <Link
            href="/me/notifications"
            className={unreadOnly ? styles.filter : styles.filterActive}
            aria-current={!unreadOnly ? 'page' : undefined}
          >
            全部
          </Link>
          <Link
            href="/me/notifications?unread=true"
            className={unreadOnly ? styles.filterActive : styles.filter}
            aria-current={unreadOnly ? 'page' : undefined}
          >
            未读
          </Link>
        </nav>
      </div>

      {envelope ? (
        items.length > 0 ? (
          <>
            <NotificationList items={items} />
            <div className={styles.pagination}>
              <Pagination
                page={meta?.page ?? 1}
                totalPages={meta?.totalPages ?? 0}
                basePath="/me/notifications"
                query={{ unread: unreadOnly ? 'true' : undefined }}
                ariaLabel="通知分页"
              />
            </div>
          </>
        ) : (
          <div className="panel">
            <EmptyState
              title={unreadOnly ? '没有未读通知' : '暂无通知'}
              description="当有人回复、提到你或处理你的内容时，会在这里显示。"
            />
          </div>
        )
      ) : (
        <div className="panel">
          <ErrorState title="通知加载失败" description="无法读取通知列表，请稍后重试。" retryHref="/me/notifications" />
        </div>
      )}
    </div>
  );
}
