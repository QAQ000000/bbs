'use client';

import { useState } from 'react';
import Link from 'next/link';
import { Button, Input } from '@arco-design/web-react';
import { IconEmail } from '@arco-design/web-react/icon';
import { browserSend } from '@/lib/api/browser';
import { toastError } from '../ui/feedback';
import styles from './auth-forms.module.css';

export function ForgotPasswordForm() {
  const [email, setEmail] = useState('');
  const [sent, setSent] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    try {
      await browserSend('/auth/password/forgot', { method: 'POST', body: { email: email.trim() } });
      setSent(true);
    } catch (error) {
      toastError(error);
    } finally {
      setSubmitting(false);
    }
  }

  if (sent) {
    return (
      <div>
        <h1 className={styles.title}>邮件已发送</h1>
        <p className={styles.subtitle}>
          如果该邮箱对应的账号存在，我们已发送重置邮件。请查收并在邮件中完成密码重置；链接有有效期，过期可重新申请。
        </p>
        <p className={styles.footer}>
          <Link href="/login">返回登录</Link>
        </p>
      </div>
    );
  }

  return (
    <div>
      <h1 className={styles.title}>找回密码</h1>
      <p className={styles.subtitle}>输入注册邮箱，我们会发送重置链接。为保护账号隐私，无论邮箱是否存在响应一致。</p>
      <form onSubmit={submit}>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="forgot-email">
            邮箱
          </label>
          <Input
            id="forgot-email"
            value={email}
            onChange={setEmail}
            prefix={<IconEmail />}
            placeholder="注册时使用的邮箱"
            autoComplete="email"
          />
        </div>
        <Button className={styles.submit} type="primary" long htmlType="submit" loading={submitting}>
          发送重置邮件
        </Button>
      </form>
      <p className={styles.footer}>
        想起密码了？<Link href="/login">返回登录</Link>
      </p>
    </div>
  );
}
