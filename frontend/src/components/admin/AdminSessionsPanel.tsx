'use client';

import { useState } from 'react';
import { Button, Input, Modal } from '@arco-design/web-react';
import { browserGet, browserRequest, browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { SessionDeviceView } from '@/lib/api/types';
import { formatDateTime } from '@/lib/format';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './AdminSessionsPanel.module.css';

export interface AdminSessionsPanelProps {
  currentUserId: string;
}

export function AdminSessionsPanel({ currentUserId }: AdminSessionsPanelProps) {
  const [uid, setUid] = useState('');
  const [currentUid, setCurrentUid] = useState('');
  const [items, setItems] = useState<SessionDeviceView[]>([]);
  const [cursor, setCursor] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  // currentUid 初始是空字符串（非 nullish），不能用 ?? 链读取，否则会拿到空串。
  function resolveUid(target?: string): string {
    const direct = (target ?? '').trim();
    if (direct) return direct;
    if (currentUid) return currentUid;
    return uid.trim();
  }

  async function load(target?: string) {
    const list = resolveUid(target);
    if (!/^[1-9][0-9]*$/.test(list)) {
      setError('请输入有效的用户 ID（正整数）。');
      return;
    }
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const page = await browserRequest<{ items: SessionDeviceView[]; nextBefore: string }>(
        '/admin/users/' + list + '/sessions',
        { query: { page: 1 } },
      );
      setItems(page.data?.items ?? []);
      setCursor(page.data?.nextBefore ?? '');
      setCurrentUid(list);
    } catch (caught) {
      const err = caught as ApiError;
      setItems([]);
      setError(err.status === 403 ? '缺少 sessions.manage 权限：' + err.message : err.message);
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  async function verify(target?: string) {
    const list = resolveUid(target);
    if (!/^[1-9][0-9]*$/.test(list)) {
      setError('请输入有效的用户 ID（正整数）。');
      return;
    }
    setBusy(true);
    try {
      const found = await browserGet<SessionDeviceView[]>('/admin/users/' + list + '/sessions');
      setItems(Array.isArray(found) ? found : []);
    } catch {
      // 超时或失败时先查询实际状态，不自动重复撤销。
    } finally {
      setBusy(false);
    }
  }

  function revoke(session: SessionDeviceView | null, all: boolean) {
    if (!currentUid) return;
    const target = all ? currentUid + ' 的全部有效会话' : '会话 #' + session?.id + '（' + (session?.name || '未命名设备') + '）';
    const selfWarning = currentUid === currentUserId ? '注意：这是你当前登录的账号，撤销后你会被强制下线。' : '';
    Modal.confirm({
      title: all ? '确认撤销该用户全部会话' : '确认撤销该会话',
      content: (
        <div>
          <p className={styles.confirmLine}>目标账号：#{currentUid}</p>
          <p className={styles.confirmLine}>对象：{target}</p>
          {session && !all ? (
            <p className={styles.confirmLine}>
              设备：{session.name || '未命名'} · {session.maskedIp || 'IP 未知'} · 最近活动 {formatDateTime(session.lastSeenAt)}
            </p>
          ) : null}
          <p className={styles.confirmLine}>
            影响：被撤销的会话立即失效，需要重新登录；不会封禁账号，也不修改密码或权限。
          </p>
          {selfWarning ? <p className={styles.warn}>{selfWarning}</p> : null}
        </div>
      ),
      okText: all ? '撤销全部' : '撤销该会话',
      cancelText: '取消',
      onOk: async () => {
        setBusy(true);
        setError(null);
        setNotice(null);
        try {
          const path = all
            ? '/admin/users/' + currentUid + '/sessions'
            : '/admin/users/' + currentUid + '/sessions/' + session?.id;
          const result = await browserSend<{ affected: number }>(path, { method: 'DELETE' });
          toastSuccess('已撤销 ' + (result?.affected ?? 0) + ' 个会话');
          setNotice('已撤销 ' + (result?.affected ?? 0) + ' 个会话，列表已重新读取。');
          await verify(currentUid);
        } catch (caught) {
          const err = caught as ApiError;
          setError(
            err.status === 404
              ? '用户或会话不存在（可能已经失效）。请先重新读取列表确认实际状态。'
              : err.status === 403
                ? '缺少 sessions.manage 权限：' + err.message
                : '撤销未成功：' + err.message + '。请先重新读取列表确认实际状态，再决定是否再次操作。',
          );
          toastError(err);
        } finally {
          setBusy(false);
        }
      },
    });
  }

  return (
    <section className={['panel', styles.card].join(' ')} aria-label="用户设备会话">
      <div className={styles.head}>
        <h2 className={styles.title}>用户设备会话（管理员）</h2>
        <span className={styles.hint}>
          只读列表 + 撤销会话；不改密码、不改权限、不封禁账号。普通用户在自己的「安全设置」里也能管理这些设备。
        </span>
      </div>
      <div className={styles.searchRow}>
        <Input value={uid} onChange={setUid} placeholder="用户 ID" style={{ width: 160 }} onPressEnter={() => void load()} />
        <Button size="small" type="secondary" loading={busy} onClick={() => void load()}>
          查询
        </Button>
      </div>
      {error ? <p className={styles.error} role="alert">{error}</p> : null}
      {notice ? <p className={styles.notice}>{notice}</p> : null}

      {!currentUid ? (
        <p className={styles.hint}>输入用户 ID 查看其登录设备；撤销前会显示目标账号与设备。</p>
      ) : items.length === 0 ? (
        <p className={styles.hint}>该用户当前没有有效会话。</p>
      ) : (
        <>
          <div className={styles.tableWrap}>
            <table className={styles.table}>
              <thead>
                <tr>
                  <th>设备</th>
                  <th>IP</th>
                  <th>最近活动</th>
                  <th>创建 / 过期</th>
                  <th>状态</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {items.map((session) => (
                  <tr key={session.id}>
                    <td>
                      <span className={styles.deviceName}>{session.name || '未命名设备'}</span>
                      <span className={styles.ua}>{session.userAgent || '未知客户端'}</span>
                    </td>
                    <td className={styles.mono}>{session.maskedIp || '-'}</td>
                    <td className={styles.hint}>{formatDateTime(session.lastSeenAt)}</td>
                    <td className={styles.hint}>
                      {formatDateTime(session.createdAt)}
                      <br />
                      {formatDateTime(session.expiresAt)}
                    </td>
                    <td>{session.status}</td>
                    <td className={styles.actions}>
                      <Button size="mini" type="text" status="danger" loading={busy} onClick={() => revoke(session, false)}>
                        撤销
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className={styles.actions}>
            <Button size="small" type="secondary" status="danger" loading={busy} onClick={() => revoke(null, true)}>
              撤销全部会话
            </Button>
            {cursor ? <span className={styles.hint}>还有更早的会话未显示（分页 50/页）。</span> : null}
          </div>
        </>
      )}
    </section>
  );
}
