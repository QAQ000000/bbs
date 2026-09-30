import type { ThreadSummary } from './api/types';

export type FlagTone = 'danger' | 'warning' | 'primary' | 'success' | 'neutral';

export interface ThreadFlag {
  key: string;
  label: string;
  tone: FlagTone;
}

/** 依据后端字段派生展示标签；不臆造后端未返回的状态。 */
export function threadFlags(thread: ThreadSummary): ThreadFlag[] {
  const flags: ThreadFlag[] = [];
  if (thread.sticky > 0) flags.push({ key: 'sticky', label: '置顶', tone: 'danger' });
  if (thread.digest) flags.push({ key: 'digest', label: '精华', tone: 'warning' });
  if (thread.poll) flags.push({ key: 'poll', label: '投票', tone: 'primary' });
  if (thread.bounty) flags.push({ key: 'bounty', label: '悬赏', tone: 'warning' });
  if (thread.closed) flags.push({ key: 'closed', label: '已关闭', tone: 'neutral' });
  if (thread.pending) flags.push({ key: 'pending', label: '待审核', tone: 'neutral' });
  return flags;
}

export interface SortOption {
  value: string;
  label: string;
  hint?: string;
}

// 与 store.Threads 支持的 sort 参数一一对应；空值为默认（最后回复）。
export const FORUM_SORTS: SortOption[] = [
  { value: '', label: '最近回复' },
  { value: 'new', label: '最新发布' },
  { value: 'digest', label: '精华' },
  { value: 'hot', label: '热门', hint: '按查看数' },
];

export function sortLabel(sort?: string): string {
  const found = FORUM_SORTS.find((s) => s.value === (sort ?? ''));
  return found ? found.label : '最近回复';
}

export function forumNameMap(categories: { forums: { id: string; name: string }[] }[]): Record<string, string> {
  const map: Record<string, string> = {};
  for (const category of categories) {
    for (const forum of category.forums) {
      map[forum.id] = forum.name;
    }
  }
  return map;
}
