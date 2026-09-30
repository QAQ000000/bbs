'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button } from '@arco-design/web-react';
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
    <Button size="mini" type="text" status="danger" loading={loading} onClick={() => void remove()}>
      删除
    </Button>
  );
}
