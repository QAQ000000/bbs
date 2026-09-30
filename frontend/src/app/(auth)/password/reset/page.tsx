import type { Metadata } from 'next';
import { ResetPasswordForm } from '@/components/auth/ResetPasswordForm';

export const metadata: Metadata = {
  title: '重置密码',
  robots: { index: false, follow: false },
};

export default function ResetPasswordPage({ searchParams }: { searchParams: { token?: string } }) {
  return <ResetPasswordForm token={searchParams.token ?? ''} />;
}
