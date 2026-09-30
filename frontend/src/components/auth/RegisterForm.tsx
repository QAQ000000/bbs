'use client';

import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { Button, Checkbox, Input } from '@arco-design/web-react';
import { IconEmail, IconLock, IconSafe, IconUser } from '@arco-design/web-react/icon';
import { browserGet, browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import { useSession } from '@/lib/auth/session';
import type { CurrentUser } from '@/lib/api/types';
import styles from './auth-forms.module.css';

export interface RegisterFormProps {
  captchaEnabled: boolean;
  requireConsent: boolean;
  termsContent?: string;
  privacyContent?: string;
}

interface Captcha {
  id: string;
  url: string;
}

export function RegisterForm({ captchaEnabled, requireConsent }: RegisterFormProps) {
  const router = useRouter();
  const { refresh } = useSession();
  const [username, setUsername] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [captchaCode, setCaptchaCode] = useState('');
  const [captcha, setCaptcha] = useState<Captcha | null>(null);
  const [agreed, setAgreed] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const loadCaptcha = useCallback(async () => {
    try {
      const data = await browserGet<Captcha>('/auth/captcha');
      setCaptcha(data);
      setCaptchaCode('');
    } catch {
      setCaptcha(null);
    }
  }, []);

  useEffect(() => {
    if (captchaEnabled) void loadCaptcha();
  }, [captchaEnabled, loadCaptcha]);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    if (!/^[\u4e00-\u9fa5a-zA-Z0-9_]{2,15}$/.test(username.trim())) {
      setError('用户名需为 2-15 位中文、字母、数字或下划线');
      return;
    }
    if (email.trim() && !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email.trim())) {
      setError('邮箱格式不正确');
      return;
    }
    if (password.length < 8) {
      setError('密码至少 8 位');
      return;
    }
    if (password !== confirm) {
      setError('两次输入的密码不一致');
      return;
    }
    if (requireConsent && !agreed) {
      setError('请先阅读并同意服务条款与隐私政策');
      return;
    }
    if (captchaEnabled && !captcha) {
      setError('验证码加载失败，请点击图片刷新');
      return;
    }
    setSubmitting(true);
    try {
      const user = await browserSend<CurrentUser>('/auth/register', {
        method: 'POST',
        body: {
          username: username.trim(),
          email: email.trim(),
          password,
          consent: agreed ? '1' : '',
          captcha_id: captcha?.id ?? '',
          captcha: captchaCode.trim(),
        },
      });
      await refresh();
      void user;
      router.push('/');
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      setError(err.message);
      if (captchaEnabled) void loadCaptcha();
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div>
      <h1 className={styles.title}>创建账号</h1>
      <p className={styles.subtitle}>加入社区，开始你的交流与分享</p>
      {error ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : null}
      <form onSubmit={submit}>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="reg-username">
            用户名
            <span className={styles.required}>*</span>
          </label>
          <Input
            id="reg-username"
            value={username}
            onChange={setUsername}
            prefix={<IconUser />}
            placeholder="2-15 位中文、字母、数字或下划线"
            autoComplete="username"
          />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="reg-email">
            邮箱
          </label>
          <Input
            id="reg-email"
            value={email}
            onChange={setEmail}
            prefix={<IconEmail />}
            placeholder="选填，用于找回密码与通知"
            autoComplete="email"
          />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="reg-password">
            密码
            <span className={styles.required}>*</span>
          </label>
          <Input
            id="reg-password"
            value={password}
            onChange={setPassword}
            type="password"
            prefix={<IconLock />}
            placeholder="至少 8 位"
            autoComplete="new-password"
          />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="reg-confirm">
            确认密码
            <span className={styles.required}>*</span>
          </label>
          <Input
            id="reg-confirm"
            value={confirm}
            onChange={setConfirm}
            type="password"
            prefix={<IconLock />}
            placeholder="再次输入密码"
            autoComplete="new-password"
          />
        </div>
        {captchaEnabled ? (
          <div className={styles.field}>
            <label className={styles.label} htmlFor="reg-captcha">
              图形验证码
              <span className={styles.required}>*</span>
            </label>
            <div className={styles.captchaRow}>
              <Input
                id="reg-captcha"
                value={captchaCode}
                onChange={setCaptchaCode}
                prefix={<IconSafe />}
                placeholder="算式结果"
              />
              {captcha ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img
                  className={styles.captchaImage}
                  src={captcha.url}
                  alt="图形验证码，点击刷新"
                  onClick={() => void loadCaptcha()}
                />
              ) : null}
              <button type="button" className={styles.captchaRefresh} onClick={() => void loadCaptcha()}>
                换一张
              </button>
            </div>
          </div>
        ) : null}
        {requireConsent ? (
          <div className={styles.consent}>
            <Checkbox checked={agreed} onChange={setAgreed}>
              <span>
                我已阅读并同意
                <Link href="/terms" target="_blank">
                  服务条款
                </Link>
                与
                <Link href="/privacy" target="_blank">
                  隐私政策
                </Link>
              </span>
            </Checkbox>
          </div>
        ) : null}
        <Button className={styles.submit} type="primary" long htmlType="submit" loading={submitting}>
          注册
        </Button>
      </form>
      <p className={styles.footer}>
        已有账号？<Link href="/login">去登录</Link>
      </p>
    </div>
  );
}
