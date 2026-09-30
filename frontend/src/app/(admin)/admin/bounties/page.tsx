import type { Metadata } from 'next';
import Link from 'next/link';
import { adminGet } from '@/lib/admin.server';
import type { AdminBountyView, BountyRefundDiagnostics } from '@/lib/api/types';
import { BountyAdmin } from '@/components/admin/BountyAdmin';
import styles from './bounties-admin.module.css';

export const metadata: Metadata = {
  title: '悬赏管理',
  robots: { index: false, follow: false },
};

const STATES = ['active', 'awarded', 'canceled', 'expired', 'all'];

export default async function AdminBountiesPage({
  searchParams,
}: {
  searchParams: { state?: string; refundFailed?: string; before?: string };
}) {
  const state = searchParams.state && STATES.includes(searchParams.state) ? searchParams.state : 'active';
  const refundFailed = searchParams.refundFailed === 'true';
  const before = searchParams.before;

  const [list, diagnostics] = await Promise.all([
    adminGet<{ items: AdminBountyView[]; nextBefore: string }>('/api/v1/admin/bounties', {
      state,
      refundFailed: refundFailed ? 'true' : undefined,
      before,
    }),
    adminGet<BountyRefundDiagnostics>('/api/v1/admin/bounties/diagnostics'),
  ]);

  if (!list.ok) {
    return (
      <div className={styles.forbidden}>
        <h1>无访问权限</h1>
        <p>悬赏管理需要 bounty.manage 权限。</p>
        <Link href="/admin">返回概览</Link>
      </div>
    );
  }

  return (
    <div>
      <div className={styles.pageHead}>
        <h1 className={styles.pageTitle}>悬赏管理</h1>
        <span className={styles.pageMeta}>
          只提供列出、查看、重试退款与取消退款；不提供修改余额或直接改状态的表单。
        </span>
      </div>
      <BountyAdmin
        items={list.data.items ?? []}
        nextBefore={list.data.nextBefore ?? ''}
        state={state}
        refundFailed={refundFailed}
        diagnostics={diagnostics.ok ? diagnostics.data : null}
      />
    </div>
  );
}
