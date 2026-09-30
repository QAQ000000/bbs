'use client';

import { useEffect, useRef, useState } from 'react';
import { Button, Input, Select, Switch } from '@arco-design/web-react';
import { browserGet } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { AdminDiagnosticsView, MemberDecisionView, MemberLogRow } from '@/lib/api/types';
import { MEMBER_ACTIONS } from '@/lib/api/types';
import { formatCount, formatDateTime } from '@/lib/format';
import { toastError } from '../ui/feedback';
import styles from './DiagnosticsPanel.module.css';

const REFRESH_MS = 30000;

const ACTION_LABELS: Record<string, string> = {
  'poll.create': '发起投票',
  'poll.vote': '参与投票',
  'bounty.create': '发布悬赏',
  'checkin.claim': '每日签到',
  'forum.read': '浏览版块',
  'thread.create': '发布主题',
  'post.reply': '回复主题',
  'post.edit': '编辑内容',
  'post.delete': '删除内容',
  'post.like': '点赞',
  'thread.favorite': '收藏',
  'post.report': '举报',
  'upload.image': '上传图片',
  'upload.file': '上传附件',
  'attachment.download': '下载附件',
  'post.link.direct': '直接外链',
  'post.skip.moderate': '免审核发布',
};

function issues(data: AdminDiagnosticsView): string[] {
  const out: string[] = [];
  if (data.lockWaits > 0) out.push('存在 ' + data.lockWaits + ' 个锁等待查询');
  if (data.forumStats.retrying > 0) out.push('版块统计队列有 ' + data.forumStats.retrying + ' 个重试中事件');
  if (data.searchIndex.retrying > 0) out.push('搜索索引队列有 ' + data.searchIndex.retrying + ' 个重试中事件');
  if (data.forumStats.pending > 0 && data.forumStats.oldestAgeSeconds > 300) {
    out.push('版块统计队列积压超过 5 分钟（最旧 ' + Math.round(data.forumStats.oldestAgeSeconds) + ' 秒）');
  }
  if (data.searchIndex.pending > 0 && data.searchIndex.oldestAgeSeconds > 300) {
    out.push('搜索索引队列积压超过 5 分钟（最旧 ' + Math.round(data.searchIndex.oldestAgeSeconds) + ' 秒）');
  }
  if (data.databaseWorkload.cacheHitRatio < 0.9) {
    out.push('数据库缓存命中率偏低：' + (data.databaseWorkload.cacheHitRatio * 100).toFixed(1) + '%');
  }
  if (data.databasePool.max > 0 && data.databasePool.acquired >= data.databasePool.max) {
    out.push('连接池已占满（' + data.databasePool.acquired + '/' + data.databasePool.max + '）');
  }
  return out;
}

export interface DiagnosticsPanelProps {
  initial: AdminDiagnosticsView | null;
  initialError: boolean;
  logs: MemberLogRow[];
}

