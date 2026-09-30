import { cache } from 'react';
import { isDynamicUsageError } from './api/errors';
import { serverGet } from './api/server';
import type { SiteView } from './api/types';

export const getSite = cache(async (): Promise<SiteView | null> => {
  try {
    return await serverGet<SiteView>('/api/v1/site');
  } catch (error) {
    if (isDynamicUsageError(error)) throw error;
    return null;
  }
});
