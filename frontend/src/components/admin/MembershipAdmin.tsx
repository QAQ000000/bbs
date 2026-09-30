'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Checkbox, Collapse, Input, InputNumber, Select, Switch } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { ForumMembership, GrowthRule, MemberLevel, MemberPreview, MembershipConfig } from '@/lib/api/types';
import { LEVEL_BADGE_ICONS, MEMBER_ACTIONS, MEMBERSHIP_RULES } from '@/lib/api/types';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './MembershipAdmin.module.css';

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
  'thread.favorite': '收藏主题',
  'post.report': '举报',
  'upload.image': '上传图片',
  'upload.file': '上传附件',
  'attachment.download': '下载附件',
  'post.link.direct': '直接外链',
  'post.skip.moderate': '免审核发布',
};

const LIMIT_FIELDS: { key: keyof MemberLevel['limits']; label: string }[] = [
  { key: 'threadsPerDay', label: '每日主题数' },
  { key: 'repliesPerDay', label: '每日回复数' },
  { key: 'uploadsPerDay', label: '每日上传数' },
  { key: 'uploadBytesPerDay', label: '每日上传字节' },
  { key: 'imageBytes', label: '单图字节' },
  { key: 'fileBytes', label: '单文件字节' },
  { key: 'attachmentsPerPost', label: '每帖附件数' },
  { key: 'editMinutes', label: '编辑时限（分钟）' },
  { key: 'signatureLength', label: '签名长度' },
];

const RULE_LABELS: Record<string, string> = {
  active: '每日活跃',
  thread: '发布主题',
  reply: '回复主题',
  like: '获得点赞',
  digest: '主题加精',
};

export interface MembershipAdminProps {
  initial: MembershipConfig;
  forums: { id: string; name: string }[];
}

