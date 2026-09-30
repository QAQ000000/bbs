'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Input, Modal } from '@arco-design/web-react';
import { browserGet, browserRequest, browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { AdminBountyView, BountyRefundDiagnostics } from '@/lib/api/types';
import { formatCount, formatDateTime } from '@/lib/format';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './BountyAdmin.module.css';

const STATE_LABELS: Record<string, string> = {
  active: '进行中',
  awarded: '已结算',
  canceled: '已取消并退款',
  expired: '已过期并退款',
  all: '全部',
};

export interface BountyAdminProps {
  items: AdminBountyView[];
  nextBefore: string;
  state: string;
  refundFailed: boolean;
  diagnostics: BountyRefundDiagnostics | null;
}

export function BountyAdmin({ items, nextBefore, state, refundFailed, diagnostics }: BountyAdminProps) {
  const router = useRouter();
  const [rows, setRows] = useState<AdminBountyView[]>(items);
  const [cursor, setCursor] = useState(nextBefore);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [op, setOp] = useState<{ item: AdminBountyView; kind: 'retry' | 'cancel' } | null>(null);
  const [reason, setReason] = useState('');
  const [detail, setDetail] = useState<AdminBountyView | null>(null);

  async function loadMore() {
    if (!cursor || busy) return;
    setBusy(true);
    setError(null);
    try {
      const page = await browserRequest<{ items: AdminBountyView[]; nextBefore: string }>('/admin/bounties', {
        query: { before: cursor, state, refundFailed: refundFailed ? 'true' : undefined },
      });
      const next = page.data?.items ?? [];
      setRows((prev) => {
        const seen = new Set(prev.map((item) => item.threadId));
        return prev.concat(next.filter((item) => !seen.has(item.threadId)));
      });
      setCursor(page.data?.nextBefore ?? '');
    } catch (caught) {
      toastError(caught as ApiError);
    } finally {
      setBusy(false);
    }
  }

  async function openDetail(item: AdminBountyView) {
    setBusy(true);
    try {
      const full = await browserGet<AdminBountyView>('/admin/bounties/' + item.threadId);
      setDetail(full);
    } catch (caught) {
      toastError(caught as ApiError);
    } finally {
      setBusy(false);
    }
  }

  async function submit() {
    if (!op) return;
    if (reason.trim().length === 0) {
      setError('请填写操作原因（必填，≤500 字，会写入审计）。');
      return;
    }
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const path = '/admin/bounties/' + op.item.threadId + (op.kind === 'retry' ? '/retry' : '/cancel');
      await browserSend(path, { method: 'POST', body: { reason: reason.trim() } });
      toastSuccess(op.kind === 'retry' ? '已重新排队退款' : '已取消并退款');
      setNotice(
        op.kind === 'retry'
          ? '退款已重新排队，由后台 Worker 处理；可稍后重新读取状态。'
          : '悬赏已取消并退回冻结积分（已支付的悬赏不允许取消）。',
      );
      setOp(null);
      setReason('');
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      setError(
        err.status === 409
          ? '状态已变化：已结算 / 已退款的悬赏不能再取消或重试。请重新读取后确认。'
          : err.message,
      );
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className={['panel', styles.card].join(' ')}>
      <div className={styles.head}>
        <h2 className={styles.title}>悬赏列表（{rows.length}）</h2>
        <span className={styles.hint}>
          已支付（awarded）的悬赏不能撤销或重算，后台不提供改余额或改状态的表单。
        </span>
      </div>

      {diagnostics ? (
        <div className={styles.diag}>
          <span>进行中 <strong>{formatCount(diagnostics.active)}</strong></span>
          <span>已到期待退 <strong>{formatCount(diagnostics.due)}</strong></span>
          <span>退款失败 <strong>{formatCount(diagnostics.failed)}</strong></span>
          <span>已排期重试 <strong>{formatCount(diagnostics.scheduled)}</strong></span>
          <span>
            最早到期 <strong>{diagnostics.oldestDueAt ? formatDateTime(diagnostics.oldestDueAt) : '—'}</strong>
          </span>
        </div>
      ) : null}

      <div className={styles.filters}>
        <span className={styles.filterLabel}>状态</span>
        {['active', 'awarded', 'canceled', 'expired', 'all'].map((value) => (
          <a
            key={value}
            className={value === state ? styles.filterActive : styles.filter}
            href={'/admin/bounties?state=' + value + (refundFailed ? '&refundFailed=true' : '')}
          >
            {STATE_LABELS[value]}
          </a>
        ))}
        <a
          className={refundFailed ? styles.filterActive : styles.filter}
          href={'/admin/bounties?state=' + state + (refundFailed ? '' : '&refundFailed=true')}
        >
          只看退款失败
        </a>
      </div>

      {error ? <p className={styles.error} role="alert">{error}</p> : null}
      {notice ? <p className={styles.notice}>{notice}</p> : null}

      {rows.length === 0 ? (
        <p className={styles.hint}>当前筛选下没有悬赏。</p>
      ) : (
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>主题</th>
                <th>发起人</th>
                <th>积分</th>
                <th>状态</th>
                <th>结算 / 截止</th>
                <th>退款</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((item) => (
                <tr key={item.threadId}>
                  <td>
                    <a href={'/threads/' + item.threadId} target="_blank" rel="noreferrer">
                      #{item.threadId}
                    </a>
                  </td>
                  <td>#{item.ownerId}</td>
                  <td>{formatCount(item.amount)}</td>
                  <td>{STATE_LABELS[item.state] ?? item.state}</td>
                  <td className={styles.time}>
                    {item.settledAt ? '结算 ' + formatDateTime(item.settledAt) : '截止 ' + formatDateTime(item.closesAt)}
                  </td>
                  <td className={styles.time}>
                    {item.refundErrorCode ? (
                      <span className={styles.bad}>
                        {item.refundErrorCode}（第 {item.refundAttempts} 次）
                      </span>
                    ) : (
                      <span className={styles.hint}>无异常</span>
                    )}
                    {item.refundNextAttemptAt ? (
                      <div className={styles.hint}>下次 {formatDateTime(item.refundNextAttemptAt)}</div>
                    ) : null}
                  </td>
                  <td className={styles.actions}>
                    <Button size="mini" type="text" loading={busy} onClick={() => void openDetail(item)}>
                      详情
                    </Button>
                    {item.state === 'active' && item.refundErrorCode ? (
                      <Button size="mini" type="text" loading={busy} onClick={() => { setOp({ item, kind: 'retry' }); setReason(''); setError(null); }}>
                        重试退款
                      </Button>
                    ) : null}
                    {item.state === 'active' ? (
                      <Button size="mini" type="text" status="danger" loading={busy} onClick={() => { setOp({ item, kind: 'cancel' }); setReason(''); setError(null); }}>
                        取消并退款
                      </Button>
                    ) : (
                      <span className={styles.hint}>不可操作</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {cursor ? (
        <div className={styles.footer}>
          <Button size="small" type="secondary" loading={busy} onClick={() => void loadMore()}>
            加载更多
          </Button>
        </div>
      ) : null}

      <Modal
        title={op ? (op.kind === 'retry' ? '重试退款' : '取消并退款') + '：主题 #' + op.item.threadId : '悬赏操作'}
        visible={Boolean(op)}
        confirmLoading={busy}
        onCancel={() => setOp(null)}
        onOk={() => void submit()}
        okText="确认"
        cancelText="取消"
      >
        {op ? (
          <div className={styles.form}>
            {error ? <p className={styles.error} role="alert">{error}</p> : null}
            <p className={styles.hint}>
              对象：主题 #{op.item.threadId} · 发起人 #{op.item.ownerId} · 冻结 {formatCount(op.item.amount)} 积分
            </p>
            <p className={styles.hint}>
              后果：
              {op.kind === 'retry'
                ? '把失败的退款重新排给后台 Worker 处理，走既有退避与幂等，不会重复扣减或重复退款。'
                : '取消活动并把冻结积分退回发起人；只对“进行中”的悬赏生效，已支付或已退款的会返回 409。'}
            </p>
            <label className={styles.field}>
              原因（必填，≤500）
              <Input value={reason} onChange={setReason} maxLength={500} placeholder="说明处理依据" />
            </label>
          </div>
        ) : null}
      </Modal>

      <Modal title="悬赏详情" visible={Boolean(detail)} footer={null} onCancel={() => setDetail(null)} style={{ width: 620 }}>
        {detail ? (
          <dl className={styles.detail}>
            <div><dt>主题</dt><dd>#{detail.threadId}</dd></div>
            <div><dt>状态</dt><dd>{STATE_LABELS[detail.state] ?? detail.state}</dd></div>
            <div><dt>发起人</dt><dd>#{detail.ownerId}</dd></div>
            <div><dt>积分</dt><dd>{formatCount(detail.amount)}</dd></div>
            <div><dt>规则版本</dt><dd>v{detail.ruleVersion}</dd></div>
            <div><dt>有效小时数</dt><dd>{detail.durationHours}</dd></div>
            <div><dt>创建</dt><dd>{formatDateTime(detail.createdAt)}</dd></div>
            <div><dt>截止</dt><dd>{formatDateTime(detail.closesAt)}</dd></div>
            <div><dt>结算时间</dt><dd>{detail.settledAt ? formatDateTime(detail.settledAt) : '—'}</dd></div>
            <div><dt>收款人 / 楼层</dt><dd>#{detail.recipientId} · #{detail.postId}</dd></div>
            <div><dt>退款尝试</dt><dd>{detail.refundAttempts}</dd></div>
            <div><dt>退款错误</dt><dd>{detail.refundErrorCode || '无'}</dd></div>
            <div><dt>备注</dt><dd>{detail.note || '—'}</dd></div>
          </dl>
        ) : null}
      </Modal>
    </section>
  );
}
