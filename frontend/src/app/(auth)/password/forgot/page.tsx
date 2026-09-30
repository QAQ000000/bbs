import type { Metadata } from 'next';
import { ForgotPasswordForm } from '@/components/auth/ForgotPasswordForm';

export const metadata: Metadata = {
  title: '找回密码',
  robots: { index: false, follow: false },
};

export default function ForgotPasswordPage() {
  return <ForgotPasswordForm />;
}
