'use client';

import { useState } from 'react';
import { Button, Input, InputNumber, Modal, Select, Switch } from '@arco-design/web-react';
import { browserGet, browserRequest, browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { ExperienceEntry, MemberAdjustment, MemberStateView } from '@/lib/api/types';
import { formatDateTime } from '@/lib/format';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './MemberLookup.module.css';

export interface MemberLookupProps {
  levels: { id: number; name: string; rank: number }[];
}

function newKey(uid: string): string {
  return 'member-adjust-' + uid + '-' + Date.now().toString(36);
}

export function MemberLookup({ levels }: MemberLookupProps) {
  const [uid, setUid] = useState('');
  const [member, setMember] = useState<MemberStateView | null>(null);
  const [history, setHistory] = useState<ExperienceEntry[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  // 调整表单
  const [changeLevel, setChangeLevel] = useState(false);
  const [levelId, setLevelId] = useState(0);
  const [changeLock, setChangeLock] = useState(false);
  const [locked, setLocked] = useState(false);
  const [delta, setDelta] = useState(0);
  const [reason, setReason] = useState('');
  const [key, setKey] = useState('');

  async function load(target?: string) {
    const id = (target ?? uid).trim();
    if (!/^[1-9][0-9]*$/.test(id)) {
      setError('请输入有效的用户 ID（正整数）。');
      return;
    }
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const state = await browserGet<MemberStateView>('/admin/membership/users/' + id);
      setMember(state);
      setUid(id);
      setLevelId(state.levelId);
      setLocked(state.locked);
      setChangeLevel(false);
      setChangeLock(false);
      setDelta(0);
      setReason('');
      setKey(newKey(id));
      const page = await browserRequest<ExperienceEntry[]>('/admin/membership/users/' + id + '/experience', {
        query: { page: 1 },
      });
      setHistory((page.data ?? []).slice(0, 10));
    } catch (caught) {
      const err = caught as ApiError;
      setMember(null);
      setHistory([]);
      setError(err.status === 403 ? '缺少查看会员资料的权限：' + err.message : err.message);
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  function targetExperience(): number {
    return (member?.experience ?? 0) + (delta || 0);
  }

  function targetLevelName(): string {
    if (!changeLevel) return member?.level?.name ?? '';
    const found = levels.find((l) => l.id === levelId);
    return found ? found.name : String(levelId);
  }

  async function submit() {
    if (!member) return;
    if (reason.trim().length === 0) {
      setError('请填写调整原因（会写入审计）。');
      return;
    }
    if (key.trim().length < 8) {
      setError('幂等键至少 8 个字符。');
      return;
    }
    if (!changeLevel && !changeLock && !delta) {
      setError('请至少修改等级、锁定状态或经验值中的一项。');
      return;
    }
    const changes: string[] = [];
    if (changeLevel) changes.push('等级 ' + member.level.name + ' → ' + targetLevelName());
    if (changeLock) changes.push('锁定 ' + (member.locked ? '是' : '否') + ' → ' + (locked ? '是' : '否'));
    if (delta) changes.push('经验 ' + member.experience + ' → ' + targetExperience() + '（' + (delta > 0 ? '+' : '') + delta + '）');

    Modal.confirm({
      title: '确认人工调整',
      content: (
        <div>
          <p className={styles.confirmLine}>用户 #{member.userId}（当前 {member.level.name} · 经验 {member.experience}）</p>
          <ul className={styles.confirmList}>
            {changes.map((line) => (
              <li key={line}>{line}</li>
            ))}
          </ul>
          <p className={styles.confirmLine}>原因：{reason}</p>
          <p className={styles.confirmLine}>升级与补发由后端执行；调整不可绕过额度与结算规则。</p>
        </div>
      ),
      okText: '确认调整',
      cancelText: '取消',
      onOk: async () => {
        const body: MemberAdjustment = {
          version: member.version,
          delta: delta || 0,
          reason: reason.trim(),
          key: key.trim(),
        };
        if (changeLevel) body.levelId = levelId;
        if (changeLock) body.locked = locked;
        setBusy(true);
        setError(null);
        try {
          const updated = await browserSend<MemberStateView>('/admin/membership/users/' + member.userId, {
            method: 'PATCH',
            body,
          });
          setMember(updated);
          setNotice('调整已生效：' + changes.join('；'));
          setDelta(0);
          setReason('');
          setChangeLevel(false);
          setChangeLock(false);
          setKey(newKey(member.userId));
          toastSuccess('已调整');
          await load(member.userId);
        } catch (caught) {
          const err = caught as ApiError;
          setError(err.status === 403 ? '缺少调整权限：' + err.message : err.message);
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
        <h2 className={styles.title}>用户会员状态与人工调整</h2>
        <span className={styles.hint}>经验决定等级；积分是独立账本，本页不修改积分。</span>
      </div>
      <div className={styles.searchRow}>
        <Input value={uid} onChange={setUid} placeholder="用户 ID" style={{ width: 160 }} onPressEnter={() => void load()} />
        <Button size="small" type="secondary" loading={busy} onClick={() => void load()}>
          查询
        </Button>
      </div>
      {error ? <p className={styles.error} role="alert">{error}</p> : null}
      {notice ? <p className={styles.notice}>{notice}</p> : null}

      {member ? (
        <>
          <dl className={styles.state}>
            <div>
              <dt>用户</dt>
              <dd>#{member.userId}</dd>
            </div>
            <div>
              <dt>等级</dt>
              <dd>{member.level.name}（ID {member.levelId}）</dd>
            </div>
            <div>
              <dt>经验</dt>
              <dd>{member.experience}</dd>
            </div>
            <div>
              <dt>下一级</dt>
              <dd>{member.nextLevel ? member.nextLevel.name + ' · ' + member.nextLevel.experience : '已是最高级'}</dd>
            </div>
            <div>
              <dt>经验版本</dt>
              <dd>v{member.version}</dd>
            </div>
            <div>
              <dt>锁定 / 受限</dt>
              <dd>{member.locked ? '已锁定' : '未锁定'} · {member.restricted ? '受限' : '正常'}</dd>
            </div>
            <div>
              <dt>活跃天数 / 阅读</dt>
              <dd>{member.daysVisited} / {member.postsRead}</dd>
            </div>
            <div>
              <dt>发帖 / 邮箱验证</dt>
              <dd>{member.postCount} · {member.emailVerified ? '已验证' : '未验证'}</dd>
            </div>
          </dl>

          <p className={styles.groupLabel}>人工调整</p>
          <div className={styles.form}>
            <div className={styles.fieldRow}>
              <CheckboxLike checked={changeLevel} onChange={setChangeLevel} label="修改等级" />
              <Select
                value={String(levelId)}
                onChange={(v) => setLevelId(Number(v))}
                disabled={!changeLevel}
                style={{ width: 200 }}
              >
                {levels.map((level) => (
                  <Select.Option key={String(level.id)} value={String(level.id)}>
                    {level.name}
                  </Select.Option>
                ))}
              </Select>
            </div>
            <div className={styles.fieldRow}>
              <CheckboxLike checked={changeLock} onChange={setChangeLock} label="修改锁定" />
              <Switch checked={locked} disabled={!changeLock} onChange={setLocked} />
            </div>
            <label className={styles.field}>
              经验增减（正数增加、负数扣减）
              <InputNumber min={-1e6} max={1e6} value={delta} onChange={(v) => setDelta(Number(v) || 0)} />
            </label>
            <label className={styles.field}>
              原因（必填，≤500）
              <Input value={reason} onChange={setReason} maxLength={500} placeholder="例如：活动补发 / 误判更正" />
            </label>
            <label className={styles.field}>
              幂等键（8-100，重试请沿用同一个）
              <Input value={key} onChange={setKey} maxLength={100} />
            </label>
            <p className={styles.hint}>
              预览影响：{changesPreview(member, changeLevel, targetLevelName(), changeLock, locked, delta)}
            </p>
            <div className={styles.actions}>
              <Button type="primary" loading={busy} onClick={() => void submit()}>
                提交调整
              </Button>
            </div>
          </div>

          <p className={styles.groupLabel}>最近经验记录</p>
          {history.length === 0 ? (
            <p className={styles.hint}>暂无经验记录。</p>
          ) : (
            <ul className={styles.history}>
              {history.map((entry) => (
                <li key={entry.id}>
                  <span className={entry.delta >= 0 ? styles.plus : styles.minus}>
                    {entry.delta >= 0 ? '+' : ''}
                    {entry.delta}
                  </span>
                  <span className={styles.histReason}>{entry.reason || entry.source}</span>
                  <span className={styles.histMeta}>
                    {entry.kind}
                    {entry.reversed ? ' · 已冲回' : ''} · {formatDateTime(entry.createdAt)}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </>
      ) : (
        <p className={styles.hint}>输入用户 ID 查看等级、经验与调整入口。</p>
      )}
    </section>
  );
}

function changesPreview(
  member: MemberStateView,
  changeLevel: boolean,
  levelName: string,
  changeLock: boolean,
  locked: boolean,
  delta: number,
): string {
  const parts: string[] = [];
  if (changeLevel) parts.push('等级 → ' + levelName);
  if (changeLock) parts.push('锁定 → ' + (locked ? '是' : '否'));
  if (delta) parts.push('经验 ' + member.experience + ' → ' + (member.experience + delta));
  return parts.length ? parts.join('；') : '尚未选择任何改动';
}

function CheckboxLike({
  checked,
  onChange,
  label,
}: {
  checked: boolean;
  onChange: (value: boolean) => void;
  label: string;
}) {
  return (
    <label className={styles.checkboxLike}>
      <input type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} />
      {label}
    </label>
  );
}
