'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, DatePicker, Input, InputNumber, Modal, Select, Switch } from '@arco-design/web-react';
import { browserRequest, browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { TitleDefinition, TitleJobRow, TitleLogRow, TitlePreview } from '@/lib/api/types';
import { LEVEL_BADGE_ICONS, TITLE_METRICS } from '@/lib/api/types';
import { formatDateTime } from '@/lib/format';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './TitleAdmin.module.css';

const STATUS_LABELS: Record<string, string> = { draft: '草稿', active: '已发布', paused: '暂停发放', disabled: '已停用' };
const MODE_LABELS: Record<string, string> = { automatic: '自动达成', manual: '手动授予' };

function metricLabel(metric: string): string {
  return TITLE_METRICS.find((m) => m.value === metric)?.label ?? metric;
}

function toLocalInput(iso: string | null): string | undefined {
  if (!iso) return undefined;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return undefined;
  const pad = (n: number) => String(n).padStart(2, '0');
  return (
    d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) + ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes()) + ':' + pad(d.getSeconds())
  );
}

function fromLocalInput(value: string | undefined): string | null {
  if (!value) return null;
  const d = new Date(value.replace(' ', 'T'));
  return Number.isNaN(d.getTime()) ? null : d.toISOString();
}

function emptyTitle(): TitleDefinition {
  return {
    id: '0',
    version: 0,
    name: '',
    description: '',
    badge: { label: '', icon: 'star', color: '#334155', background: '#e2e8f0' },
    status: 'draft',
    mode: 'automatic',
    match: 'all',
    conditions: [{ metric: 'threads_created', target: 1, forumId: '0' }],
    startsAt: null,
    endsAt: null,
    durationDays: 0,
    expiresAt: null,
    sort: 0,
  };
}

export interface TitleAdminProps {
  titles: TitleDefinition[];
  metrics: string[];
  forums: { id: string; name: string }[];
}

