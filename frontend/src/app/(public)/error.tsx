'use client';

import { Button } from '@arco-design/web-react';
import { IconRefresh } from '@arco-design/web-react/icon';
import { useRouter } from 'next/navigation';
import { ErrorState } from '@/components/ui/StateView';

export default function PublicError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  const router = useRouter();

  function retry() {
    router.refresh();
    reset();
  }

  return (
    <div className="page container">
      <ErrorState title="页面加载失败" description="服务暂时不可用，请稍后重试。" />
      <div style={{ textAlign: 'center' }}>
        <Button type="outline" icon={<IconRefresh />} onClick={retry}>重新加载</Button>
      </div>
    </div>
  );
}
