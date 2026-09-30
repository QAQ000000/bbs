import type { Metadata } from 'next';
import Link from 'next/link';
import { adminGet } from '@/lib/admin.server';
import type { PollModerationItem } from '@/lib/api/types';
import { PollAdmin } from '@/components/admin/PollAdmin';
import styles from './polls-admin.module.css';

export const metadata: Metadata = {
  title: '投票管理',
  robots: { index: false, follow: false },
};

export default async function AdminPollsPage() {
  const result = await adminGet<{ items: PollModerationItem[]; nextBefore: string }>('/api/v1/admin/polls');

  if (!result.ok) {
    return (
      <div className={styles.forbidden}>
        <h1>无访问权限</h1>
        <p>投票管理需要 poll.manage 权限。</p>
        <Link href="/admin">返回概览</Link>
      </div>
    );
  }

  return (
    <div>
      <div className={styles.pageHead}>
        <h1 className={styles.pageTitle}>投票管理</h1>
        <span className={styles.pageMeta}>
          接口只暴露待审队列；已发布 / 已结束的投票通过主题页查看，后台不提供改票、改选项或改计票。
        </span>
      </div>
      <PollAdmin items={result.data.items ?? []} nextBefore={result.data.nextBefore ?? ''} />
    </div>
  );
}
