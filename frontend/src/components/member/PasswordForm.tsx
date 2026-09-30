'use client';

import { useState } from 'react';
import { Button, Input } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './member-forms.module.css';

export function PasswordForm() {
  const [oldPassword, setOldPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    if (newPassword.length < 8) {
      setError('新密码至少 8 位');
      return;
    }
    if (newPassword !== confirm) {
      setError('两次输入的新密码不一致');
      return;
    }
    setSubmitting(true);
    try {
      await browserSend('/me/password', {
        method: 'POST',
        body: { old_password: oldPassword, new_password: newPassword, confirm_password: confirm },
      });
      toastSuccess('密码已修改，其他设备会话已下线');
      setOldPassword('');
      setNewPassword('');
      setConfirm('');
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
        <label className={styles.label} htmlFor="old-password">
          当前密码
        </label>
        <Input id="old-password" type="password" value={oldPassword} onChange={setOldPassword} autoComplete="current-password" />
      </div>
      <div className={styles.field}>
        <label className={styles.label} htmlFor="new-password">
          新密码
        </label>
        <Input id="new-password" type="password" value={newPassword} onChange={setNewPassword} autoComplete="new-password" />
      </div>
      <div className={styles.field}>
        <label className={styles.label} htmlFor="confirm-password">
          确认新密码
        </label>
        <Input id="confirm-password" type="password" value={confirm} onChange={setConfirm} autoComplete="new-password" />
      </div>
      <Button type="primary" htmlType="submit" loading={submitting}>
        修改密码
      </Button>
    </form>
  );
}
