import type { Metadata } from 'next';
import { redirect } from 'next/navigation';
import { getSession } from '@/lib/auth/server';
import { safeGet } from '@/lib/api/server';
import type { CheckinHistory, CheckinStatus } from '@/lib/api/types';
import { formatDateTime } from '@/lib/format';
import { Breadcrumb } from '@/components/ui/Breadcrumb';
import { CheckinPanel } from '@/components/forum/CheckinPanel';
import { EmptyState } from '@/components/ui/StateView';
import styles from './checkin.module.css';

export const metadata: Metadata = {
  title: '每日签到',
  robots: { index: false, follow: false },
};

export default async function CheckinPage() {
  const session = await getSession();
  if (!session.user) {
    redirect('/login?next=' + encodeURIComponent('/checkin'));
  }
  const [status, history] = await Promise.all([
    safeGet<CheckinStatus>('/api/v1/me/checkin'),
    safeGet<CheckinHistory>('/api/v1/me/checkins'),
  ]);

  return (
    <div className={styles.page}>
      <Breadcrumb items={[{ label: '首页', href: '/' }, { label: '每日签到' }]} />
      <h1 className={styles.title}>每日签到</h1>
      <p className={styles.subtitle}>签到奖励与连续天数以服务端返回为准，重复签到不会重复发放。</p>
      {status ? <CheckinPanel status={status} /> : <div className="panel" style={{ padding: 24 }}>签到状态暂不可用。</div>}
      <section className={['panel', styles.history].join(' ')}>
        <h2 className={styles.historyTitle}>最近记录</h2>
        {history && history.items.length > 0 ? (
          <ul className={styles.list}>
            {history.items.map((item) => (
              <li key={item.day} className={styles.item}>
                <span className={styles.day}>{item.day}</span>
                <span className={styles.itemMeta}>
                  连续 {item.streak} 天 · +{item.points} 积分 · +{item.experience} 经验
                </span>
                <span className={styles.itemTime}>{formatDateTime(item.createdAt)}</span>
              </li>
            ))}
          </ul>
        ) : (
          <EmptyState title="还没有签到记录" description="完成第一次签到后会在这里显示。" />
        )}
      </section>
    </div>
  );
}