export function MembershipAdmin({ initial, forums }: MembershipAdminProps) {
  const router = useRouter();
  const [config, setConfig] = useState<MembershipConfig>(initial);
  const [preview, setPreview] = useState<MemberPreview | null>(null);
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  function edit(next: MembershipConfig) {
    setConfig(next);
    // 配置一变，之前的预览令牌即失效；必须重新预览才能保存。
    setPreview(null);
    setDirty(true);
    setNotice(null);
  }

  function updateLevel(index: number, patch: Partial<MemberLevel>) {
    edit({ ...config, levels: config.levels.map((l, i) => (i === index ? { ...l, ...patch } : l)) });
  }

  function updateLimit(index: number, key: keyof MemberLevel['limits'], value: number) {
    const level = config.levels[index];
    updateLevel(index, { limits: { ...level.limits, [key]: value } });
  }

  function updatePermission(index: number, action: string, value: boolean) {
    const level = config.levels[index];
    updateLevel(index, { permissions: { ...level.permissions, [action]: value } });
  }

  function updateRule(key: string, patch: Partial<GrowthRule>) {
    edit({ ...config, rules: { ...config.rules, [key]: { ...config.rules[key], ...patch } } });
  }

  function updateForum(index: number, patch: Partial<ForumMembership>) {
    edit({ ...config, forums: config.forums.map((f, i) => (i === index ? { ...f, ...patch } : f)) });
  }

  async function runPreview() {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const result = await browserSend<MemberPreview>('/admin/membership/preview', { method: 'POST', body: config });
      setPreview(result);
      setDirty(false);
      setNotice('预览已生成，确认影响后点击“发布配置”。');
    } catch (caught) {
      const err = caught as ApiError;
      setError(
        err.status === 409
          ? '配置版本已变化（可能有人在别处保存过）。你的输入仍然保留，请重新读取后再预览。'
          : err.message,
      );
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  async function runSave() {
    if (!preview) {
      setError('请先预览并确认影响范围。');
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await browserSend('/admin/membership', { method: 'PUT', body: { config, previewToken: preview.token } });
      toastSuccess('会员配置已发布');
      setPreview(null);
      setDirty(false);
      setNotice('配置已发布，升级与补发由后端按新规则执行。');
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      setError(
        err.status === 409
          ? '预览令牌或版本已失效（配置可能已变化）。你的输入仍然保留，请重新预览。'
          : err.message,
      );
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={styles.wrap}>
      <section className={['panel', styles.card].join(' ')}>
        <div className={styles.head}>
          <h2 className={styles.title}>配置版本 v{config.version}</h2>
          <span className={styles.hint}>等级 0 必留且无门槛；等级 ID 与顺序须唯一；额度 -1 表示不限、0 表示禁止。</span>
        </div>
        {error ? <p className={styles.error} role="alert">{error}</p> : null}
        {notice ? <p className={styles.notice}>{notice}</p> : null}

        <div className={styles.guestRow}>
          <span className={styles.rowLabel}>游客权限</span>
          <Checkbox
            checked={Boolean(config.guestPermissions['forum.read'])}
            onChange={(v) => edit({ ...config, guestPermissions: { ...config.guestPermissions, 'forum.read': v } })}
          >
            可浏览版块
          </Checkbox>
          <Checkbox
            checked={Boolean(config.guestPermissions['attachment.download'])}
            onChange={(v) =>
              edit({ ...config, guestPermissions: { ...config.guestPermissions, 'attachment.download': v } })
            }
          >
            可下载附件
          </Checkbox>
        </div>

        <h3 className={styles.subTitle}>成长规则（经验，不等于积分）</h3>
        <div className={styles.rules}>
          {MEMBERSHIP_RULES.map((key) => {
            const rule = config.rules[key] ?? { enabled: false, points: 0, dailyCap: 0, reverse: false };
            return (
              <div key={key} className={styles.ruleRow}>
                <span className={styles.ruleName}>{RULE_LABELS[key] ?? key}</span>
                <Switch size="small" checked={rule.enabled} onChange={(v) => updateRule(key, { enabled: v })} />
                <label className={styles.inlineField}>
                  经验
                  <InputNumber size="small" min={0} max={99999} value={rule.points} onChange={(v) => updateRule(key, { points: Number(v) || 0 })} />
                </label>
                <label className={styles.inlineField}>
                  每日上限
                  <InputNumber size="small" min={0} max={9999999} value={rule.dailyCap} onChange={(v) => updateRule(key, { dailyCap: Number(v) || 0 })} />
                </label>
                <Checkbox checked={rule.reverse} onChange={(v) => updateRule(key, { reverse: v })}>
                  可冲回
                </Checkbox>
              </div>
            );
          })}
        </div>

        <h3 className={styles.subTitle}>等级（{config.levels.length}）</h3>
        <Collapse>
          {config.levels.map((level, index) => (
            <Collapse.Item
              key={String(index)}
              name={String(index)}
              header={
                <span className={styles.levelHeader}>
                  <span className={styles.levelBadge} style={{ background: level.badge.background, color: level.badge.color }}>
                    {level.badge.label || 'LV' + level.id}
                  </span>
                  {level.name}
                  <span className={styles.levelMeta}>ID {level.id} · 顺序 {level.rank} · 经验 {level.experience}</span>
                </span>
              }
            >
              <div className={styles.grid}>
                <label className={styles.field}>
                  名称
                  <Input value={level.name} maxLength={30} onChange={(v) => updateLevel(index, { name: v })} />
                </label>
                <label className={styles.field}>
                  等级 ID
                  <InputNumber min={0} max={32767} value={level.id} disabled={index === 0} onChange={(v) => updateLevel(index, { id: Number(v) || 0 })} />
                </label>
                <label className={styles.field}>
                  顺序 rank
                  <InputNumber min={0} max={10000} value={level.rank} onChange={(v) => updateLevel(index, { rank: Number(v) || 0 })} />
                </label>
                <label className={styles.field}>
                  经验门槛
                  <InputNumber min={0} max={1e12} value={level.experience} disabled={index === 0} onChange={(v) => updateLevel(index, { experience: Number(v) || 0 })} />
                </label>
                <label className={styles.field}>
                  活跃天数
                  <InputNumber min={0} max={36500} value={level.daysVisited} disabled={index === 0} onChange={(v) => updateLevel(index, { daysVisited: Number(v) || 0 })} />
                </label>
                <label className={styles.field}>
                  阅读帖子数
                  <InputNumber min={0} max={1e9} value={level.postsRead} disabled={index === 0} onChange={(v) => updateLevel(index, { postsRead: Number(v) || 0 })} />
                </label>
                <label className={styles.field}>
                  发帖数
                  <InputNumber min={0} max={1e9} value={level.postCount} disabled={index === 0} onChange={(v) => updateLevel(index, { postCount: Number(v) || 0 })} />
                </label>
                <div className={styles.fieldRow}>
                  <Checkbox checked={level.automatic} disabled={index === 0} onChange={(v) => updateLevel(index, { automatic: v })}>
                    自动升级
                  </Checkbox>
                  <Checkbox checked={level.emailVerified} disabled={index === 0} onChange={(v) => updateLevel(index, { emailVerified: v })}>
                    需邮箱已验证
                  </Checkbox>
                </div>
              </div>

              <div className={styles.grid}>
                <label className={styles.field}>
                  徽章文字
                  <Input value={level.badge.label} maxLength={20} onChange={(v) => updateLevel(index, { badge: { ...level.badge, label: v } })} />
                </label>
                <label className={styles.field}>
                  徽章图标
                  <Select value={level.badge.icon} onChange={(v) => updateLevel(index, { badge: { ...level.badge, icon: v as string } })} style={{ width: '100%' }}>
                    {LEVEL_BADGE_ICONS.map((icon) => (
                      <Select.Option key={icon || 'none'} value={icon}>
                        {icon || '（无）'}
                      </Select.Option>
                    ))}
                  </Select>
                </label>
                <label className={styles.field}>
                  文字色 #RRGGBB
                  <Input value={level.badge.color} onChange={(v) => updateLevel(index, { badge: { ...level.badge, color: v } })} placeholder="#334155" />
                </label>
                <label className={styles.field}>
                  背景色 #RRGGBB
                  <Input value={level.badge.background} onChange={(v) => updateLevel(index, { badge: { ...level.badge, background: v } })} placeholder="#e2e8f0" />
                </label>
              </div>

              <p className={styles.groupLabel}>额度</p>
              <div className={styles.grid}>
                {LIMIT_FIELDS.map((item) => (
                  <label key={item.key} className={styles.field}>
                    {item.label}
                    <InputNumber
                      min={-1}
                      max={1e12}
                      value={level.limits[item.key]}
                      onChange={(v) => updateLimit(index, item.key, v === null || v === undefined ? 0 : Number(v))}
                    />
                  </label>
                ))}
              </div>

              <p className={styles.groupLabel}>权限</p>
              <div className={styles.perms}>
                {MEMBER_ACTIONS.map((action) => (
                  <Checkbox
                    key={action}
                    checked={Boolean(level.permissions[action])}
                    onChange={(v) => updatePermission(index, action, v)}
                  >
                    {ACTION_LABELS[action] ?? action}
                  </Checkbox>
                ))}
              </div>
            </Collapse.Item>
          ))}
        </Collapse>

        <h3 className={styles.subTitle}>版块访问规则（{config.forums.length}）</h3>
        {config.forums.length === 0 ? <p className={styles.hint}>没有版块级覆盖规则。</p> : null}
        {config.forums.map((forum, index) => (
          <div key={String(index)} className={styles.forumRow}>
            <Select
              value={forum.forumId}
              onChange={(v) => updateForum(index, { forumId: v as string })}
              style={{ width: 180 }}
              placeholder="版块"
            >
              {forums.map((item) => (
                <Select.Option key={item.id} value={item.id}>
                  {item.name}
                </Select.Option>
              ))}
            </Select>
            <label className={styles.inlineField}>
              最低等级
              <InputNumber
                size="small"
                min={0}
                max={32767}
                value={forum.minimumLevel}
                onChange={(v) => updateForum(index, { minimumLevel: Number(v) || 0 })}
              />
            </label>
            <Checkbox checked={forum.membersOnly} onChange={(v) => updateForum(index, { membersOnly: v })}>
              仅会员
            </Checkbox>
            <Select
              mode="multiple"
              value={forum.denied}
              onChange={(v) => updateForum(index, { denied: v as string[] })}
              placeholder="额外禁止的操作"
              style={{ minWidth: 260, flex: 1 }}
            >
              {MEMBER_ACTIONS.map((action) => (
                <Select.Option key={action} value={action}>
                  {ACTION_LABELS[action] ?? action}
                </Select.Option>
              ))}
            </Select>
            <Button size="mini" type="text" status="danger" onClick={() => edit({ ...config, forums: config.forums.filter((_, i) => i !== index) })}>
              删除
            </Button>
          </div>
        ))}
        <Button
          size="small"
          type="text"
          disabled={config.forums.length >= 1000}
          onClick={() =>
            edit({
              ...config,
              forums: [...config.forums, { forumId: forums[0]?.id ?? '', minimumLevel: 0, membersOnly: false, denied: [] }],
            })
          }
        >
          + 添加版块规则
        </Button>

        <div className={styles.actions}>
          <Button type="primary" loading={busy} onClick={() => void runPreview()}>
            预览影响
          </Button>
          <Button type="secondary" loading={busy} disabled={!preview || dirty} onClick={() => void runSave()}>
            发布配置
          </Button>
          <Button type="text" onClick={() => router.refresh()}>
            重新读取
          </Button>
        </div>

        {preview ? (
          <div className={styles.preview}>
            <p className={styles.previewTitle}>预览影响（令牌已就绪）</p>
            <ul className={styles.previewList}>
              <li>扫描用户：{preview.users}</li>
              <li>会受影响：{preview.affectedUsers}</li>
              <li>会升级：{preview.upgrades}</li>
              <li>锁定用户：{preview.locked}</li>
            </ul>
            <p className={styles.hint}>发布后由后端按新规则执行升级与补发；配置变化会使令牌失效，需要重新预览。</p>
          </div>
        ) : (
          <p className={styles.hint}>必须先预览：后端用预览令牌校验发布内容与冻结口径一致。</p>
        )}
      </section>
    </div>
  );
}
