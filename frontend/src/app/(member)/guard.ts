import { redirect } from 'next/navigation';
import { getSession } from '@/lib/auth/server';
import type { SessionView } from '@/lib/api/types';

/** 用户中心页面守卫：未登录跳转登录并回跳当前路径。 */
export async function requireMember(path: string): Promise<SessionView & { user: NonNullable<SessionView['user']> }> {
  const session = await getSession();
  if (!session.user) {
    redirect('/login?next=' + encodeURIComponent(path));
  }
  return session as SessionView & { user: NonNullable<SessionView['user']> };
}
