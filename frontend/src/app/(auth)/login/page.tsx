import type { Metadata } from 'next';
import { redirect } from 'next/navigation';
import { getSession } from '@/lib/auth/server';
import { getSite } from '@/lib/site.server';
import { LoginForm } from '@/components/auth/LoginForm';

export const metadata: Metadata = {
  title: '登录',
  description: '登录 GoBBS 社区，继续你的交流与分享。',
  robots: { index: false, follow: false },
};

export default async function LoginPage({ searchParams }: { searchParams: { next?: string } }) {
  const [session, site] = await Promise.all([getSession(), getSite()]);
  const next =
    searchParams.next && searchParams.next.startsWith('/') && !searchParams.next.startsWith('//')
      ? searchParams.next
      : '/';
  if (session.user) redirect(next);
  return <LoginForm next={next} registerEnabled={site?.registerEnabled ?? true} />;
}
