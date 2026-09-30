'use client';

import { useState } from 'react';
import Link from 'next/link';
import { Button, Input } from '@arco-design/web-react';
import { IconLock } from '@arco-design/web-react/icon';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import styles from './auth-forms.module.css';

export function ResetPasswordForm({ token }: { token: string }) {
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    if (password.length < 8) {
      setError('密码至少 8 位');
      return;
    }
    if (password !== confirm) {
      setError('两次输入的密码不一致');
      return;
    }
    setSubmitting(true);
    try {
      await browserSend('/auth/password/reset', { method: 'POST', body: { token, password } });
      setDone(true);
    } catch (caught) {
      setError((caught as ApiError).message);
    } finally {
      setSubmitting(false);
    }
  }

  if (!token) {
    return (
      <div>
        <h1 className={styles.title}>链接无效</h1>
        <p className={styles.subtitle}>缺少重置令牌。请从邮件中的链接进入，或重新申请密码重置。</p>
        <p className={styles.footer}>
          <Link href="/password/forgot">重新申请</Link>
        </p>
      </div>
    );
  }

  if (done) {
    return (
      <div>
        <h1 className={styles.title}>密码已重置</h1>
        <p className={styles.subtitle}>请使用新密码登录。其他设备的会话已下线。</p>
        <p className={styles.footer}>
          <Link href="/login">去登录</Link>
        </p>
      </div>
    );
  }

  return (
    <div>
      <h1 className={styles.title}>设置新密码</h1>
      <p className={styles.subtitle}>新密码至少 8 位。重置后其他设备会话将失效。</p>
      {error ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : null}
      <form onSubmit={submit}>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="reset-password">
            新密码
          </label>
          <Input
            id="reset-password"
            value={password}
            onChange={setPassword}
            type="password"
            prefix={<IconLock />}
            placeholder="至少 8 位"
            autoComplete="new-password"
          />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="reset-confirm">
            确认新密码
          </label>
          <Input
            id="reset-confirm"
            value={confirm}
            onChange={setConfirm}
            type="password"
            prefix={<IconLock />}
            placeholder="再次输入新密码"
            autoComplete="new-password"
          />
        </div>
        <Button className={styles.submit} type="primary" long htmlType="submit" loading={submitting}>
          重置密码
        </Button>
      </form>
    </div>
  );
}
