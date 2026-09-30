import { cache } from 'react';
import { isDynamicUsageError } from '../api/errors';
import { serverGet } from '../api/server';
import type { SessionView } from '../api/types';

const EMPTY_SESSION: SessionView = { user: null, csrfToken: '', setupRequired: false };

/** 转发当前请求 Cookie 读取会话，用于 SSR 首屏渲染登录态与能力；同一请求内复用。 */
export const getSession = cache(async (): Promise<SessionView> => {
  try {
    return await serverGet<SessionView>('/api/v1/session');
  } catch (error) {
    if (isDynamicUsageError(error)) throw error;
    return EMPTY_SESSION;
  }
});

export function isLoggedIn(session: SessionView): boolean {
  return Boolean(session.user);
}
