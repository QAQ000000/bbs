import type { Metadata } from 'next';
import Link from 'next/link';
import { adminGet } from '@/lib/admin.server';
import type { EmailJobView } from '@/lib/api/types';
import { EmailQueueAdmin } from '@/components/admin/EmailQueueAdmin';
import styles from './notifications-admin.module.css';

export const metadata: Metadata = {
  title: '邮件与通知队列',
  robots: { index: false, follow: false },
};

const STATUSES = ['pending', 'sending', 'sent', 'dead', 'cancelled'];

export default async function AdminNotificationsPage({
  searchParams,
}: {
  searchParams: { status?: string; before?: string };
}) {
  const status = searchParams.status && STATUSES.includes(searchParams.status) ? searchParams.status : '';
  const result = await adminGet<{
    items: EmailJobView[];
    counts: Record<string, number>;
    nextBefore: string;
    smtpEnabled: boolean;
  }>('/api/v1/admin/email-jobs', { status: status || undefined, before: searchParams.before });

  if (!result.ok) {
    return (
      <div className={styles.forbidden}>
        <h1>无访问权限</h1>
        <p>邮件队列需要 email.manage 权限。</p>
        <Link href="/admin">返回概览</Link>
      </div>
    );
  }

  return (
    <div>
      <div className={styles.pageHead}>
        <h1 className={styles.pageTitle}>邮件与通知队列</h1>
        <span className={styles.pageMeta}>
          重试仅适用于未过期的失败任务。待发送、失败任务可取消；发送中的任务无法撤回。
        </span>
      </div>
      <EmailQueueAdmin
        items={result.data.items ?? []}
        counts={result.data.counts ?? {}}
        nextBefore={result.data.nextBefore ?? ''}
        status={status}
        smtpEnabled={Boolean(result.data.smtpEnabled)}
      />
    </div>
  );
}
