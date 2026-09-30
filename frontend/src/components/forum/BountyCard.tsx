'use client';

import { useState } from 'react';
import { Button, InputNumber } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { BountyView, EngagementRules, PointsAccount } from '@/lib/api/types';
import { formatCount, formatDateTime, isEmptyTime } from '@/lib/format';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './EngagementCard.module.css';

export interface BountyCardProps {
  threadId: string;
  bounty: BountyView | null;
  rules: EngagementRules['bounty'] | null;
  account: PointsAccount | null;
  canCreate: boolean;
  canCancel: boolean;
  loggedIn: boolean;
}

const STATE_LABELS: Record<string, string> = {
  active: '进行中',
  awarded: '已结算',
  canceled: '已取消并退款',
  expired: '已过期并退款',
};

export function BountyCard({ threadId, bounty, rules, account, canCreate, canCancel, loggedIn }: BountyCardProps) {
  const [current, setCurrent] = useState<BountyView | null>(bounty);
  const [showCreate, setShowCreate] = useState(false);
  const [amount, setAmount] = useState(rules?.minPoints ?? 100);
  const [durationHours, setDurationHours] = useState(72);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const minPoints = rules?.minPoints ?? 1;
  const maxPoints = rules?.maxPoints ?? 10000;
  const maxDays = rules?.maxDays ?? 30;
  const available = account?.available ?? null;
  const due = Boolean(current && current.state === 'active' && !isEmptyTime(current.closesAt) && Date.parse(current.closesAt) <= Date.now());

  async function create(event: React.FormEvent) {
    event.preventDefault();
    if (amount < minPoints || amount > maxPoints) {
      setError('悬赏积分需在 ' + minPoints + ' 到 ' + maxPoints + ' 之间');
      return;
    }
    if (available !== null && amount > available) {
      setError('可用积分不足：当前可用 ' + available);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const created = await browserSend<BountyView>('/threads/' + threadId + '/bounty', {
        method: 'POST',
        body: { amount, durationHours },
      });
      setCurrent(created);
      setShowCreate(false);
      setNotice('悬赏已发布，积分已冻结（不计入可用余额），采纳后结算给答主。');
      toastSuccess('悬赏已发布');
    } catch (caught) {
      const err = caught as ApiError;
      // 超时不自动重试；先按幂等语义重新读取实际状态。
      if (err.status === 409 || err.code === 'POINTS_INSUFFICIENT') {
        setError(err.message);
      } else {
        setError(err.message);
      }
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  async function cancel() {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const updated = await browserSend<BountyView>('/threads/' + threadId + '/bounty/cancel', { method: 'POST' });
      setCurrent(updated);
      setNotice('已取消并退款，冻结积分已释放。');
      toastSuccess('已取消并退款');
    } catch (caught) {
      const err = caught as ApiError;
      setError(err.message);
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  if (!current) {
    if (!canCreate) return null;
    if (rules && !rules.enabled) {
      return (
        <section className={['panel', styles.card].join(' ')} aria-label="悬赏">
          <p className={styles.muted}>站点已停用悬赏功能。</p>
        </section>
      );
    }
    if (!showCreate) {
      return (
        <section className={['panel', styles.card].join(' ')} aria-label="悬赏">
          <div className={styles.cardHead}>
            <h2 className={styles.cardTitle}>悬赏</h2>
            <Button size="small" type="outline" onClick={() => setShowCreate(true)} disabled={!loggedIn}>
              发布悬赏
            </Button>
          </div>
          <p className={styles.muted}>
            每个主题最多一笔悬赏，取消后不能重新发布。当前可用积分：
            {available === null ? '未知' : formatCount(available)}
          </p>
        </section>
      );
    }
    return (
      <section className={['panel', styles.card].join(' ')} aria-label="发布悬赏">
        <div className={styles.cardHead}>
          <h2 className={styles.cardTitle}>发布悬赏</h2>
        </div>
        {error ? <p className={styles.error}>{error}</p> : null}
        <form onSubmit={create} className={styles.form}>
          <div className={styles.inlineFields}>
            <div>
              <label className={styles.label} htmlFor="bounty-amount">
                悬赏积分（{minPoints} - {maxPoints}）
              </label>
              <InputNumber
                id="bounty-amount"
                min={minPoints}
                max={maxPoints}
                value={amount}
                onChange={(v) => setAmount(Number(v) || minPoints)}
              />
            </div>
            <div>
              <label className={styles.label} htmlFor="bounty-hours">
                有效小时数（≤ {maxDays * 24}）
              </label>
              <InputNumber
                id="bounty-hours"
                min={1}
                max={maxDays * 24}
                value={durationHours}
                onChange={(v) => setDurationHours(Number(v) || 72)}
              />
            </div>
          </div>
          <p className={styles.muted}>
            发布后积分进入冻结（balance 不减，available = balance - frozen）；采纳答主时结算，超时或删除由后台退款。
            当前可用：{available === null ? '未知' : formatCount(available)}。
          </p>
          <div className={styles.actions}>
            <Button type="primary" htmlType="submit" loading={busy}>
              发布
            </Button>
            <Button type="text" onClick={() => setShowCreate(false)}>
              取消
            </Button>
          </div>
        </form>
      </section>
    );
  }

  return (
    <section className={['panel', styles.card].join(' ')} aria-label="悬赏">
      <div className={styles.cardHead}>
        <h2 className={styles.cardTitle}>悬赏</h2>
        <span className={styles.stateBadge}>{STATE_LABELS[current.state] ?? current.state}</span>
      </div>
      {notice ? <p className={styles.notice}>{notice}</p> : null}
      {error ? <p className={styles.error}>{error}</p> : null}
      <p className={styles.bountyAmount}>
        <strong>{formatCount(current.amount)}</strong> 积分
      </p>
      <p className={styles.muted}>
        状态 {STATE_LABELS[current.state] ?? current.state}
        {current.state === 'active' ? (due ? ' · 已到期，等待退款' : ' · 截止 ' + formatDateTime(current.closesAt)) : ''}
        {current.settledAt ? ' · 结算于 ' + formatDateTime(current.settledAt) : ''}
      </p>
      {current.state === 'awarded' && current.recipientId !== '0' ? (
        <p className={styles.muted}>已支付给用户 #{current.recipientId}（楼层 #{current.postId}）。已支付后不可撤回或改付。</p>
      ) : null}
      {current.state === 'active' && due ? (
        <p className={styles.muted}>已到截止时间，不能继续采纳支付；退款由后台 Worker 处理，可稍后重新读取状态。</p>
      ) : null}
      {current.note ? <p className={styles.muted}>备注：{current.note}</p> : null}
      <div className={styles.cardFoot}>
        <span className={styles.muted}>规则版本 v{current.ruleVersion}</span>
        <div className={styles.actions}>
          {canCancel && current.state === 'active' ? (
            <Button size="small" type="secondary" loading={busy} onClick={() => void cancel()}>
              取消悬赏
            </Button>
          ) : null}
        </div>
      </div>
    </section>
  );
}
