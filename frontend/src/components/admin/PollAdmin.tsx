'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Input, Modal } from '@arco-design/web-react';
import { browserGet, browserRequest, browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { PollModerationItem, PollView } from '@/lib/api/types';
import { formatCount, formatDateTime } from '@/lib/format';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './PollAdmin.module.css';

type ModerationAction = 'approve' | 'reject' | 'close';

const ACTION_LABELS: Record<ModerationAction, string> = {
  approve: '通过（公开发布）',
  reject: '驳回',
  close: '关闭',
};

export interface PollAdminProps {
  items: PollModerationItem[];
  nextBefore: string;
}

export function PollAdmin({ items, nextBefore }: PollAdminProps) {
  const router = useRouter();
  const [rows, setRows] = useState<PollModerationItem[]>(items);
  const [cursor, setCursor] = useState(nextBefore);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [action, setAction] = useState<{ item: PollModerationItem; kind: ModerationAction } | null>(null);
  const [reason, setReason] = useState('');
  const [detail, setDetail] = useState<PollView | null>(null);
  const [detailBusy, setDetailBusy] = useState(false);

  async function loadMore() {
    if (!cursor || busy) return;
    setBusy(true);
    setError(null);
    try {
      const page = await browserRequest<{ items: PollModerationItem[]; nextBefore: string }>('/admin/polls', {
        query: { before: cursor },
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

  async function openDetail(item: PollModerationItem) {
    setDetailBusy(true);
    try {
      const poll = await browserGet<PollView>('/threads/' + item.threadId + '/poll');
      setDetail(poll);
    } catch (caught) {
      toastError(caught as ApiError);
    } finally {
      setDetailBusy(false);
    }
  }

  async function submit() {
    if (!action) return;
    if ((action.kind === 'reject' || action.kind === 'close') && reason.trim().length === 0) {
      setError('驳回或关闭需要填写原因（会写入审计，≤500 字）。');
      return;
    }
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      await browserSend('/admin/polls/' + action.item.threadId + '/moderation', {
        method: 'POST',
        body: { action: action.kind, reason: reason.trim() },
      });
      toastSuccess('已处理：' + ACTION_LABELS[action.kind]);
      setNotice('投票（主题 #' + action.item.threadId + '）已' + ACTION_LABELS[action.kind] + '，列表已重新读取。');
      setAction(null);
      setReason('');
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      setError(
        err.status === 409
          ? '该投票状态已变化（可能已被其他管理员处理），请重新读取列表。'
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
        <h2 className={styles.title}>待审投票（{rows.length}）</h2>
        <span className={styles.hint}>
          后端只提供待审队列（游标分页、50/页）；处理动作：通过 / 驳回 / 关闭。待审投票不能投票或提前结束。
        </span>
      </div>
      {error ? <p className={styles.error} role="alert">{error}</p> : null}
      {notice ? <p className={styles.notice}>{notice}</p> : null}

      {rows.length === 0 ? (
        <p className={styles.hint}>当前没有待审投票。</p>
      ) : (
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>主题</th>
                <th>问题</th>
                <th>选项</th>
                <th>截止</th>
                <th>提交时间</th>
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
                  <td className={styles.question}>{item.question}</td>
                  <td>{item.maxChoices <= 1 ? '单选' : '最多 ' + item.maxChoices + ' 项'}</td>
                  <td>{formatDateTime(item.closesAt)}</td>
                  <td>{formatDateTime(item.createdAt)}</td>
                  <td className={styles.actions}>
                    <Button size="mini" type="text" loading={detailBusy} onClick={() => void openDetail(item)}>
                      详情
                    </Button>
                    <Button size="mini" type="text" onClick={() => { setAction({ item, kind: 'approve' }); setReason(''); setError(null); }}>
                      通过
                    </Button>
                    <Button size="mini" type="text" status="warning" onClick={() => { setAction({ item, kind: 'reject' }); setReason(''); setError(null); }}>
                      驳回
                    </Button>
                    <Button size="mini" type="text" status="danger" onClick={() => { setAction({ item, kind: 'close' }); setReason(''); setError(null); }}>
                      关闭
                    </Button>
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
        title={action ? ACTION_LABELS[action.kind] + '：主题 #' + action.item.threadId : '处理投票'}
        visible={Boolean(action)}
        confirmLoading={busy}
        onCancel={() => setAction(null)}
        onOk={() => void submit()}
        okText="确认"
        cancelText="取消"
      >
        {action ? (
          <div className={styles.form}>
            {error ? <p className={styles.error} role="alert">{error}</p> : null}
            <p className={styles.hint}>
              对象：主题 #{action.item.threadId}「{action.item.question}」
            </p>
            <p className={styles.hint}>
              后果：
              {action.kind === 'approve'
                ? '投票公开可见并开始计票，作者不能再编辑问题或选项。'
                : action.kind === 'reject'
                  ? '投票不会被公开，只有作者与投票管理员可见。'
                  : '结束后停止计票，结果保留可读；待审状态下关闭等同驳回。'}
            </p>
            <label className={styles.field}>
              原因{action.kind === 'approve' ? '（可选，≤500）' : '（必填，≤500）'}
              <Input value={reason} onChange={setReason} maxLength={500} placeholder="说明处理依据" />
            </label>
          </div>
        ) : null}
      </Modal>

      <Modal title="投票详情" visible={Boolean(detail)} footer={null} onCancel={() => setDetail(null)} style={{ width: 640 }}>
        {detail ? (
          <div>
            <p className={styles.question}>{detail.question}</p>
            <p className={styles.hint}>
              状态 {detail.state} · {detail.closed ? '已结束' : '截止 ' + formatDateTime(detail.closesAt)} ·{' '}
              {formatCount(detail.voters)} 人参与
            </p>
            <ul className={styles.options}>
              {detail.options.map((option) => (
                <li key={option.id}>
                  <span>{option.text}</span>
                  <span className={styles.hint}>{formatCount(option.votes)} 票</span>
                </li>
              ))}
            </ul>
            <p className={styles.hint}>结果匿名；管理操作只提供通过 / 驳回 / 关闭，不提供改票或改选项。</p>
          </div>
        ) : null}
      </Modal>
    </section>
  );
}
