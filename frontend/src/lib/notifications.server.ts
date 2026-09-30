import { cache } from 'react';
import { isDynamicUsageError } from './api/errors';
import { serverGet } from './api/server';

interface SummaryShape {
  unread?: number;
  total?: number;
}

/** 头部未读徽标：失败时返回 0，不阻塞页面。 */
export const getNotificationUnread = cache(async (): Promise<number> => {
  try {
    const data = await serverGet<SummaryShape>('/api/v1/me/notifications/summary');
    return data?.unread ?? data?.total ?? 0;
  } catch (error) {
    if (isDynamicUsageError(error)) throw error;
    return 0;
  }
});
