import type { Metadata } from 'next';
import { EmailActionPanel } from '@/components/auth/EmailActionPanel';
import { requireMember } from '../../../guard';

export const metadata: Metadata = {
  title: '确认更换邮箱',
  robots: { index: false, follow: false },
};

export default async function ConfirmEmailChangePage({
  searchParams,
}: {
  searchParams: { token?: string };
}) {
  await requireMember('/settings/email/confirm');
  return (
    <EmailActionPanel
      endpoint="/me/email/confirm"
      token={searchParams.token ?? ''}
      workingText="正在确认新邮箱…"
      okFallback="邮箱已变更并验证，其他设备已退出登录"
    />
  );
}
