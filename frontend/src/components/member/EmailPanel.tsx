'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Input } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './member-forms.module.css';

export interface EmailPanelProps {
  email: string;
  emailVerified: boolean;
  /** 站点邮箱验证闸门是否生效（开关开启且 SMTP 可用）。 */
  emailGateEnabled: boolean;
  mfaEnabled: boolean;
}

export function EmailPanel({ email, emailVerified, emailGateEnabled, mfaEnabled }: EmailPanelProps) {
  const router = useRouter();
  const [showChange, setShowChange] = useState(false);
  const [newEmail, setNewEmail] = useState('');
  const [password, setPassword] = useState('');
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [resending, setResending] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function resend() {
    setResending(true);
    setError(null);
    setNotice(null);
    try {
      await browserSend('/me/email/verify-resend', { method: 'POST' });
      setNotice('验证邮件已加入发送队列（24 小时内有效）');
    } catch (caught) {
      setError((caught as ApiError).message);
    } finally {
      setResending(false);
    }
  }

  async function change(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    setNotice(null);
    if (!newEmail.trim()) {
      setError('请输入新邮箱');
      return;
    }
    setBusy(true);
    try {
      const result = await browserSend<{ message?: string; expiresIn?: number }>('/me/email/change', {
        method: 'POST',
        body: { email: newEmail.trim(), password, code },
      });
      setNotice(
        (result?.message || '确认邮件已发送') +
          '。请在当前登录设备打开邮件中的确认链接（30 分钟内有效）；确认前账号仍使用原邮箱。',
      );
      setShowChange(false);
      setNewEmail('');
      setPassword('');
      setCode('');
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      if (err.code === 'EMAIL_UNAVAILABLE' || err.status === 503) {
        setError('邮件服务未配置或当前邮箱不可用，暂时无法更换邮箱。请联系站点管理员。');
      } else {
        setError(err.message);
      }
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <p className={styles.emailState}>
        当前邮箱：<strong>{email || '未绑定'}</strong>
        {email ? (
          <span className={emailVerified ? styles.verified : styles.unverified}>
            {emailVerified ? '已验证' : '未验证'}
          </span>
        ) : null}
      </p>

      {notice ? <p className={styles.notice}>{notice}</p> : null}
      {error ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : null}

      <div className={styles.emailActions}>
        {email && !emailVerified && emailGateEnabled ? (
          <Button size="small" type="secondary" loading={resending} onClick={() => void resend()}>
            重新发送验证邮件
          </Button>
        ) : null}
        <Button size="small" type="secondary" onClick={() => setShowChange((v) => !v)}>
          {showChange ? '收起' : '更换邮箱'}
        </Button>
      </div>

      {showChange ? (
        <form className={styles.changeForm} onSubmit={change}>
          <div className={styles.field}>
            <label className={styles.label} htmlFor="new-email">
              新邮箱
            </label>
            <Input id="new-email" value={newEmail} onChange={setNewEmail} placeholder="新的邮箱地址" autoComplete="email" />
          </div>
          <div className={styles.field}>
            <label className={styles.label} htmlFor="email-password">
              当前密码
            </label>
            <Input
              id="email-password"
              type="password"
              value={password}
              onChange={setPassword}
              placeholder="用于确认身份"
              autoComplete="current-password"
            />
          </div>
          {mfaEnabled ? (
            <div className={styles.field}>
              <label className={styles.label} htmlFor="email-code">
                两步验证码
              </label>
              <Input id="email-code" value={code} onChange={setCode} placeholder="动态码或恢复码" />
            </div>
          ) : null}
          <Button type="primary" htmlType="submit" loading={busy}>
            发送确认邮件
          </Button>
          <p className={styles.hint}>
            换绑需要两阶段确认：先验证当前密码，再到新邮箱点击确认链接。确认后其他设备会退出登录，旧邮箱无法再用于找回密码。
          </p>
        </form>
      ) : null}
    </div>
  );
}
