import type { Metadata } from 'next';
import { safeGet } from '@/lib/api/server';
import type { CurrentUser, NotificationPreferences } from '@/lib/api/types';
import { ProfileForm, NotificationPreferencesForm } from '@/components/member/SettingsForms';
import { requireMember } from '../../guard';
import styles from '../security/security.module.css';

export const metadata: Metadata = {
  title: '资料设置',
  robots: { index: false, follow: false },
};

const DEFAULT_PREFS: NotificationPreferences = {
  mentions: true,
  replies: true,
  acceptance: true,
  membership: true,
  titles: true,
  moderation: true,
  reports: true,
  email: true,
  subscriptions: true,
};

export default async function SettingsPage() {
  await requireMember('/me/settings');
  const [me, prefs] = await Promise.all([
    safeGet<CurrentUser>('/api/v1/me'),
    safeGet<NotificationPreferences>('/api/v1/me/notification-preferences'),
  ]);

  return (
    <div>
      <h1 className={styles.title}>资料设置</h1>
      <p className={styles.subtitle}>编辑公开资料与通知偏好；保存失败会保留你已输入的内容。</p>
      <section className={['panel', styles.card].join(' ')}>
        <h2 className={styles.cardTitle}>公开资料</h2>
        <ProfileForm signature={me?.signature ?? ''} />
      </section>
      <section className={['panel', styles.card].join(' ')}>
        <h2 className={styles.cardTitle}>通知偏好</h2>
        <NotificationPreferencesForm initial={prefs ?? DEFAULT_PREFS} />
      </section>
    </div>
  );
}
