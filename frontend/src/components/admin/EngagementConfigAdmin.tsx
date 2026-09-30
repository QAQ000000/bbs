'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Checkbox, Input, InputNumber, Switch } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { EngagementRules, GrowthRule, PointsConfig } from '@/lib/api/types';
import { POINTS_RULES } from '@/lib/api/types';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './EngagementConfigAdmin.module.css';

const POINTS_LABELS: Record<string, string> = {
  active: '每日活跃',
  thread: '发布主题',
  reply: '回复主题',
  like: '获得点赞',
  digest: '主题加精',
  accepted: '回复被采纳',
};

interface EngagementView {
  version: number;
  poll: { enabled: boolean; maxOptions: number; maxDays: number };
  bounty: { enabled: boolean; minPoints: number; maxPoints: number; maxDays: number };
  checkin: { enabled: boolean; experience: number; points: number; timeZone: string };
}

function RuleEditor({
  rule,
  onChange,
  maxPoints,
  maxCap,
}: {
  rule: GrowthRule;
  onChange: (next: GrowthRule) => void;
  maxPoints: number;
  maxCap: number;
}) {
  return (
    <>
      <Switch size="small" checked={rule.enabled} onChange={(v) => onChange({ ...rule, enabled: v })} />
      <label className={styles.inlineField}>
        分值
        <InputNumber size="small" min={0} max={maxPoints} value={rule.points} onChange={(v) => onChange({ ...rule, points: Number(v) || 0 })} />
      </label>
      <label className={styles.inlineField}>
        每日上限
        <InputNumber size="small" min={0} max={maxCap} value={rule.dailyCap} onChange={(v) => onChange({ ...rule, dailyCap: Number(v) || 0 })} />
      </label>
      <Checkbox checked={rule.reverse} onChange={(v) => onChange({ ...rule, reverse: v })}>
        可冲回
      </Checkbox>
    </>
  );
}

export interface EngagementConfigAdminProps {
  points: PointsConfig;
  engagement: EngagementView | null;
  rules: EngagementRules | null;
}

