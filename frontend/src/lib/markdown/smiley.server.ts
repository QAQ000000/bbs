import { cache } from 'react';
import { isDynamicUsageError } from '../api/errors';
import { serverGet } from '../api/server';
import type { SmileyGroup } from '../api/types';
import { parseSmileyGroups, type SmileyMap } from './smiley';

/** 同一请求内复用，避免每次渲染正文都请求表情目录。 */
export const getSmileyMap = cache(async (): Promise<SmileyMap> => {
  try {
    const groups = await serverGet<SmileyGroup[]>('/api/v1/smileys');
    return parseSmileyGroups(groups);
  } catch (error) {
    if (isDynamicUsageError(error)) throw error;
    return {};
  }
});
