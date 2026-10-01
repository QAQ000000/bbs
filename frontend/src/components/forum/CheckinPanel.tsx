'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { CheckinRecord, CheckinStatus } from '@/lib/api/types';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './CheckinPanel.module.css';

export function CheckinPanel({ status }: { status: CheckinStatus }) {
  const router = useRouter();
  const [current, setCurrent] = useState<CheckinStatus>(status);
  const [busy, setBusy] = useState(false);

  async function claim() {
    setBusy(true);
    try {
      const record = await browserSend<CheckinRecord>('/me/checkin', { method: 'POST' });
      setCurrent((prev) => ({ ...prev, checkedIn: true, streak: record.streak, checkin: record }));
      toastSuccess('签到成功，获得 ' + record.points + ' 积分、' + record.experience + ' 经验');
      router.refresh();
    } catch (caught) {
      toastError(caught as ApiError);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={['panel', styles.panel].join(' ')}>
      <div className={styles.status}>
        <h2 className={styles.heading}>{current.checkedIn ? '今日已签到' : current.enabled ? '每日签到' : '签到未开放'}</h2>
        <p className={styles.tz}>统计时区：{current.timeZone} · 当前日期 {current.day}</p>
      </div>
      <div className={styles.streak}>
        <span className={styles.streakValue}>{current.streak}</span>
        <span className={styles.streakLabel}>连续签到天数</span>
      </div>
      {current.checkin ? (
        <p className={styles.reward}>
          今日已签到，获得 {current.checkin.points} 积分、{current.checkin.experience} 经验（规则版本 v
          {current.checkin.ruleVersion}）。
        </p>
      ) : (
        <p className={styles.reward}>今日尚未签到。签到可累计经验与积分。</p>
      )}
      <Button type="primary" size="large" loading={busy} disabled={!current.enabled || current.checkedIn} onClick={() => void claim()}>
        {current.checkedIn ? '今日已签到' : current.enabled ? '立即签到' : '签到未开放'}
      </Button>
    </div>
  );
}
