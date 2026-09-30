'use client';

import { useState } from 'react';
import { Button, Input, Modal } from '@arco-design/web-react';
import { browserGet, browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { TitleAdjustment, UserTitle } from '@/lib/api/types';
import { formatDateTime } from '@/lib/format';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './TitleGrants.module.css';

function newKey(uid: string, titleId: string): string {
  return 'title-adjust-' + uid + '-' + titleId + '-' + Date.now().toString(36);
}

export function TitleGrants() {
  const [uid, setUid] = useState('');
  const [currentUid, setCurrentUid] = useState('');
  const [items, setItems] = useState<UserTitle[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [reason, setReason] = useState('');

  async function load(target?: string) {
    const id = (target ?? uid).trim();
    if (!/^[1-9][0-9]*$/.test(id)) {
      setError('请输入有效的用户 ID（正整数）。');
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const list = await browserGet<UserTitle[]>('/admin/users/' + id + '/titles');
      setItems(list ?? []);
      setCurrentUid(id);
    } catch (caught) {
      const err = caught as ApiError;
      setItems([]);
      setError(err.status === 403 ? '缺少查看用户称号的权限：' + err.message : err.message);
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  function adjust(item: UserTitle, action: 'grant' | 'revoke') {
    if ((reason ?? '').trim().length === 0) {
      setError('请先在下方填写操作原因（会写入审计）。');
      return;
    }
    Modal.confirm({
      title: action === 'grant' ? '确认授予称号' : '确认撤销称号',
      content: (
        <div>
          <p className={styles.confirmLine}>用户 #{currentUid} · 称号「{item.title.name}」</p>
          <p className={styles.confirmLine}>
            {action === 'grant'
              ? '授予后立即获得该称号，可自行佩戴；不会绕过有效期与发放窗口。'
              : '撤销会移除该称号并取消佩戴，且阻止后续自动再次授予。'}
          </p>
          <p className={styles.confirmLine}>原因：{reason}</p>
        </div>
      ),
      okText: action === 'grant' ? '确认授予' : '确认撤销',
      cancelText: '取消',
      onOk: async () => {
        const body: TitleAdjustment = {
          action,
          reason: reason.trim(),
          key: newKey(currentUid, item.title.id),
          // 后端要求传入称号定义当前版本，而不是用户获得记录的版本。
          version: item.title.version,
        };
        setBusy(true);
        setError(null);
        try {
          await browserSend('/admin/users/' + currentUid + '/titles/' + item.title.id, { method: 'PATCH', body });
          toastSuccess(action === 'grant' ? '已授予' : '已撤销');
          setReason('');
          await load(currentUid);
        } catch (caught) {
          const err = caught as ApiError;
          setError(
            err.status === 403
              ? '缺少授予 / 撤销权限：' + err.message
              : err.status === 409
                ? '版本冲突或幂等键冲突（称号定义可能已更新），请刷新后重试。'
                : err.message,
          );
          toastError(err);
        } finally {
          setBusy(false);
        }
      },
    });
  }

  return (
    <section className={['panel', styles.card].join(' ')}>
      <div className={styles.head}>
        <h2 className={styles.title}>用户称号与授予 / 撤销</h2>
        <span className={styles.hint}>配置、获得记录与佩戴状态分开：这里只处理获得记录。</span>
      </div>
      <div className={styles.searchRow}>
        <Input value={uid} onChange={setUid} placeholder="用户 ID" style={{ width: 160 }} onPressEnter={() => void load()} />
        <Button size="small" type="secondary" loading={busy} onClick={() => void load()}>
          查询
        </Button>
      </div>
      {error ? <p className={styles.error} role="alert">{error}</p> : null}
      <label className={styles.reasonField}>
        操作原因（授予 / 撤销共用，必填 ≤500）
        <Input value={reason} onChange={setReason} maxLength={500} placeholder="例如：活动奖励补发 / 违规撤销" />
      </label>

      {currentUid === '' ? (
        <p className={styles.hint}>输入用户 ID 查看已获得称号与佩戴状态。</p>
      ) : items.length === 0 ? (
        <p className={styles.hint}>该用户还没有称号记录。</p>
      ) : (
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>称号</th>
                <th>状态</th>
                <th>来源</th>
                <th>进度</th>
                <th>获得 / 到期</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.title.id}>
                  <td>
                    <span className={styles.badge} style={{ background: item.title.badge.background, color: item.title.badge.color }}>
                      {item.title.badge.label || item.title.name}
                    </span>
                    {item.title.name}
                    {item.equipped ? <span className={styles.equipped}>佩戴中</span> : null}
                  </td>
                  <td>{item.status}</td>
                  <td>{item.source || '-'}</td>
                  <td className={styles.progress}>
                    {item.title.conditions.map((cond, index) => (
                      <div key={String(index)}>
                        {cond.metric} ≥ {cond.target}：{item.counts[index] ?? 0}
                      </div>
                    ))}
                    {item.progressPending ? <div className={styles.hint}>进度计算中</div> : null}
                  </td>
                  <td className={styles.hint}>
                    {item.earnedAt ? formatDateTime(item.earnedAt) : '-'}
                    <br />
                    {item.expiresAt ? '到期 ' + formatDateTime(item.expiresAt) : '永久'}
                  </td>
                  <td className={styles.actions}>
                    <Button size="mini" type="text" loading={busy} onClick={() => adjust(item, 'grant')}>
                      授予
                    </Button>
                    <Button size="mini" type="text" status="danger" loading={busy} onClick={() => adjust(item, 'revoke')}>
                      撤销
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
