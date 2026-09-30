import { redirect } from 'next/navigation';
import { getSession } from '@/lib/auth/server';
import { AdminShell } from '@/components/admin/AdminShell';

export default async function AdminLayout({ children }: { children: React.ReactNode }) {
  const session = await getSession();
  if (!session.user) {
    redirect('/login?next=' + encodeURIComponent('/admin'));
  }
  return <AdminShell username={session.user.username}>{children}</AdminShell>;
}
