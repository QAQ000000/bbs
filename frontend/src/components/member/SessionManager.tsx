'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import type { SessionList } from '@/lib/api/types';
import { formatDateTime } from '@/lib/format';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './member-forms.module.css';

export function SessionManager({ sessions }: { sessions: SessionList }) {
  const router = useRouter();
  const [busy, setBusy] = useState<string | null>(null);

  async function revokeOthers() {
    setBusy('others');
    try {
      await browserSend('/me/sessions/revoke-others', { method: 'POST' });
      toastSuccess('已退出其他设备');
      router.refresh();
    } catch (error) {
      toastError(error);
    } finally {
      setBusy(null);
    }
  }

  async function revoke(id: string) {
    setBusy(id);
    try {
      await browserSend('/me/sessions/' + id, { method: 'DELETE' });
      toastSuccess('已撤销该设备');
      router.refresh();
    } catch (error) {
      toastError(error);
    } finally {
      setBusy(null);
    }
  }

  return (
    <div>
      <ul className={styles.sessions}>
        {sessions.items.map((item) => (
          <li key={item.id} className={styles.session}>
            <div className={styles.sessionBody}>
              <p className={styles.sessionName}>
                {item.name || item.userAgent || '未知设备'}
                {item.current ? <span className={styles.current}>当前设备</span> : null}
              </p>
              <span className={styles.sessionMeta}>
                {item.maskedIp || 'IP 未知'} · 最后活动 {formatDateTime(item.lastSeenAt)}
              </span>
            </div>
            {!item.current ? (
              <Button size="mini" type="secondary" loading={busy === item.id} onClick={() => void revoke(item.id)}>
                撤销
              </Button>
            ) : null}
          </li>
        ))}
      </ul>
      {sessions.items.length > 1 ? (
        <Button size="small" type="secondary" loading={busy === 'others'} onClick={() => void revokeOthers()}>
          退出其他设备
        </Button>
      ) : null}
    </div>
  );
}
