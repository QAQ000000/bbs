'use client';

import { useState } from 'react';
import { Button } from '@arco-design/web-react';
import { IconCheck, IconSubscribe } from '@arco-design/web-react/icon';
import { browserSend } from '@/lib/api/browser';
import { toastError, toastSuccess } from '../ui/feedback';

export type SubscribeKind = 'thread' | 'forum' | 'tag';

export interface SubscribeButtonProps {
  kind: SubscribeKind;
  id: string;
  /** true / false 为确定状态；null 表示查询失败，显示“状态未确认”。 */
  initialSubscribed: boolean | null;
  size?: 'mini' | 'small' | 'default';
  block?: boolean;
}

function subscribePath(kind: SubscribeKind, id: string): string {
  if (kind === 'thread') return '/threads/' + id + '/subscribe';
  if (kind === 'tag') return '/tags/' + id + '/subscribe';
  return '/forums/' + id + '/subscribe';
}

// 订阅是显式目标状态（POST 新建 / DELETE 取消），不是 toggle 重放。
export function SubscribeButton({ kind, id, initialSubscribed, size = 'default', block }: SubscribeButtonProps) {
  const [subscribed, setSubscribed] = useState<boolean | null>(initialSubscribed);
  const [loading, setLoading] = useState(false);
  const path = subscribePath(kind, id);

  async function toggle() {
    if (subscribed === null) return;
    setLoading(true);
    try {
      if (subscribed) {
        await browserSend(path, { method: 'DELETE' });
        setSubscribed(false);
        toastSuccess('已取消关注');
      } else {
        await browserSend(path, { method: 'POST' });
        setSubscribed(true);
        toastSuccess('已关注，默认开启通知');
      }
    } catch (error) {
      toastError(error);
    } finally {
      setLoading(false);
    }
  }

  if (subscribed === null) {
    return (
      <Button size={size} long={block} disabled title="订阅状态查询失败，请刷新页面重试">
        状态未确认
      </Button>
    );
  }

  return (
    <Button
      size={size}
      long={block}
      loading={loading}
      onClick={() => void toggle()}
      type={subscribed ? 'secondary' : 'outline'}
      icon={subscribed ? <IconCheck /> : <IconSubscribe />}
    >
      {subscribed ? '已关注' : '关注' + (kind === 'forum' ? '版块' : '')}
    </Button>
  );
}
