import { cache } from 'react';
import { ApiError, isDynamicUsageError } from './api/errors';
import { safeGet, serverGet, serverRequest, safeRequest } from './api/server';
import type {
  BountyView,
  EngagementRules,
  LeaderboardView,
  ForumDetail,
  ListEnvelope,
  PointsAccount,
  PollView,
  PostView,
  TagView,
  ThreadListData,
  ThreadSummary,
  UserProfileView,
} from './api/types';

/**
 * 三态取数结果：真实 404、接口失败、成功互相区分。
 * 页面只在 notfound 时调用 notFound()，error 时渲染可重试的失败状态。
 */
export type Loaded<T> = { status: 'ok'; value: T } | { status: 'notfound' } | { status: 'error' };

/** 只把真实 404 当作“不存在”，其余错误照常抛出，由错误边界返回 5xx 语义。 */
async function getOr404<T>(path: string): Promise<T | null> {
  try {
    return await serverGet<T>(path);
  } catch (error) {
    if (isDynamicUsageError(error)) throw error;
    if (error instanceof ApiError && error.status === 404) return null;
    throw error;
  }
}

/** 需要区分 404 与接口故障时使用，不抛出非 404 错误。 */
async function loadOr404<T>(
  path: string,
  query?: Record<string, string | number | undefined>,
): Promise<Loaded<ListEnvelope<T>>> {
  try {
    return { status: 'ok', value: await serverRequest<T>(path, { query }) };
  } catch (error) {
    if (isDynamicUsageError(error)) throw error;
    if (error instanceof ApiError && error.status === 404) return { status: 'notfound' };
    return { status: 'error' };
  }
}

// 同一请求内按参数复用，避免 generateMetadata 与页面重复取数。
export const getForum = cache(async (fid: string): Promise<ForumDetail | null> =>
  getOr404<ForumDetail>('/api/v1/forums/' + fid),
);

export const getThread = cache(async (tid: string): Promise<ThreadSummary | null> =>
  getOr404<ThreadSummary>('/api/v1/threads/' + tid),
);

export const getForumThreads = cache(
  async (fid: string, page: number, sort: string): Promise<ListEnvelope<ThreadListData> | null> =>
    safeRequest<ThreadListData>('/api/v1/threads', { query: { forumId: fid, page, sort } }),
);

export const getPost = cache(async (pid: string): Promise<PostView | null> =>
  getOr404<PostView>('/api/v1/posts/' + pid),
);

export const getPosts = cache(
  async (tid: string, page: number): Promise<ListEnvelope<PostView[]> | null> =>
    safeRequest<PostView[]>('/api/v1/threads/' + tid + '/posts', { query: { page } }),
);

/** 主题详情侧栏用：失败时返回 null，仅隐藏辅助卡片。 */
export const getUserProfile = cache(async (uid: string): Promise<UserProfileView | null> =>
  safeGet<UserProfileView>('/api/v1/users/' + uid),
);

/** 公开用户主页：区分不存在与加载失败。 */
export const loadUserProfilePage = cache(
  async (uid: string, tab: string, page: number): Promise<Loaded<ListEnvelope<UserProfileView>>> =>
    loadOr404<UserProfileView>('/api/v1/users/' + uid, { tab, page }),
);

// ---- 互动：投票 / 悬赏 / 积分 ----

export const getEngagementRules = cache(async (): Promise<EngagementRules | null> =>
  safeGet<EngagementRules>('/api/v1/engagement/rules'),
);

export const getPoll = cache(async (tid: string): Promise<PollView | null> =>
  safeGet<PollView>('/api/v1/threads/' + tid + '/poll'),
);

export const getBounty = cache(async (tid: string): Promise<BountyView | null> =>
  safeGet<BountyView>('/api/v1/threads/' + tid + '/bounty'),
);

/** 仅登录后有意义；未登录或失败返回 null。 */
export const getPointsAccount = cache(async (): Promise<PointsAccount | null> =>
  safeGet<PointsAccount>('/api/v1/me/points'),
);

// ---- 公开排行榜：只读 Worker 快照 ----

/** 读取公开积分余额榜快照；失败返回 null，页面区分“快照缺失/过期”与请求失败。 */
export const getPointsLeaderboard = cache(async (): Promise<LeaderboardView | null> =>
  safeGet<LeaderboardView>('/api/v1/leaderboard/points'),
);

export const getTag = cache(async (slug: string): Promise<TagView | null> => {
  const path = /^[0-9]+$/.test(slug) ? '/api/v1/tags/' + slug : '/api/v1/tag-slugs/' + encodeURIComponent(slug);
  return getOr404<TagView>(path);
});