export function TitleAdmin({ titles, forums }: TitleAdminProps) {
  const router = useRouter();
  const [draft, setDraft] = useState<TitleDefinition | null>(null);
  const [preview, setPreview] = useState<TitlePreview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [panel, setPanel] = useState<{ title: TitleDefinition; jobs: TitleJobRow[]; logs: TitleLogRow[] } | null>(null);
  const [panelBusy, setPanelBusy] = useState(false);

  function openEditor(title?: TitleDefinition) {
    setDraft(title ? JSON.parse(JSON.stringify(title)) : emptyTitle());
    setPreview(null);
    setError(null);
    setNotice(null);
  }

  function patch(next: Partial<TitleDefinition>) {
    setDraft((prev) => (prev ? { ...prev, ...next } : prev));
    setPreview(null);
  }

  function patchCondition(index: number, next: Partial<TitleDefinition['conditions'][number]>) {
    if (!draft) return;
    patch({ conditions: draft.conditions.map((c, i) => (i === index ? { ...c, ...next } : c)) });
  }

  async function runPreview() {
    if (!draft) return;
    setBusy(true);
    setError(null);
    try {
      const result = await browserSend<TitlePreview>('/admin/titles/preview', { method: 'POST', body: draft });
      setPreview(result);
      setNotice('预览完成：符合条件 ' + result.eligible + ' 人，本次新增 ' + result.newAwards + ' 人。');
    } catch (caught) {
      const err = caught as ApiError;
      setError(err.status === 429 ? '预览过于频繁（每分钟最多 5 次），请稍后再试。' : err.message);
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  async function runSave() {
    if (!draft) return;
    setBusy(true);
    setError(null);
    try {
      const isCreate = draft.id === '0' || draft.version === 0;
      await browserSend('/admin/titles' + (isCreate ? '' : '/' + draft.id), {
        method: isCreate ? 'POST' : 'PUT',
        body: draft,
      });
      toastSuccess(isCreate ? '称号已创建（草稿）' : '称号已保存');
      setNotice(
        draft.status === 'active' && draft.mode === 'automatic'
          ? '已保存并发布：后端已入队补发任务，可在“任务”里查看进度。'
          : '已保存。',
      );
      setDraft(null);
      setPreview(null);
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      setError(
        err.status === 409
          ? '版本冲突：称号已被他人修改。你的输入仍然保留，请刷新列表后重新编辑。'
          : err.message,
      );
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  async function openPanel(title: TitleDefinition) {
    setPanelBusy(true);
    setPanel({ title, jobs: [], logs: [] });
    try {
      const [jobs, logs] = await Promise.all([
        browserRequest<TitleJobRow[]>('/admin/titles/' + title.id + '/jobs', { query: { page: 1 } }),
        browserRequest<TitleLogRow[]>('/admin/titles/' + title.id + '/logs', { query: { page: 1 } }),
      ]);
      setPanel({ title, jobs: jobs.data ?? [], logs: (logs.data ?? []).slice(0, 10) });
    } catch (caught) {
      toastError(caught as ApiError);
      setPanel(null);
    } finally {
      setPanelBusy(false);
    }
  }

  return (
    <section className={['panel', styles.card].join(' ')}>
      <div className={styles.head}>
        <h2 className={styles.title}>称号列表（{titles.length}）</h2>
        <Button size="small" type="primary" onClick={() => openEditor()}>
          新建称号
        </Button>
      </div>
      {error && !draft ? <p className={styles.error} role="alert">{error}</p> : null}
      {notice && !draft ? <p className={styles.notice}>{notice}</p> : null}

      {titles.length === 0 ? (
        <p className={styles.hint}>还没有称号。新建后默认是草稿，需要发布才会自动补发。</p>
      ) : (
        <div className={styles.tableWrap}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>称号</th>
                <th>状态</th>
                <th>方式</th>
                <th>条件</th>
                <th>版本</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {titles.map((title) => (
                <tr key={title.id}>
                  <td>
                    <span className={styles.badge} style={{ background: title.badge.background, color: title.badge.color }}>
                      {title.badge.label || title.name}
                    </span>
                    <span className={styles.name}>{title.name}</span>
                    {title.description ? <p className={styles.desc}>{title.description}</p> : null}
                  </td>
                  <td>{STATUS_LABELS[title.status] ?? title.status}</td>
                  <td>{MODE_LABELS[title.mode] ?? title.mode}</td>
                  <td className={styles.condCell}>
                    {title.mode === 'manual'
                      ? '手动授予'
                      : title.conditions.length === 0
                        ? '无'
                        : title.conditions
                            .map((c) => metricLabel(c.metric) + ' ≥ ' + c.target + (c.forumId !== '0' ? '（版块 ' + c.forumId + '）' : ''))
                            .join('；')}
                  </td>
                  <td>v{title.version}</td>
                  <td className={styles.actions}>
                    <Button size="mini" type="text" onClick={() => openEditor(title)}>
                      编辑
                    </Button>
                    <Button size="mini" type="text" loading={panelBusy} onClick={() => void openPanel(title)}>
                      任务 / 日志
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Modal
        title={draft && (draft.id === '0' || draft.version === 0) ? '新建称号' : '编辑称号'}
        visible={Boolean(draft)}
        style={{ width: 760 }}
        confirmLoading={busy}
        onCancel={() => setDraft(null)}
        onOk={() => void runSave()}
        okText="保存"
        cancelText="取消"
        footer={
          <div className={styles.modalFooter}>
            <span className={styles.hint}>发布状态 + 自动达成会触发后端补发任务。</span>
            <span>
              <Button onClick={() => setDraft(null)}>取消</Button>
              <Button type="secondary" loading={busy} onClick={() => void runPreview()}>
                预览
              </Button>
              <Button type="primary" loading={busy} onClick={() => void runSave()}>
                保存
              </Button>
            </span>
          </div>
        }
      >
        {draft ? (
          <div className={styles.form}>
            {error ? <p className={styles.error} role="alert">{error}</p> : null}
            {notice ? <p className={styles.notice}>{notice}</p> : null}
            {preview ? (
              <p className={styles.previewNote}>
                预览：符合条件 {preview.eligible} 人，新增发放 {preview.newAwards} 人（按版本 v{preview.version} 计算）。
              </p>
            ) : null}
            <div className={styles.grid}>
              <label className={styles.field}>
                名称（≤30 字）
                <Input value={draft.name} maxLength={30} onChange={(v) => patch({ name: v })} />
              </label>
              <label className={styles.field}>
                排序
                <InputNumber min={0} max={10000} value={draft.sort} onChange={(v) => patch({ sort: Number(v) || 0 })} />
              </label>
              <label className={styles.field}>
                状态
                <Select value={draft.status} onChange={(v) => patch({ status: v as string })} style={{ width: '100%' }}>
                  {Object.keys(STATUS_LABELS).map((key) => (
                    <Select.Option key={key} value={key}>
                      {STATUS_LABELS[key]}
                    </Select.Option>
                  ))}
                </Select>
              </label>
              <label className={styles.field}>
                发放方式
                <Select value={draft.mode} onChange={(v) => patch({ mode: v as string })} style={{ width: '100%' }}>
                  <Select.Option value="automatic">自动达成</Select.Option>
                  <Select.Option value="manual">手动授予</Select.Option>
                </Select>
              </label>
              <label className={styles.field}>
                条件匹配
                <Select value={draft.match} disabled={draft.mode === 'manual'} onChange={(v) => patch({ match: v as string })} style={{ width: '100%' }}>
                  <Select.Option value="all">全部满足</Select.Option>
                  <Select.Option value="any">任一满足</Select.Option>
                </Select>
              </label>
              <label className={styles.field}>
                有效期天数（0 = 永久）
                <InputNumber min={0} max={36500} value={draft.durationDays} onChange={(v) => patch({ durationDays: Number(v) || 0 })} />
              </label>
              <label className={styles.field}>
                开始时间（可空）
                <DatePicker
                  showTime
                  value={toLocalInput(draft.startsAt)}
                  onChange={(v) => patch({ startsAt: fromLocalInput(v) })}
                />
              </label>
              <label className={styles.field}>
                结束时间（可空）
                <DatePicker
                  showTime
                  value={toLocalInput(draft.endsAt)}
                  onChange={(v) => patch({ endsAt: fromLocalInput(v) })}
                />
              </label>
            </div>
            <label className={styles.field}>
              说明（≤500 字）
              <Input.TextArea value={draft.description} maxLength={500} rows={2} onChange={(v) => patch({ description: v })} />
            </label>

            <p className={styles.groupLabel}>徽章</p>
            <div className={styles.grid}>
              <label className={styles.field}>
                徽章文字
                <Input value={draft.badge.label} maxLength={20} onChange={(v) => patch({ badge: { ...draft.badge, label: v } })} />
              </label>
              <label className={styles.field}>
                图标
                <Select value={draft.badge.icon} onChange={(v) => patch({ badge: { ...draft.badge, icon: v as string } })} style={{ width: '100%' }}>
                  {LEVEL_BADGE_ICONS.map((icon) => (
                    <Select.Option key={icon || 'none'} value={icon}>
                      {icon || '（无）'}
                    </Select.Option>
                  ))}
                </Select>
              </label>
              <label className={styles.field}>
                文字色 #RRGGBB
                <Input value={draft.badge.color} onChange={(v) => patch({ badge: { ...draft.badge, color: v } })} />
              </label>
              <label className={styles.field}>
                背景色 #RRGGBB
                <Input value={draft.badge.background} onChange={(v) => patch({ badge: { ...draft.badge, background: v } })} />
              </label>
            </div>

            <p className={styles.groupLabel}>获取条件（{draft.conditions.length}/10）</p>
            {draft.mode === 'manual' ? (
              <p className={styles.hint}>手动授予的称号不能带条件。</p>
            ) : (
              <>
                {draft.conditions.map((condition, index) => {
                  const meta = TITLE_METRICS.find((m) => m.value === condition.metric);
                  return (
                    <div key={String(index)} className={styles.condRow}>
                      <Select
                        value={condition.metric}
                        onChange={(v) => patchCondition(index, { metric: v as string, forumId: '0' })}
                        style={{ width: 180 }}
                      >
                        {TITLE_METRICS.map((m) => (
                          <Select.Option key={m.value} value={m.value}>
                            {m.label}
                          </Select.Option>
                        ))}
                      </Select>
                      <label className={styles.inlineField}>
                        目标
                        <InputNumber
                          size="small"
                          min={1}
                          max={1e12}
                          value={condition.target}
                          onChange={(v) => patchCondition(index, { target: Number(v) || 1 })}
                        />
                      </label>
                      {meta?.forumScoped ? (
                        <Select
                          value={condition.forumId}
                          onChange={(v) => patchCondition(index, { forumId: v as string })}
                          style={{ width: 200 }}
                        >
                          <Select.Option value="0">全站</Select.Option>
                          {forums.map((forum) => (
                            <Select.Option key={forum.id} value={forum.id}>
                              {forum.name}
                            </Select.Option>
                          ))}
                        </Select>
                      ) : (
                        <span className={styles.hint}>该指标不支持版块范围</span>
                      )}
                      <Button size="mini" type="text" status="danger" onClick={() => patch({ conditions: draft.conditions.filter((_, i) => i !== index) })}>
                        删除
                      </Button>
                    </div>
                  );
                })}
                <Button size="small" type="text" disabled={draft.conditions.length >= 10} onClick={() => patch({ conditions: [...draft.conditions, { metric: 'threads_created', target: 1, forumId: '0' }] })}>
                  + 添加条件
                </Button>
                {draft.conditions.length === 0 ? <p className={styles.hint}>自动称号至少需要 1 个条件。</p> : null}
              </>
            )}
          </div>
        ) : null}
      </Modal>

      <Modal
        title={panel ? '称号任务与日志：' + panel.title.name : '称号任务与日志'}
        visible={Boolean(panel)}
        footer={null}
        style={{ width: 760 }}
        onCancel={() => setPanel(null)}
      >
        {panel ? (
          <div>
            <p className={styles.groupLabel}>补发任务</p>
            {panel.jobs.length === 0 ? (
              <p className={styles.hint}>没有任务记录（发布自动称号后才会生成）。</p>
            ) : (
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th>状态</th>
                    <th>规则版本</th>
                    <th>已处理</th>
                    <th>已发放</th>
                    <th>创建时间</th>
                  </tr>
                </thead>
                <tbody>
                  {panel.jobs.map((job) => (
                    <tr key={job.id}>
                      <td>{job.status}</td>
                      <td>v{job.version}</td>
                      <td>{job.processed}</td>
                      <td>{job.awarded}</td>
                      <td>{formatDateTime(job.createdAt)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            <p className={styles.groupLabel}>审计日志</p>
            {panel.logs.length === 0 ? (
              <p className={styles.hint}>没有日志。</p>
            ) : (
              <ul className={styles.logs}>
                {panel.logs.map((log) => (
                  <li key={log.id}>
                    <span className={styles.logAction}>{log.action}</span>
                    <span className={styles.hint}>
                      用户 {log.userId} · 操作者 {log.actorId} · {formatDateTime(log.createdAt)}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        ) : null}
      </Modal>
    </section>
  );
}
