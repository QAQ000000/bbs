import type { Metadata } from 'next';
import { redirect } from 'next/navigation';
import { getSession } from '@/lib/auth/server';
import { getSite } from '@/lib/site.server';
import { RegisterForm } from '@/components/auth/RegisterForm';

export const metadata: Metadata = {
  title: '注册',
  description: '注册 GoBBS 社区账号，加入技术交流与经验分享。',
  robots: { index: false, follow: false },
};

export default async function RegisterPage() {
  const [session, site] = await Promise.all([getSession(), getSite()]);
  if (session.user) redirect('/');
  if (site && !site.registerEnabled) {
    return (
      <div>
        <h1 style={{ margin: '0 0 8px', fontSize: 22 }}>注册已关闭</h1>
        <p style={{ margin: 0, color: 'var(--color-text-tertiary)', fontSize: 14 }}>
          本站暂时关闭新用户注册，请联系管理员。
        </p>
      </div>
    );
  }
  return (
    <RegisterForm
      captchaEnabled={Boolean(site?.captchaEnabled)}
      requireConsent={Boolean(site?.requireConsent)}
    />
  );
}
