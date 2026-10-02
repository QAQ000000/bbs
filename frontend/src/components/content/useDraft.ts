'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { browserGet, browserSend } from '@/lib/api/browser';
import { toastError } from '../ui/feedback';

export interface DraftData {
  context: string;
  subject: string;
  content: string;
  updatedAt: string;
}

/**
 * 服务端草稿：登录后读取 / 保存 / 清除。
 * 返回值只用于首次恢复，不参与每次输入渲染，避免受控抖动。
 */
export function useServerDraft(context: string, enabled: boolean) {
  const [restored, setRestored] = useState<DraftData | null>(null);
  const restoredRef = useRef(false);
  const saveFailureNotified = useRef(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (!enabled || restoredRef.current) return;
    let cancelled = false;
    browserGet<DraftData>('/me/draft', { query: { context } })
      .then((draft) => {
        if (!cancelled && draft) setRestored(draft);
      })
      .catch(() => {
        // 草稿读取失败不影响编辑。
      })
      .finally(() => {
        restoredRef.current = true;
      });
    return () => {
      cancelled = true;
    };
  }, [context, enabled]);

  const save = useCallback(
    (content: string, subject = '') => {
      if (!enabled) return;
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => {
        browserSend('/me/draft', { method: 'POST', body: { context, content, subject } })
          .then(() => {
            saveFailureNotified.current = false;
          })
          .catch(() => {
            if (!saveFailureNotified.current) {
              toastError(new Error('草稿保存失败，当前内容尚未同步到服务器。'));
              saveFailureNotified.current = true;
            }
          });
      }, 2500);
    },
    [context, enabled],
  );

  const clear = useCallback(() => {
    if (!enabled) return;
    if (timer.current) clearTimeout(timer.current);
    browserSend('/me/draft', { method: 'DELETE', body: { context } }).catch(() => {});
  }, [context, enabled]);

  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  return { restored, save, clear };
}
