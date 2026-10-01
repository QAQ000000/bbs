import type { Metadata } from 'next';
import Link from 'next/link';
import { safeRequest } from '@/lib/api/server';
import type { SubscriptionView } from '@/lib/api/types';
import { formatDateTime } from '@/lib/format';
import { Pagination } from '@/components/ui/Pagination';
import { EmptyState, ErrorState } from '@/components/ui/StateView';
import { UnsubscribeButton } from '@/components/forum/UnsubscribeButton';
import { requireMember } from '../../guard';
import styles from '../member-page.module.css';
import list from './subscriptions.module.css';

export const metadata: Metadata = {
  title: '我的订阅',
  robots: { index: false, follow: false },
};

const KINDS = [
  { value: 'thread', label: '主题' },
  { value: 'forum', label: '版块' },
  { value: 'tag', label: '标签' },
];

function parsePage(value?: string): number {
  const n = Number.parseInt(value ?? '1', 10);
  return Number.isFinite(n) && n > 0 ? n : 1;
}

function targetHref(item: SubscriptionView): string {
  if (item.kind === 'thread') return '/threads/' + item.targetId;
  if (item.kind === 'tag') return '/tags/' + item.targetId;
  return '/forums/' + item.targetId;
}

export default async function SubscriptionsPage({
  searchParams,
}: {
  searchParams: { kind?: string; page?: string };
}) {
  await requireMember('/me/subscriptions');
  const kind = KINDS.some((item) => item.value === searchParams.kind) ? (searchParams.kind as string) : 'thread';
  const page = parsePage(searchParams.page);
  const envelope = await safeRequest<SubscriptionView[]>('/api/v1/me/subscriptions', {
    query: { kind, page },
  });
  const items = envelope?.data ?? [];

  return (
    <div>
      <div className={styles.header}>
        <h1 className={styles.title}>我的订阅</h1>
        <nav className={styles.filters} aria-label="订阅类型">
          {KINDS.map((item) => (
            <Link
              key={item.value}
              href={'/me/subscriptions?kind=' + item.value}
              className={item.value === kind ? styles.filterActive : styles.filter}
              aria-current={item.value === kind ? 'page' : undefined}
            >
              {item.label}
            </Link>
          ))}
        </nav>
      </div>
      <p className={styles.subtitle}>新订阅默认开启通知；关闭通知或静音不会取消订阅，取消订阅才会移除关系。</p>
      {envelope ? (
        items.length > 0 ? (
          <div className={styles.panel}>
            <ul className={list.items}>
              {items.map((item) => (
                <li key={item.kind + item.targetId} className={list.item}>
                  <div className={list.body}>
                    <Link className={list.name} href={targetHref(item)}>
                      {item.name || item.kind + ' #' + item.targetId}
                    </Link>
                    <div className={list.badges}>
                      <span className={item.enabled ? list.on : list.off}>{item.enabled ? '已启用' : '已关闭'}</span>
                      <span className={item.notifyInApp ? list.on : list.off}>站内</span>
                      <span className={item.notifyEmail ? list.on : list.off}>邮件</span>
                      <span className={list.meta}>
                        {item.mutedUntil ? '静音至 ' + formatDateTime(item.mutedUntil) : '未静音'}
                      </span>
                    </div>
                  </div>
                  <UnsubscribeButton kind={item.kind} targetId={item.targetId} />
                </li>
              ))}
            </ul>
          </div>
        ) : (
          <div className={styles.panel}>
            <EmptyState title="没有订阅" description="在版块、主题或标签页点击「关注」即可订阅。" />
          </div>
        )
      ) : (
        <div className={styles.panel}>
          <ErrorState title="订阅加载失败" description="无法读取订阅列表，请稍后重试。" retryHref="/me/subscriptions" />
        </div>
      )}
      <div className={styles.pagination}>
        <Pagination
          page={envelope?.meta?.page ?? 1}
          totalPages={envelope?.meta?.totalPages ?? 0}
          basePath="/me/subscriptions"
          query={{ kind }}
          ariaLabel="订阅分页"
        />
      </div>
    </div>
  );
}
