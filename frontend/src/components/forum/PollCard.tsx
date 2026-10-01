'use client';

import { useState } from 'react';
import { Button, Input, InputNumber } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { EngagementRules, PollView } from '@/lib/api/types';
import { formatCount, formatDateTime, isEmptyTime } from '@/lib/format';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './EngagementCard.module.css';

export interface PollCardProps {
  threadId: string;
  poll: PollView | null;
  rules: EngagementRules['poll'] | null;
  canCreate: boolean;
  canVote: boolean;
  canClose: boolean;
  loggedIn: boolean;
  isAuthor: boolean;
}

const STATE_LABELS: Record<string, string> = {
  pending: '待审核',
  published: '进行中',
  closed: '已结束',
  rejected: '已驳回',
};

function isClosed(poll: PollView): boolean {
  if (poll.closed || poll.state === 'closed' || poll.state === 'rejected') return true;
  return !isEmptyTime(poll.closesAt) && Date.parse(poll.closesAt) <= Date.now();
}

export function PollCard({ threadId, poll, rules, canCreate, canVote, canClose, loggedIn, isAuthor }: PollCardProps) {
  const [current, setCurrent] = useState<PollView | null>(poll);
  const [selected, setSelected] = useState<number[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // 创建表单
  const [showCreate, setShowCreate] = useState(false);
  const [question, setQuestion] = useState('');
  const [options, setOptions] = useState<string[]>(['', '']);
  const [maxChoices, setMaxChoices] = useState(1);
  const [durationHours, setDurationHours] = useState(24);

  const maxOptions = rules?.maxOptions ?? 10;
  const maxDays = rules?.maxDays ?? 30;

  function toggleChoice(id: number) {
    if (!current) return;
    const single = current.maxChoices <= 1;
    if (single) {
      setSelected([id]);
      return;
    }
    setSelected((prev) => {
      if (prev.includes(id)) return prev.filter((v) => v !== id);
      if (prev.length >= current.maxChoices) return prev;
      return [...prev, id];
    });
  }

  async function vote() {
    if (!current || selected.length === 0) return;
    setBusy(true);
    setError(null);
    try {
      const updated = await browserSend<PollView>('/threads/' + threadId + '/poll/vote', {
        method: 'PUT',
        body: { optionIds: selected },
      });
      setCurrent(updated);
      setSelected([]);
      toastSuccess('投票已提交');
    } catch (caught) {
      const err = caught as ApiError;
      setError(err.message);
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  async function close() {
    setBusy(true);
    setError(null);
    try {
      const updated = await browserSend<PollView>('/threads/' + threadId + '/poll/close', { method: 'POST' });
      setCurrent(updated);
      toastSuccess('投票已关闭');
    } catch (caught) {
      setError((caught as ApiError).message);
      toastError(caught as ApiError);
    } finally {
      setBusy(false);
    }
  }

  async function create(event: React.FormEvent) {
    event.preventDefault();
    const cleaned = options.map((o) => o.trim()).filter(Boolean);
    if (!question.trim()) {
      setError('请输入投票问题');
      return;
    }
    if (cleaned.length < 2) {
      setError('至少需要 2 个选项');
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const created = await browserSend<PollView>('/threads/' + threadId + '/poll', {
        method: 'POST',
        body: { question: question.trim(), options: cleaned, maxChoices, durationHours },
      });
      setCurrent(created);
      setShowCreate(false);
      toastSuccess('投票已创建');
    } catch (caught) {
      setError((caught as ApiError).message);
      toastError(caught as ApiError);
    } finally {
      setBusy(false);
    }
  }

  // 无投票：仅在允许创建时显示入口
  if (!current) {
    if (!canCreate) return null;
    if (rules && !rules.enabled) {
      return (
        <section className={['panel', styles.card].join(' ')} aria-label="投票">
          <p className={styles.muted}>站点已停用投票功能。</p>
        </section>
      );
    }
    if (!showCreate) {
      return (
        <section className={['panel', styles.card].join(' ')} aria-label="投票">
          <div className={styles.cardHead}>
            <h2 className={styles.cardTitle}>投票</h2>
            <Button size="small" type="outline" onClick={() => setShowCreate(true)}>
              创建投票
            </Button>
          </div>
          <p className={styles.muted}>本主题还没有投票。每个主题最多一个，创建后不可编辑或重开。</p>
        </section>
      );
    }
    return (
      <section className={['panel', styles.card].join(' ')} aria-label="创建投票">
        <div className={styles.cardHead}>
          <h2 className={styles.cardTitle}>创建投票</h2>
        </div>
        {error ? <p className={styles.error}>{error}</p> : null}
        <form onSubmit={create} className={styles.form}>
          <label className={styles.label} htmlFor="poll-question">
            问题
          </label>
          <Input id="poll-question" value={question} onChange={setQuestion} maxLength={200} showWordLimit placeholder="一句话说明要投票的问题" />
          <label className={styles.label}>选项（2 - {maxOptions} 个）</label>
          {options.map((value, index) => (
            <div key={index} className={styles.optionRow}>
              <Input
                value={value}
                onChange={(v) => setOptions((prev) => prev.map((item, i) => (i === index ? v : item)))}
                maxLength={200}
                placeholder={'选项 ' + (index + 1)}
              />
              {options.length > 2 ? (
                <button
                  type="button"
                  className={styles.removeOption}
                  onClick={() => setOptions((prev) => prev.filter((_, i) => i !== index))}
                >
                  删除
                </button>
              ) : null}
            </div>
          ))}
          {options.length < maxOptions ? (
            <Button size="small" type="text" onClick={() => setOptions((prev) => [...prev, ''])}>
              + 添加选项
            </Button>
          ) : null}
          <div className={styles.inlineFields}>
            <div>
              <label className={styles.label} htmlFor="poll-choices">
                每人可选项
              </label>
              <InputNumber
                id="poll-choices"
                min={1}
                max={Math.max(2, options.filter((o) => o.trim()).length)}
                value={maxChoices}
                onChange={(v) => setMaxChoices(Number(v) || 1)}
              />
            </div>
            <div>
              <label className={styles.label} htmlFor="poll-hours">
                有效小时数（≤ {maxDays * 24}）
              </label>
              <InputNumber
                id="poll-hours"
                min={1}
                max={maxDays * 24}
                value={durationHours}
                onChange={(v) => setDurationHours(Number(v) || 24)}
              />
            </div>
          </div>
          <p className={styles.muted}>单选将 maxChoices 设为 1；创建后不可编辑、不可重开。</p>
          <div className={styles.actions}>
            <Button type="primary" htmlType="submit" loading={busy}>
              创建
            </Button>
            <Button type="text" onClick={() => setShowCreate(false)}>
              取消
            </Button>
          </div>
        </form>
      </section>
    );
  }

  const closed = isClosed(current);
  const hasVoted = current.myChoices.length > 0;
  const showResults = closed || hasVoted || current.state !== 'published';
  const limited = current.maxChoices > 1;
  const choosing = !showResults && canVote && loggedIn;

  return (
    <section className={['panel', styles.card].join(' ')} aria-label="投票">
      <div className={styles.cardHead}>
        <h2 className={styles.cardTitle}>投票</h2>
        <span className={styles.stateBadge}>{closed && current.state === 'published' ? '已结束' : STATE_LABELS[current.state] ?? current.state}</span>
      </div>
      <p className={styles.question}>{current.question}</p>
      {error ? <p className={styles.error}>{error}</p> : null}
      {current.state === 'pending' || current.state === 'rejected' ? (
        <p className={styles.muted}>
          {current.state === 'pending' ? '投票正在审核中，仅作者与投票管理员可见。' : '投票未通过审核，不会公开显示。'}
        </p>
      ) : null}

      <ul className={styles.optionList}>
        {current.options.map((option) => {
          const pct = current.voters > 0 ? Math.round((option.votes / current.voters) * 100) : 0;
          const picked = current.myChoices.includes(option.id);
          return (
            <li key={option.id} className={styles.optionItem}>
              {choosing ? (
                <label className={styles.choice}>
                  <input
                    type={limited ? 'checkbox' : 'radio'}
                    name={'poll-' + current.threadId}
                    checked={selected.includes(option.id)}
                    onChange={() => toggleChoice(option.id)}
                  />
                  <span>{option.text}</span>
                </label>
              ) : (
                <div className={styles.resultRow}>
                  <span className={picked ? styles.resultTextPicked : styles.resultText}>{option.text}</span>
                  <span className={styles.resultMeta}>
                    {formatCount(option.votes)} 票 · {pct}%
                  </span>
                </div>
              )}
              {!choosing ? (
                <div className={styles.bar} aria-hidden="true">
                  <span className={styles.barFill} style={{ width: pct + '%' }} />
                </div>
              ) : null}
            </li>
          );
        })}
      </ul>

      <div className={styles.cardFoot}>
        <span className={styles.muted}>
          {formatCount(current.voters)} 人参与 ·{' '}
          {closed ? '已结束' : '截止 ' + formatDateTime(current.closesAt)}
          {limited ? ' · 最多选 ' + current.maxChoices + ' 项' : ' · 单选'}
        </span>
        <div className={styles.actions}>
          {choosing && selected.length > 0 ? (
            <Button size="small" type="primary" loading={busy} onClick={() => void vote()}>
              提交投票
            </Button>
          ) : null}
          {!closed && canClose && current.state === 'published' ? (
            <Button size="small" type="secondary" loading={busy} onClick={() => void close()}>
              提前结束
            </Button>
          ) : null}
          {!choosing && hasVoted ? <span className={styles.picked}>你已投过票</span> : null}
        </div>
      </div>
      {!loggedIn && !closed && current.state === 'published' ? (
        <p className={styles.muted}>登录后可参与投票。结果对可读该主题的人匿名可见。</p>
      ) : null}
      {isAuthor && !closed && current.state === 'published' ? (
        <p className={styles.muted}>作者可提前结束投票；投票创建后不可编辑或重开。</p>
      ) : null}
    </section>
  );
}
