'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Modal } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import { toastError, toastSuccess } from '../ui/feedback';

export interface DeletePostButtonProps {
  postId: string;
  floor: number;
  threadId: string;
  forumId: string;
  size?: 'mini' | 'small' | 'default';
}

// 删除首楼会连主题一起删除；删除回复成功时后端 data 可以为空，不依赖 message。
export function DeletePostButton({ postId, floor, threadId, forumId, size = 'mini' }: DeletePostButtonProps) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const isFirstFloor = floor === 1;

  function confirmDelete() {
    Modal.confirm({
      title: isFirstFloor ? '确认删除整个主题？' : '确认删除该回复？',
      content: isFirstFloor
        ? '删除首楼会同时删除主题及其全部回复，操作不可自动撤销。'
        : '删除后该楼层公开不可见，操作会记录审计日志。',
      okText: '确认删除',
      cancelText: '取消',
      onOk: async () => {
        setBusy(true);
        try {
          await browserSend('/posts/' + postId, { method: 'DELETE' });
          toastSuccess(isFirstFloor ? '主题已删除' : '回复已删除');
          if (isFirstFloor) {
            router.push(forumId ? '/forums/' + forumId : '/forums');
          }
          router.refresh();
        } catch (caught) {
          toastError(caught as ApiError);
          throw caught;
        } finally {
          setBusy(false);
        }
      },
    });
  }

  return (
    <Button size={size} type="text" status="danger" loading={busy} onClick={confirmDelete}>
      {isFirstFloor ? '删除主题' : '删除'}
    </Button>
  );
}
