import type { Metadata } from 'next';
import { EmailActionPanel } from '@/components/auth/EmailActionPanel';

export const metadata: Metadata = {
  title: '邮箱验证',
  robots: { index: false, follow: false },
};

export default function VerifyEmailPage({ searchParams }: { searchParams: { token?: string } }) {
  return (
    <div className="container">
      <EmailActionPanel
        endpoint="/auth/email/verify"
        token={searchParams.token ?? ''}
        workingText="正在验证邮箱…"
        okFallback="邮箱验证成功"
      />
    </div>
  );
}
