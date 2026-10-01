'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button } from '@arco-design/web-react';
import { IconDelete } from '@arco-design/web-react/icon';
import { browserSend } from '@/lib/api/browser';
import { toastError, toastSuccess } from '../ui/feedback';

export function DraftActions({ context }: { context: string }) {
  const router = useRouter();
  const [loading, setLoading] = useState(false);

  async function remove() {
    setLoading(true);
    try {
      await browserSend('/me/draft', { method: 'DELETE', body: { context } });
      toastSuccess('草稿已删除');
      router.refresh();
    } catch (error) {
      toastError(error);
    } finally {
      setLoading(false);
    }
  }

  return (
    <Button size="small" type="text" status="danger" icon={<IconDelete />} loading={loading} onClick={() => void remove()}>
      删除
    </Button>
  );
}
