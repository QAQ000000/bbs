'use client';

import { useEffect, useRef, useState } from 'react';
import { Button } from '@arco-design/web-react';
import { browserRequest } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { FeedMeta, FollowFeedData, ThreadSummary } from '@/lib/api/types';
import { ThreadList } from './ThreadList';
import styles from './FollowFeedList.module.css';

export interface FollowFeedListProps {
  stream: 'forums' | 'users';
  threads: ThreadSummary[];
  meta: FeedMeta;
  forumNames: Record<string, string>;
}

interface FeedState {
  identity: string;
  items: ThreadSummary[];
  cursor: string;
  hasMore: boolean;
}

/**
 * 关注聚合流：首屏由 SSR 渲染，加载更多在浏览器侧按游标追加并按 ID 去重。
 *
 * 组件身份 = 流类型 + 首屏游标 + 分页大小：父级 key 也包含这些条件，
 * 切换标签即重新挂载；组件内再做一层兜底，属性变化时同步重置列表，
 * 避免把上一种流的 items / cursor 带到新流（旧游标会被后端判为 INVALID_CURSOR）。
 */
export function FollowFeedList({ stream, threads, meta, forumNames }: FollowFeedListProps) {
  const identity = [stream, meta.nextCursor, meta.pageSize].join('|');
  const [state, setState] = useState<FeedState>(() => ({
    identity,
    items: threads,
    cursor: meta.nextCursor,
    hasMore: meta.hasMore,
  }));
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const requestIdRef = useRef(0);

  if (state.identity !== identity) {
    // React 的“props 变化时调整 state”模式：渲染期同步重置，不用等 effect。
    setState({ identity, items: threads, cursor: meta.nextCursor, hasMore: meta.hasMore });
    setLoading(false);
    setError(null);
  }

  useEffect(() => {
    return () => {
      // 卸载即作废在途请求，防止旧流的结果被追加到新流。
      requestIdRef.current += 1;
      abortRef.current?.abort();
    };
  }, []);

  async function loadMore() {
    if (!state.cursor || loading) return;
    const requestId = requestIdRef.current + 1;
    requestIdRef.current = requestId;
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    setLoading(true);
    setError(null);
    try {
      const page = await browserRequest<FollowFeedData>('/me/feed/' + stream, {
        query: { cursor: state.cursor, limit: meta.pageSize },
        signal: controller.signal,
      });
      // 已有更新的请求或组件已切换 / 卸载：丢弃这次结果。
      if (requestId !== requestIdRef.current) return;
      setState((prev) => {
        const seen = new Set(prev.items.map((item) => item.id));
        return {
          ...prev,
          items: prev.items.concat((page.data?.threads ?? []).filter((item) => !seen.has(item.id))),
          cursor: page.meta?.nextCursor ?? '',
          hasMore: Boolean(page.meta?.hasMore),
        };
      });
    } catch (caught) {
      if (controller.signal.aborted || requestId !== requestIdRef.current) return;
      const err = caught as ApiError;
      setError(err.status === 401 ? '登录状态已变化，请刷新页面后重试。' : err.message);
    } finally {
      if (requestId === requestIdRef.current) setLoading(false);
    }
  }

  return (
    <div>
      <ThreadList
        threads={state.items}
        forumNames={forumNames}
        emptyTitle="暂无内容"
        emptyDescription="关注的版块或用户还没有发布新主题。"
        variant="home"
      />
      {error ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : null}
      <div className={styles.footer}>
        {state.hasMore ? (
          <Button size="small" type="secondary" loading={loading} onClick={() => void loadMore()}>
            加载更多
          </Button>
        ) : (
          <span className={styles.done}>已经到底了</span>
        )}
        <a className={styles.refresh} href={'/?feed=' + (stream === 'forums' ? 'forums' : 'people')}>
          回到最新
        </a>
      </div>
    </div>
  );
}