export function DiagnosticsPanel({ initial, initialError, logs }: DiagnosticsPanelProps) {
  const [data, setData] = useState<AdminDiagnosticsView | null>(initial);
  const [failed, setFailed] = useState(initialError);
  const [auto, setAuto] = useState(false);
  const [busy, setBusy] = useState(false);
  const [refreshedAt, setRefreshedAt] = useState<string | null>(null);
  const mounted = useRef(true);
  const [uid, setUid] = useState('');
  const [action, setAction] = useState('thread.create');
  const [forumId, setForumId] = useState('');
  const [decision, setDecision] = useState<MemberDecisionView | null>(null);
  const [queryError, setQueryError] = useState<string | null>(null);
  const [querying, setQuerying] = useState(false);

  async function refresh() {
    setBusy(true);
    try {
      const next = await browserGet<AdminDiagnosticsView>('/admin/diagnostics');
      if (!mounted.current) return;
      setData(next);
      setFailed(false);
      setRefreshedAt(new Date().toISOString());
    } catch (caught) {
      if (!mounted.current) return;
      setFailed(true);
      toastError(caught as ApiError);
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    if (!auto) return;
    // 有限频率自动刷新；页面隐藏或离开时停止，避免持续无效请求。
    const timer = setInterval(() => {
      if (document.hidden) return;
      void refresh();
    }, REFRESH_MS);
    return () => clearInterval(timer);
  }, [auto]);

  async function runDiagnose() {
    if (!/^[1-9][0-9]*$/.test(uid.trim())) {
      setQueryError('请输入有效的用户 ID（正整数）。');
      return;
    }
    setQuerying(true);
    setQueryError(null);
    try {
      const result = await browserGet<MemberDecisionView>('/admin/membership/diagnose', {
        query: { userId: uid.trim(), action, forumId: forumId.trim() || undefined },
      });
      setDecision(result);
    } catch (caught) {
      const err = caught as ApiError;
      setDecision(null);
      setQueryError(err.status === 403 ? '缺少 membership.view 权限：' + err.message : err.message);
    } finally {
      setQuerying(false);
    }
  }

  const findings = data ? issues(data) : [];

  return (
    <div className={styles.wrap}>
      <section className={['panel', styles.card].join(' ')}>
        <div className={styles.head}>
          <h2 className={styles.title}>系统诊断</h2>
          <div className={styles.headActions}>
            <label className={styles.autoToggle}>
              <Switch size="small" checked={auto} onChange={setAuto} />
              每 {REFRESH_MS / 1000} 秒自动刷新（离开或页面隐藏时停止）
            </label>
            <Button size="small" type="secondary" loading={busy} onClick={() => void refresh()}>
              重新读取
            </Button>
          </div>
        </div>
        {refreshedAt ? <p className={styles.hint}>最近读取：{formatDateTime(refreshedAt)}</p> : null}

        {failed ? (
          <p className={styles.error} role="alert">
            诊断接口读取失败：这表示“没读到”，不表示指标为零。请稍后重新读取。
          </p>
        ) : !data ? (
          <p className={styles.hint}>没有诊断数据。</p>
        ) : (
          <>
            {findings.length > 0 ? (
              <div className={styles.findings}>
                <p className={styles.findingTitle}>后端明确报告的异常</p>
                <ul>
                  {findings.map((line) => (
                    <li key={line}>{line}</li>
                  ))}
                </ul>
              </div>
            ) : (
              <p className={styles.ok}>未发现队列积压、锁等待、连接池耗尽或缓存命中率异常。</p>
            )}
            <div className={styles.grid}>
              <dl className={styles.meta}>
                <div><dt>连接池 max</dt><dd>{formatCount(data.databasePool.max)}</dd></div>
                <div><dt>已用 / 空闲</dt><dd>{data.databasePool.acquired} / {data.databasePool.idle}</dd></div>
                <div><dt>获取次数</dt><dd>{formatCount(data.databasePool.acquireCount)}</dd></div>
                <div><dt>获取总耗时</dt><dd>{formatCount(data.databasePool.acquireDurationMs)} ms</dd></div>
                <div><dt>空获取 / 取消</dt><dd>{data.databasePool.emptyAcquireCount} / {data.databasePool.canceledAcquireCount}</dd></div>
                <div><dt>锁等待</dt><dd>{formatCount(data.lockWaits)}</dd></div>
              </dl>
              <dl className={styles.meta}>
                <div><dt>事务数</dt><dd>{formatCount(data.databaseWorkload.transactions)}</dd></div>
                <div><dt>读 / 命中块</dt><dd>{formatCount(data.databaseWorkload.readIO)} / {formatCount(data.databaseWorkload.hitIO)}</dd></div>
                <div><dt>缓存命中率</dt><dd>{(data.databaseWorkload.cacheHitRatio * 100).toFixed(1)}%</dd></div>
                <div><dt>版块统计队列</dt><dd>{data.forumStats.pending} 待处理 / {data.forumStats.retrying} 重试</dd></div>
                <div><dt>搜索索引队列</dt><dd>{data.searchIndex.pending} 待处理 / {data.searchIndex.retrying} 重试</dd></div>
                <div><dt>异步发布</dt><dd>{data.forumStats.asyncPublication ? '开启' : '关闭'}</dd></div>
              </dl>
            </div>
          </>
        )}
      </section>

      <section className={['panel', styles.card].join(' ')}>
        <div className={styles.head}>
          <h2 className={styles.title}>会员权限判定诊断</h2>
          <span className={styles.hint}>只读：按后端真实规则回放一次判定，不修改任何数据。</span>
        </div>
        <div className={styles.queryRow}>
          <Input value={uid} onChange={setUid} placeholder="用户 ID" style={{ width: 140 }} />
          <Select value={action} onChange={(v) => setAction(v as string)} style={{ width: 200 }}>
            {MEMBER_ACTIONS.map((item) => (
              <Select.Option key={item} value={item}>
                {ACTION_LABELS[item] ?? item}
              </Select.Option>
            ))}
          </Select>
          <Input value={forumId} onChange={setForumId} placeholder="版块 ID（可选）" style={{ width: 160 }} />
          <Button size="small" type="secondary" loading={querying} onClick={() => void runDiagnose()}>
            诊断
          </Button>
        </div>
        {queryError ? <p className={styles.error} role="alert">{queryError}</p> : null}
        {decision ? (
          <dl className={styles.meta}>
            <div><dt>动作</dt><dd>{decision.action}</dd></div>
            <div><dt>结论</dt><dd className={decision.allowed ? styles.ok : styles.bad}>{decision.allowed ? '允许' : '拒绝'}</dd></div>
            <div><dt>原因</dt><dd>{decision.reason}</dd></div>
            <div><dt>额度</dt><dd>{decision.limit < 0 ? '不限' : decision.limit + '，已用 ' + decision.used}</dd></div>
          </dl>
        ) : null}
        <p className={styles.hint}>
          诊断结果来自后端 memberDecision，包含版块可见性、封禁、等级权限、额度与主题状态。
        </p>
      </section>

      <section className={['panel', styles.card].join(' ')}>
        <div className={styles.head}>
          <h2 className={styles.title}>会员审计日志（最近 30 条）</h2>
          <span className={styles.hint}>读取为主；本页没有任何修复或改数据的入口。</span>
        </div>
        {logs.length === 0 ? (
          <p className={styles.hint}>没有审计记录。</p>
        ) : (
          <div className={styles.tableWrap}>
            <table className={styles.table}>
              <thead>
                <tr><th>动作</th><th>用户</th><th>操作者</th><th>时间</th></tr>
              </thead>
              <tbody>
                {logs.map((log) => (
                  <tr key={log.id}>
                    <td>{log.action}</td>
                    <td>#{log.userId}</td>
                    <td>#{log.actorId}</td>
                    <td>{formatDateTime(log.createdAt)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
