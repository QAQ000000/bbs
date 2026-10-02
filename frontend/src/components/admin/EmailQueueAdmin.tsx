'use client';

import { useEffect, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { Button, Modal } from '@arco-design/web-react';
import { browserGet, browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { EmailJobView } from '@/lib/api/types';
import { formatCount, formatDateTime } from '@/lib/format';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './EmailQueueAdmin.module.css';

const STATUS_LABELS: Record<string, string> = {
  pending: '待发送',
  sending: '发送中',
  sent: '已发送',
  dead: '已失败（可重试）',
  cancelled: '已取消',
};

const FILTERS = [
  { value: '', label: '全部' },
  { value: 'pending', label: '待发送' },
  { value: 'sending', label: '发送中' },
  { value: 'sent', label: '已发送' },
  { value: 'dead', label: '已失败' },
  { value: 'cancelled', label: '已取消' },
];

export interface EmailQueueAdminProps {
  items: EmailJobView[];
  counts: Record<string, number>;
  nextBefore: string;
  status: string;
  smtpEnabled: boolean;
}

export function EmailQueueAdmin({ items, counts, nextBefore, status, smtpEnabled }: EmailQueueAdminProps) {
  const router = useRouter();
  const [rows, setRows] = useState<EmailJobView[]>(items);
  const [cursor, setCursor] = useState(nextBefore);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [detail, setDetail] = useState<EmailJobView | null>(null);
  const [detailId, setDetailId] = useState<string | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);
  const detailRequest = useRef(0);

  useEffect(() => {
    setRows(items);
    setCursor(nextBefore);
  }, [items, nextBefore]);

  async function readDetail(job: EmailJobView) {
    const request = ++detailRequest.current;
    setDetailId(job.id);
    setDetail(null);
    setDetailError(null);
    try {
      const latest = await browserGet<EmailJobView>('/admin/email-jobs/' + job.id);
      if (request === detailRequest.current) {
        setDetail(latest);
        setRows((prev) => prev.map((row) => row.id === latest.id ? latest : row));
      }
    } catch (caught) {
      if (request === detailRequest.current) setDetailError((caught as ApiError).message);
    }
  }

  function cancel(job: EmailJobView) {
    Modal.confirm({
      title: '取消邮件任务 #' + job.id + '？',
      style: { width: 420, maxWidth: 'calc(100vw - 32px)' },
      content: '取消后不会投递，也不能重新排队。已进入发送流程的任务无法取消。',
      okText: '确认取消', cancelText: '保留任务',
      onOk: async () => {
        setBusy(true);
        setError(null);
        setNotice(null);
        try {
          await browserSend('/admin/email-jobs/' + job.id + '/cancel', { method: 'POST', body: { version: job.version } });
          setNotice('邮件任务 #' + job.id + ' 已取消。');
          setDetailId(null);
          ++detailRequest.current;
          router.refresh();
        } catch (caught) {
          const err = caught as ApiError;
          setError(err.code === 'EMAIL_CANCEL_CONFLICT' ? '任务状态已变化，请重新读取后确认。' : '取消结果未确认：' + err.message + '。请先重新读取任务状态。');
          router.refresh();
        } finally {
          setBusy(false);
        }
      },
    });
  }

  async function loadMore() {
    if (!cursor || busy) return;
    setBusy(true);
    try {
      const res = await fetch('/api/v1/admin/email-jobs?before=' + encodeURIComponent(cursor) + (status ? '&status=' + status : ''), {
        credentials: 'same-origin',
        cache: 'no-store',
      });
      const body = await res.json();
      if (!res.ok) throw ApiError.fromBody(res.status, body);
      const next: EmailJobView[] = body.data?.items ?? [];
      setRows((prev) => {
        const seen = new Set(prev.map((item) => item.id));
        return prev.concat(next.filter((item) => !seen.has(item.id)));
      });
      setCursor(body.data?.nextBefore ?? '');
    } catch (caught) {
      toastError(caught as ApiError);
    } finally {
      setBusy(false);
    }
  }

  async function retry(job: EmailJobView) {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      await browserSend('/admin/email-jobs/' + job.id + '/retry', { method: 'POST', body: { version: job.version } });
      toastSuccess('已重新加入队列');
      setNotice('邮件任务 #' + job.id + ' 已重新排队；请重新读取列表确认状态。');
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      // 超时不自动重试：提示先重新读取确认实际状态。
      setError(
        err.code === 'SMTP_DISABLED'
          ? '尚未配置邮件服务，不能重试投递。'
          : err.code === 'EMAIL_RETRY_CONFLICT'
            ? '任务状态已变化（可能已被处理或已过期），请重新读取列表。'
            : '重试请求未成功：' + err.message + '。请先重新读取列表确认任务实际状态，再决定是否再次重试。',
      );
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className={['panel', styles.card].join(' ')}>
      <div className={styles.head}>
        <h2 className={styles.title}>邮件队列（{rows.length}）</h2>
        <span className={styles.hint}>
          收件人、主题关联与令牌字段由接口标记为不返回，页面不会展示或记录这些内容。
        </span>
      </div>

      {!smtpEnabled ? (
        <p className={styles.error} role="status">
          邮件服务未配置（SMTP_DISABLED）：队列仍会累积，但重试会被后端拒绝，也不会真正投递。
        </p>
      ) : null}
      {error ? <p className={styles.error} role="alert">{error}</p> : null}
      {notice ? <p className={styles.notice}>{notice}</p> : null}

      <div className={styles.filters}>
        {FILTERS.map((filter) => (
          <Link
            key={filter.value || 'all'}
            className={filter.value === status ? styles.filterActive : styles.filter}
            href={'/admin/notifications' + (filter.value ? '?status=' + filter.value : '')}
          >
            {filter.label}
            <span className={styles.count}>{formatCount(filter.value ? counts[filter.value] ?? 0 : Object.values(counts).reduce((sum, count) => sum + count, 0))}</span>
          </Link>
        ))}
      </div>

      {error ? (
        <div className={styles.actions}>
          <Button size="small" type="secondary" onClick={() => router.refresh()}>
            重新读取
          </Button>
        </div>
      ) : null}

      {rows.length === 0 ? (
        <p className={styles.hint}>当前筛选下没有邮件任务。</p>
      ) : (
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>任务</th>
                <th>类型</th>
                <th>用户</th>
                <th>状态</th>
                <th>尝试 / 重试</th>
                <th>下次尝试 / 发送</th>
                <th>最后错误</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((job) => (
                <tr key={job.id}>
                  <td>#{job.id}</td>
                  <td>{job.kind}</td>
                  <td>#{job.userId}</td>
                  <td>{STATUS_LABELS[job.status] ?? job.status}</td>
                  <td>{job.attempts} / {job.retries}</td>
                  <td className={styles.time}>
                    {job.sentAt ? '发送 ' + formatDateTime(job.sentAt) : '下次 ' + formatDateTime(job.nextAttemptAt)}
                  </td>
                  <td className={styles.errorCell}>{job.lastError || '无'}</td>
                  <td className={styles.actions}>
                    <Button size="mini" type="text" disabled={busy} onClick={() => void readDetail(job)}>
                      详情
                    </Button>
                    {job.status === 'dead' ? (
                      <Button size="mini" type="text" loading={busy} disabled={!smtpEnabled} onClick={() => void retry(job)}>
                        重试
                      </Button>
                    ) : (
                      <span className={styles.hint}>不可重试</span>
                    )}
                    {job.status === 'pending' || job.status === 'dead' ? <Button size="mini" type="text" status="danger" disabled={busy} onClick={() => cancel(job)}>取消</Button> : null}
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

      <Modal title={'邮件任务 #' + (detailId ?? '')} visible={Boolean(detailId)} footer={null} onCancel={() => { setDetailId(null); ++detailRequest.current; }} style={{ width: 620, maxWidth: 'calc(100vw - 32px)' }}>
        {!detail && !detailError ? <p role="status">正在读取任务…</p> : null}
        {detailError ? <p className={styles.error} role="alert">{detailError}</p> : null}
        {detail ? (
          <dl className={styles.detail}>
            <div><dt>任务 ID</dt><dd>{detail.id}</dd></div>
            <div><dt>类型</dt><dd>{detail.kind}</dd></div>
            <div><dt>用户</dt><dd>#{detail.userId}</dd></div>
            <div><dt>状态</dt><dd>{STATUS_LABELS[detail.status] ?? detail.status}</dd></div>
            <div><dt>尝试 / 重试</dt><dd>{detail.attempts} / {detail.retries}</dd></div>
            <div><dt>版本</dt><dd>v{detail.version}</dd></div>
            <div><dt>创建</dt><dd>{formatDateTime(detail.createdAt)}</dd></div>
            <div><dt>下次尝试</dt><dd>{formatDateTime(detail.nextAttemptAt)}</dd></div>
            <div><dt>过期</dt><dd>{formatDateTime(detail.expiresAt)}</dd></div>
            <div><dt>发送</dt><dd>{detail.sentAt ? formatDateTime(detail.sentAt) : '未发送'}</dd></div>
            <div className={styles.full}><dt>最后错误</dt><dd>{detail.lastError || '无'}</dd></div>
          </dl>
        ) : null}
        {detail && (detail.status === 'pending' || detail.status === 'dead') ? <Button status="danger" disabled={busy} onClick={() => cancel(detail)}>取消任务</Button> : null}
        <p className={styles.hint}>接口不返回收件人地址、关联楼层与邮件令牌；此处也不写入前端日志。</p>
      </Modal>
    </section>
  );
}
