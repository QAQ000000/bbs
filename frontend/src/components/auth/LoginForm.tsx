'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { Button, Input } from '@arco-design/web-react';
import { IconEye, IconEyeInvisible, IconLock, IconSafe, IconUser } from '@arco-design/web-react/icon';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import { useSession } from '@/lib/auth/session';
import type { CurrentUser } from '@/lib/api/types';
import { toastSuccess } from '../ui/feedback';
import styles from './auth-forms.module.css';

export interface LoginFormProps {
  next?: string;
  registerEnabled: boolean;
}

function safeNext(value?: string): string {
  if (!value || !value.startsWith('/') || value.startsWith('//')) return '/';
  return value;
}

export function LoginForm({ next, registerEnabled }: LoginFormProps) {
  const router = useRouter();
  const { refresh } = useSession();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [challenge, setChallenge] = useState<string | null>(null);
  const [code, setCode] = useState('');
  const [recovery, setRecovery] = useState('');
  const [useRecovery, setUseRecovery] = useState(false);

  async function finish(user: CurrentUser | undefined) {
    await refresh();
    if (user?.mustChangePassword) {
      toastSuccess('请先修改初始密码');
      router.push('/me/security');
      router.refresh();
      return;
    }
    router.push(safeNext(next));
    router.refresh();
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    if (!username.trim() || !password) {
      setError('请输入用户名和密码');
      return;
    }
    setSubmitting(true);
    try {
      const user = await browserSend<CurrentUser>('/auth/login', {
        method: 'POST',
        body: { username: username.trim(), password },
      });
      await finish(user);
    } catch (caught) {
      const err = caught as ApiError;
      const raw = err.raw as { code?: string; challenge?: string } | undefined;
      if (err.status === 401 && raw?.code === 'MFA_REQUIRED' && raw.challenge) {
        setChallenge(raw.challenge);
        setError(null);
        return;
      }
      setError(err.message);
    } finally {
      setSubmitting(false);
    }
  }

  async function submitMfa(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    if (!challenge) return;
    setSubmitting(true);
    try {
      const user = await browserSend<CurrentUser>('/auth/2fa', {
        method: 'POST',
        body: { challenge, code, recovery: useRecovery ? recovery : '' },
      });
      await finish(user);
    } catch (caught) {
      setError((caught as ApiError).message);
    } finally {
      setSubmitting(false);
    }
  }

  if (challenge) {
    return (
      <div className={styles.mfa}>
        <h1 className={styles.title}>两步验证</h1>
        <p className={styles.subtitle}>请输入验证器应用中的 6 位动态码，或使用恢复码。</p>
        {error ? (
          <p className={styles.error} role="alert">
            {error}
          </p>
        ) : null}
        <form onSubmit={submitMfa}>
          {useRecovery ? (
            <div className={styles.field}>
              <label className={styles.label} htmlFor="recovery">
                恢复码
              </label>
              <Input
                id="recovery"
                value={recovery}
                onChange={setRecovery}
                prefix={<IconSafe />}
                placeholder="一次性恢复码"
              />
            </div>
          ) : (
            <div className={styles.field}>
              <label className={styles.label} htmlFor="code">
                动态验证码
              </label>
              <Input
                id="code"
                value={code}
                onChange={setCode}
                prefix={<IconSafe />}
                placeholder="6 位验证码"
                maxLength={6}
              />
            </div>
          )}
          <Button className={styles.submit} type="primary" long htmlType="submit" loading={submitting}>
            验证并登录
          </Button>
        </form>
        <p className={styles.footer}>
          <button type="button" className={styles.captchaRefresh} onClick={() => setUseRecovery((v) => !v)}>
            {useRecovery ? '改用动态验证码' : '使用恢复码'}
          </button>
        </p>
      </div>
    );
  }

  return (
    <div>
      <h1 className={styles.title}>欢迎回来</h1>
      <p className={styles.subtitle}>登录后继续你的交流</p>
      {error ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : null}
      <form onSubmit={submit}>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="username">
            用户名
          </label>
          <Input
            id="username"
            value={username}
            onChange={setUsername}
            prefix={<IconUser />}
            placeholder="请输入用户名"
            autoComplete="username"
          />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="password">
            密码
          </label>
          <Input
            id="password"
            value={password}
            onChange={setPassword}
            type={showPassword ? 'text' : 'password'}
            prefix={<IconLock />}
            placeholder="请输入密码"
            autoComplete="current-password"
            suffix={
              <button
                type="button"
                className={styles.captchaRefresh}
                onClick={() => setShowPassword((v) => !v)}
                aria-label={showPassword ? '隐藏密码' : '显示密码'}
              >
                {showPassword ? <IconEyeInvisible /> : <IconEye />}
              </button>
            }
          />
        </div>
        <Button className={styles.submit} type="primary" long htmlType="submit" loading={submitting}>
          登录
        </Button>
      </form>
      <p className={styles.footer}>
        还没有账号？
        {registerEnabled ? <Link href="/register">立即注册</Link> : <span>本站已关闭注册</span>}
      </p>
      <p className={styles.footer}>
        <Link href="/password/forgot">忘记密码？</Link>
      </p>
    </div>
  );
}
