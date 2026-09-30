'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Checkbox, Input } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { NotificationPreferences } from '@/lib/api/types';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './member-forms.module.css';

export function ProfileForm({ signature }: { signature: string }) {
  const router = useRouter();
  const [value, setValue] = useState(signature);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await browserSend('/me', { method: 'PATCH', body: { signature: value } });
      toastSuccess('资料已保存');
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      setError(err.message);
      toastError(err);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form onSubmit={submit}>
      {error ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : null}
      <div className={styles.field}>
        <label className={styles.label} htmlFor="signature">
          个人签名（最多 200 字）
        </label>
        <Input.TextArea
          id="signature"
          value={value}
          onChange={setValue}
          rows={3}
          maxLength={200}
          showWordLimit
          placeholder="介绍一下自己…"
        />
      </div>
      <p className={styles.hint}>邮箱换绑需要独立的身份验证流程，请在邮件设置中完成。</p>
      <Button type="primary" htmlType="submit" loading={submitting}>
        保存资料
      </Button>
    </form>
  );
}

const PREF_FIELDS: { key: keyof NotificationPreferences; label: string }[] = [
  { key: 'replies', label: '回复我的主题' },
  { key: 'mentions', label: '有人提到我' },
  { key: 'acceptance', label: '我的回复被采纳' },
  { key: 'membership', label: '会员等级变化' },
  { key: 'titles', label: '称号变化' },
  { key: 'moderation', label: '内容审核结果' },
  { key: 'reports', label: '举报处理结果' },
  { key: 'subscriptions', label: '订阅内容更新' },
  { key: 'email', label: '同时发送邮件通知' },
];

export function NotificationPreferencesForm({ initial }: { initial: NotificationPreferences }) {
  const router = useRouter();
  const [prefs, setPrefs] = useState<NotificationPreferences>(initial);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  function toggle(key: keyof NotificationPreferences, value: boolean) {
    setPrefs((prev) => ({ ...prev, [key]: value }));
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await browserSend('/me/notification-preferences', { method: 'PUT', body: prefs });
      toastSuccess('通知偏好已保存');
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      setError(err.message);
      toastError(err);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form onSubmit={submit}>
      {error ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : null}
      <div className={styles.prefs}>
        {PREF_FIELDS.map((field) => (
          <Checkbox
            key={field.key}
            checked={Boolean(prefs[field.key])}
            onChange={(checked: boolean) => toggle(field.key, checked)}
          >
            {field.label}
          </Checkbox>
        ))}
      </div>
      <p className={styles.hint}>
        保存时会提交全部开关。单项订阅的邮件开关仍需站点启用邮件、邮箱有效，才会真正投递。
      </p>
      <Button type="primary" htmlType="submit" loading={submitting}>
        保存通知偏好
      </Button>
    </form>
  );
}
