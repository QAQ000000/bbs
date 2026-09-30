'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { toastError, toastSuccess } from '../ui/feedback';

export interface UnsubscribeButtonProps {
  kind: 'thread' | 'forum' | 'tag';
  targetId: string;
}

function path(kind: string, targetId: string): string {
  if (kind === 'thread') return '/threads/' + targetId + '/subscribe';
  if (kind === 'tag') return '/tags/' + targetId + '/subscribe';
  return '/forums/' + targetId + '/subscribe';
}

export function UnsubscribeButton({ kind, targetId }: UnsubscribeButtonProps) {
  const router = useRouter();
  const [loading, setLoading] = useState(false);

  async function remove() {
    setLoading(true);
    try {
      await browserSend(path(kind, targetId), { method: 'DELETE' });
      toastSuccess('已取消订阅');
      router.refresh();
    } catch (error) {
      toastError(error);
    } finally {
      setLoading(false);
    }
  }

  return (
    <Button size="mini" type="secondary" loading={loading} onClick={() => void remove()}>
      取消订阅
    </Button>
  );
}
