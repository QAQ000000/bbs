'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import { toastError, toastSuccess } from '../ui/feedback';

export interface AcceptButtonProps {
  postId: string;
  accepted: boolean;
  canAccept: boolean;
  canUnaccept: boolean;
}

// 采纳会同时结算活动悬赏；已支付悬赏不能撤销。
export function AcceptButton({ postId, accepted, canAccept, canUnaccept }: AcceptButtonProps) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);

  async function run(action: 'accept' | 'unaccept') {
    setBusy(true);
    try {
      await browserSend('/posts/' + postId + '/acceptance', {
        method: action === 'accept' ? 'PUT' : 'DELETE',
      });
      toastSuccess(action === 'accept' ? '已采纳该回复' : '已取消采纳');
      router.refresh();
    } catch (caught) {
      toastError(caught as ApiError);
    } finally {
      setBusy(false);
    }
  }

  if (!canAccept && !canUnaccept) return null;
  if (accepted && canUnaccept) {
    return (
      <Button size="mini" type="text" loading={busy} onClick={() => void run('unaccept')}>
        取消采纳
      </Button>
    );
  }
  if (!accepted && canAccept) {
    return (
      <Button size="mini" type="text" loading={busy} onClick={() => void run('accept')}>
        采纳
      </Button>
    );
  }
  return null;
}
