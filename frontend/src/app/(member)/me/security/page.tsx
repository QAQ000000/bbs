import type { Metadata } from 'next';
import { safeGet } from '@/lib/api/server';
import { getSite } from '@/lib/site.server';
import type { CurrentUser, MfaStatus, SessionList } from '@/lib/api/types';
import { EmailPanel } from '@/components/member/EmailPanel';
import { PasswordForm } from '@/components/member/PasswordForm';
import { SessionManager } from '@/components/member/SessionManager';
import { TwoFactorPanel } from '@/components/member/TwoFactorPanel';
import { requireMember } from '../../guard';
import styles from './security.module.css';

export const metadata: Metadata = {
  title: '安全设置',
  robots: { index: false, follow: false },
};

export default async function SecurityPage() {
  await requireMember('/me/security');
  const [sessions, mfa, me, site] = await Promise.all([
    safeGet<SessionList>('/api/v1/me/sessions'),
    safeGet<MfaStatus>('/api/v1/me/2fa'),
    safeGet<CurrentUser>('/api/v1/me'),
    getSite(),
  ]);

  return (
    <div>
      <h1 className={styles.title}>安全设置</h1>
      <p className={styles.subtitle}>密码、两步验证与登录设备。高危操作需要重新验证当前密码。</p>

      <section className={styles.card}>
        <h2 className={styles.cardTitle}>邮箱</h2>
        <EmailPanel
          email={me?.email ?? ''}
          emailVerified={Boolean(me?.emailVerified)}
          emailGateEnabled={Boolean(site?.emailVerificationRequired)}
          mfaEnabled={Boolean(mfa?.enabled)}
        />
      </section>

      <section className={styles.card}>
        <h2 className={styles.cardTitle}>修改密码</h2>
        <PasswordForm />
      </section>

      <section className={styles.card}>
        <h2 className={styles.cardTitle}>两步验证</h2>
        {mfa ? <TwoFactorPanel status={mfa} /> : <p className={styles.empty}>两步验证状态暂不可用。</p>}
      </section>

      <section className={styles.card}>
        <h2 className={styles.cardTitle}>登录设备</h2>
        {sessions ? <SessionManager sessions={sessions} /> : <p className={styles.empty}>设备列表暂不可用。</p>}
      </section>
    </div>
  );
}