export function EngagementConfigAdmin({ points: initialPoints, engagement: initialEngagement }: EngagementConfigAdminProps) {
  const router = useRouter();
  const [points, setPoints] = useState<PointsConfig>(initialPoints);
  const [engagement, setEngagement] = useState<EngagementView | null>(initialEngagement);
  const [busy, setBusy] = useState(false);
  const [pointsError, setPointsError] = useState<string | null>(null);
  const [pointsNotice, setPointsNotice] = useState<string | null>(null);
  const [engError, setEngError] = useState<string | null>(null);
  const [engNotice, setEngNotice] = useState<string | null>(null);

  function patchRule(kind: string, next: GrowthRule) {
    setPoints((prev) => ({ ...prev, rules: { ...prev.rules, [kind]: next } }));
    setPointsNotice(null);
  }

  async function savePoints() {
    setBusy(true);
    setPointsError(null);
    setPointsNotice(null);
    try {
      const saved = await browserSend<PointsConfig>('/admin/points/config', { method: 'PUT', body: points });
      setPoints(saved);
      toastSuccess('积分规则已保存');
      setPointsNotice('已保存，版本提升到 v' + saved.version + '；新的发放按新规则执行。');
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      setPointsError(
        err.status === 409
          ? '积分配置版本已变化或请求标识冲突。你的输入仍然保留，请重新读取后合并。'
          : err.message,
      );
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  async function saveEngagement() {
    if (!engagement) return;
    setBusy(true);
    setEngError(null);
    setEngNotice(null);
    try {
      const saved = await browserSend<EngagementView>('/admin/engagement/config', { method: 'PUT', body: engagement });
      setEngagement(saved);
      toastSuccess('互动配置已保存');
      setEngNotice('已保存，版本提升到 v' + saved.version + '。');
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      setEngError(
        err.status === 409
          ? '互动配置版本已变化。你的输入仍然保留，请重新读取后合并。'
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
          <h2 className={styles.title}>积分规则（当前余额榜的唯一来源）</h2>
          <span className={styles.hint}>版本 v{points.version} · 固定 6 类，缺一不可；积分与经验是两个独立账本。</span>
        </div>
        {pointsError ? <p className={styles.error} role="alert">{pointsError}</p> : null}
        {pointsNotice ? <p className={styles.notice}>{pointsNotice}</p> : null}
        <div className={styles.rules}>
          {POINTS_RULES.map((kind) => {
            const rule = points.rules[kind] ?? { enabled: false, points: 0, dailyCap: 0, reverse: false };
            return (
              <div key={kind} className={styles.ruleRow}>
                <span className={styles.ruleName}>{POINTS_LABELS[kind] ?? kind}</span>
                <RuleEditor rule={rule} onChange={(next) => patchRule(kind, next)} maxPoints={10000} maxCap={1000000} />
              </div>
            );
          })}
          <div className={styles.ruleRow}>
            <span className={styles.ruleName}>回复被采纳（经验）</span>
            <RuleEditor
              rule={points.acceptedExperience}
              onChange={(next) => {
                setPoints((prev) => ({ ...prev, acceptedExperience: next }));
                setPointsNotice(null);
              }}
              maxPoints={10000}
              maxCap={1000000}
            />
          </div>
        </div>
        <div className={styles.actions}>
          <Button type="primary" loading={busy} onClick={() => void savePoints()}>
            保存积分规则
          </Button>
          <Button type="text" onClick={() => router.refresh()}>
            重新读取
          </Button>
        </div>
      </section>

      <section className={['panel', styles.card].join(' ')}>
        <div className={styles.head}>
          <h2 className={styles.title}>互动配置（投票 / 悬赏 / 签到）</h2>
          <span className={styles.hint}>
            {engagement ? '版本 v' + engagement.version : '配置不可读取'} · 范围以实际契约为准；停用后前台入口会隐藏。
          </span>
        </div>
        {engError ? <p className={styles.error} role="alert">{engError}</p> : null}
        {engNotice ? <p className={styles.notice}>{engNotice}</p> : null}
        {!engagement ? (
          <p className={styles.hint}>互动配置读取失败，请重新读取后再修改；保存失败不会回退成默认值。</p>
        ) : (
          <>
            <div className={styles.group}>
              <p className={styles.groupTitle}>投票</p>
              <div className={styles.rules}>
                <div className={styles.ruleRow}>
                  <span className={styles.ruleName}>启用</span>
                  <Switch
                    size="small"
                    checked={engagement.poll.enabled}
                    onChange={(v) => { setEngagement({ ...engagement, poll: { ...engagement.poll, enabled: v } }); setEngNotice(null); }}
                  />
                  <label className={styles.inlineField}>
                    最多选项（2-20）
                    <InputNumber
                      size="small"
                      min={2}
                      max={20}
                      value={engagement.poll.maxOptions}
                      onChange={(v) => { setEngagement({ ...engagement, poll: { ...engagement.poll, maxOptions: Number(v) || 2 } }); setEngNotice(null); }}
                    />
                  </label>
                  <label className={styles.inlineField}>
                    最长天数（1-365）
                    <InputNumber
                      size="small"
                      min={1}
                      max={365}
                      value={engagement.poll.maxDays}
                      onChange={(v) => { setEngagement({ ...engagement, poll: { ...engagement.poll, maxDays: Number(v) || 1 } }); setEngNotice(null); }}
                    />
                  </label>
                </div>
              </div>
            </div>

            <div className={styles.group}>
              <p className={styles.groupTitle}>悬赏</p>
              <div className={styles.rules}>
                <div className={styles.ruleRow}>
                  <span className={styles.ruleName}>启用</span>
                  <Switch
                    size="small"
                    checked={engagement.bounty.enabled}
                    onChange={(v) => { setEngagement({ ...engagement, bounty: { ...engagement.bounty, enabled: v } }); setEngNotice(null); }}
                  />
                  <label className={styles.inlineField}>
                    最少积分
                    <InputNumber
                      size="small"
                      min={1}
                      max={1000000}
                      value={engagement.bounty.minPoints}
                      onChange={(v) => { setEngagement({ ...engagement, bounty: { ...engagement.bounty, minPoints: Number(v) || 1 } }); setEngNotice(null); }}
                    />
                  </label>
                  <label className={styles.inlineField}>
                    最多积分（≤1,000,000）
                    <InputNumber
                      size="small"
                      min={1}
                      max={1000000}
                      value={engagement.bounty.maxPoints}
                      onChange={(v) => { setEngagement({ ...engagement, bounty: { ...engagement.bounty, maxPoints: Number(v) || 1 } }); setEngNotice(null); }}
                    />
                  </label>
                  <label className={styles.inlineField}>
                    最长天数（1-365）
                    <InputNumber
                      size="small"
                      min={1}
                      max={365}
                      value={engagement.bounty.maxDays}
                      onChange={(v) => { setEngagement({ ...engagement, bounty: { ...engagement.bounty, maxDays: Number(v) || 1 } }); setEngNotice(null); }}
                    />
                  </label>
                </div>
                <p className={styles.hint}>
                  已支付的悬赏不能通过配置或后台撤销；停用只影响新发布。
                </p>
              </div>
            </div>

            <div className={styles.group}>
              <p className={styles.groupTitle}>签到</p>
              <div className={styles.rules}>
                <div className={styles.ruleRow}>
                  <span className={styles.ruleName}>启用</span>
                  <Switch
                    size="small"
                    checked={engagement.checkin.enabled}
                    onChange={(v) => { setEngagement({ ...engagement, checkin: { ...engagement.checkin, enabled: v } }); setEngNotice(null); }}
                  />
                  <label className={styles.inlineField}>
                    经验奖励（0-10000）
                    <InputNumber
                      size="small"
                      min={0}
                      max={10000}
                      value={engagement.checkin.experience}
                      onChange={(v) => { setEngagement({ ...engagement, checkin: { ...engagement.checkin, experience: Number(v) || 0 } }); setEngNotice(null); }}
                    />
                  </label>
                  <label className={styles.inlineField}>
                    积分奖励（0-10000）
                    <InputNumber
                      size="small"
                      min={0}
                      max={10000}
                      value={engagement.checkin.points}
                      onChange={(v) => { setEngagement({ ...engagement, checkin: { ...engagement.checkin, points: Number(v) || 0 } }); setEngNotice(null); }}
                    />
                  </label>
                  <label className={styles.inlineField}>
                    时区（IANA）
                    <Input
                      size="small"
                      value={engagement.checkin.timeZone}
                      style={{ width: 160 }}
                      onChange={(v) => { setEngagement({ ...engagement, checkin: { ...engagement.checkin, timeZone: v } }); setEngNotice(null); }}
                    />
                  </label>
                </div>
                <p className={styles.hint}>签到按该时区划分自然日；不支持 Local。</p>
              </div>
            </div>

            <div className={styles.actions}>
              <Button type="primary" loading={busy} onClick={() => void saveEngagement()}>
                保存互动配置
              </Button>
              <Button type="text" onClick={() => router.refresh()}>
                重新读取
              </Button>
            </div>
          </>
        )}
      </section>
    </div>
  );
}
