'use client';

import { useState } from 'react';
import { Button } from '@arco-design/web-react';
import { IconCheck, IconPlus } from '@arco-design/web-react/icon';
import { browserSend } from '@/lib/api/browser';
import { toastError, toastSuccess } from '../ui/feedback';

export interface FollowButtonProps {
  userId: string;
  /**
   * true / false 是后端返回的确定状态；null 表示状态查询失败。
   * 前者按真实状态渲染，后者显示“状态未确认”，不把查询失败当成未关注。
   */
  initialFollowing: boolean | null;
  size?: 'mini' | 'small' | 'default';
  block?: boolean;
  disabled?: boolean;
}

// 关注使用显式目标状态（POST 关注 / DELETE 取消），超时不自动重放。
export function FollowButton({ userId, initialFollowing, size = 'default', block, disabled }: FollowButtonProps) {
  const [following, setFollowing] = useState<boolean | null>(initialFollowing);
  const [loading, setLoading] = useState(false);

  async function toggle() {
    if (following === null) return;
    setLoading(true);
    try {
      if (following) {
        await browserSend('/users/' + userId + '/follow', { method: 'DELETE' });
        setFollowing(false);
        toastSuccess('已取消关注');
      } else {
        await browserSend('/users/' + userId + '/follow', { method: 'POST' });
        setFollowing(true);
        toastSuccess('已关注');
      }
    } catch (error) {
      toastError(error);
    } finally {
      setLoading(false);
    }
  }

  if (following === null) {
    return (
      <Button size={size} long={block} disabled title="关注状态查询失败，请刷新页面重试">
        状态未确认
      </Button>
    );
  }

  return (
    <Button
      size={size}
      long={block}
      disabled={disabled}
      loading={loading}
      type={following ? 'secondary' : 'primary'}
      icon={following ? <IconCheck /> : <IconPlus />}
      onClick={() => void toggle()}
    >
      {following ? '已关注' : '加关注'}
    </Button>
  );
}
