'use client';

import { useState } from 'react';
import { IconHeart, IconHeartFill } from '@arco-design/web-react/icon';
import { browserSend } from '@/lib/api/browser';
import { toastError } from '../ui/feedback';
import styles from './LikeButton.module.css';

export interface LikeButtonProps {
  postId: string;
  initialLiked: boolean;
  initialCount: number;
}

// 点赞是 toggle：超时不自动重放，状态以服务端返回为准。
export function LikeButton({ postId, initialLiked, initialCount }: LikeButtonProps) {
  const [liked, setLiked] = useState(initialLiked);
  const [count, setCount] = useState(initialCount);
  const [loading, setLoading] = useState(false);

  async function toggle() {
    if (loading) return;
    setLoading(true);
    try {
      const result = await browserSend<{ liked: boolean; count: number }>('/posts/' + postId + '/like', {
        method: 'POST',
      });
      setLiked(result.liked);
      setCount(result.count);
    } catch (error) {
      toastError(error);
    } finally {
      setLoading(false);
    }
  }

  return (
    <button
      type="button"
      className={liked ? styles.liked : styles.like}
      onClick={() => void toggle()}
      disabled={loading}
      aria-pressed={liked}
      aria-label={liked ? '取消点赞' : '点赞'}
    >
      {liked ? <IconHeartFill /> : <IconHeart />}
      <span>{count}</span>
    </button>
  );
}
