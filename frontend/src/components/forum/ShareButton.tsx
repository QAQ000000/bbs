'use client';

import { Button } from '@arco-design/web-react';
import { IconLink } from '@arco-design/web-react/icon';
import { toastError, toastSuccess } from '../ui/feedback';

export function ShareButton({ title }: { title?: string }) {
  async function share() {
    const url = typeof window === 'undefined' ? '' : window.location.href;
    if (!url) return;
    if (typeof navigator !== 'undefined' && navigator.share) {
      try {
        await navigator.share({ url, title: title ?? document.title });
        return;
      } catch {
        // 用户取消或不支持时回退到复制。
      }
    }
    try {
      await navigator.clipboard.writeText(url);
      toastSuccess('链接已复制');
    } catch (error) {
      toastError(error);
    }
  }

  return (
    <Button size="small" type="secondary" icon={<IconLink />} onClick={() => void share()}>
      分享
    </Button>
  );
}
