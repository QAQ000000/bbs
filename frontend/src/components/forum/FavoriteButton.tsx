'use client';

import { useState } from 'react';
import { Button } from '@arco-design/web-react';
import { IconStar, IconStarFill } from '@arco-design/web-react/icon';
import { browserSend } from '@/lib/api/browser';
import { toastError, toastSuccess } from '../ui/feedback';

export interface FavoriteButtonProps {
  threadId: string;
  initialFavorite: boolean;
}

// 收藏是 toggle：禁止自动重放；失败后由用户重新点击，状态以服务端返回为准。
export function FavoriteButton({ threadId, initialFavorite }: FavoriteButtonProps) {
  const [favorite, setFavorite] = useState(initialFavorite);
  const [loading, setLoading] = useState(false);

  async function toggle() {
    setLoading(true);
    try {
      const result = await browserSend<{ favorite: boolean }>('/threads/' + threadId + '/favorite', {
        method: 'POST',
      });
      setFavorite(result.favorite);
      toastSuccess(result.favorite ? '已收藏' : '已取消收藏');
    } catch (error) {
      toastError(error);
    } finally {
      setLoading(false);
    }
  }

  return (
    <Button
      size="small"
      type={favorite ? 'primary' : 'secondary'}
      loading={loading}
      icon={favorite ? <IconStarFill /> : <IconStar />}
      onClick={() => void toggle()}
    >
      {favorite ? '已收藏' : '收藏'}
    </Button>
  );
}
