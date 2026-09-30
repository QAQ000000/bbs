'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { Button, Input } from '@arco-design/web-react';
import { IconEmail, IconLock, IconSafe, IconUser } from '@arco-design/web-react/icon';
import { browserGet, browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import { useSession } from '@/lib/auth/session';
import type { CurrentUser } from '@/lib/api/types';
import { toastSuccess } from '../ui/feedback';
import styles from './auth-forms.module.css';

type Phase = 'idle' | 'submitting' | 'success' | 'installed';

/** 安装向导：只在站点尚未初始化（GET /setup required=true）时由页面渲染。 */
export function SetupForm() {
  const router = useRouter();
  const { refresh } = useSession();
  const [siteName, setSiteName] = useState('');
  const [username, setUsername] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [phase, setPhase] = useState<Phase>('idle');
  const [error, setError] = useState<string | null>(null);

  const busy = phase === 'submitting';

  /** 提交超时或结果不明时先重读安装状态，不直接重复提交。 */
  async function rereadState(): Promise<boolean> {
    try {
      const state = await browserGet<{ required: boolean }>('/setup');
      return state?.required === false;
    } catch {
      return false;
    }
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    const name = siteName.trim();
    const account = username.trim();
    if (!name || name.length > 40) {
      setError('站点名称不能为空且不超过 40 字');
      return;
    }
    if (!/^[\u4e00-\u9fa5a-zA-Z0-9_]{2,15}$/.test(account)) {
      setError('用户名需为 2-15 位中文、字母、数字或下划线');
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
    if (email.trim() && !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email.trim())) {
      setError('邮箱格式不正确');
      return;
    }

    setPhase('submitting');
    try {
      const user = await browserSend<CurrentUser>('/setup', {
        method: 'POST',
        body: {
          site_name: name,
          username: account,
          email: email.trim(),
          password,
          confirm_password: confirm,
        },
      });
      await refresh();
      setPhase('success');
      toastSuccess('安装完成');
      // 按实际登录结果跳转：安装接口会签发管理员会话。
      if (user?.mustChangePassword) {
        router.push('/me/security');
      } else {
        router.push('/admin');
      }
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      if (err.code === 'ALREADY_INITIALIZED' || err.status === 409) {
        setPhase('installed');
        return;
      }
      // 网络错误 / 超时：先查询真实状态，避免重复初始化。
      if (err.status === 0) {
        if (await rereadState()) {
          setPhase('installed');
          return;
        }
        setError('提交结果未知（网络异常）。已重新读取安装状态，站点仍未初始化，可再次提交。');
        setPhase('idle');
        return;
      }
      setError(err.message);
      setPhase('idle');
    }
  }

  if (phase === 'installed') {
    return (
      <div>
        <h1 className={styles.title}>站点已初始化</h1>
        <p className={styles.subtitle}>该站点已经完成安装，不能再次进入初始化流程。</p>
        <p className={styles.footer}>
          <Link href="/login">前往登录</Link>
        </p>
      </div>
    );
  }

  if (phase === 'success') {
    return (
      <div>
        <h1 className={styles.title}>安装完成</h1>
        <p className={styles.subtitle}>已创建初始管理员并登录，正在进入管理后台…</p>
        <p className={styles.footer}>
          如果没有自动跳转，请<Link href="/admin">手动进入后台</Link>或<Link href="/login">重新登录</Link>。
        </p>
      </div>
    );
  }

  return (
    <div>
      <h1 className={styles.title}>安装 GoBBS</h1>
      <p className={styles.subtitle}>填写站点信息与初始管理员账号。数据库连接由部署环境提供，此处不收集数据库凭据。</p>
      {error ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : null}
      <form onSubmit={submit}>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="setup-site">
            站点名称
            <span className={styles.required}>*</span>
          </label>
          <Input
            id="setup-site"
            value={siteName}
            onChange={setSiteName}
            prefix={<IconSafe />}
            placeholder="不超过 40 字，例如：GoBBS 社区"
            maxLength={40}
          />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="setup-username">
            管理员用户名
            <span className={styles.required}>*</span>
          </label>
          <Input
            id="setup-username"
            value={username}
            onChange={setUsername}
            prefix={<IconUser />}
            placeholder="2-15 位中文、字母、数字或下划线"
            autoComplete="username"
          />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="setup-email">
            管理员邮箱
          </label>
          <Input
            id="setup-email"
            value={email}
            onChange={setEmail}
            prefix={<IconEmail />}
            placeholder="选填，用于找回密码与通知"
            autoComplete="email"
          />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="setup-password">
            管理员密码
            <span className={styles.required}>*</span>
          </label>
          <Input
            id="setup-password"
            value={password}
            onChange={setPassword}
            type="password"
            prefix={<IconLock />}
            placeholder="至少 8 位"
            autoComplete="new-password"
          />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="setup-confirm">
            确认密码
            <span className={styles.required}>*</span>
          </label>
          <Input
            id="setup-confirm"
            value={confirm}
            onChange={setConfirm}
            type="password"
            prefix={<IconLock />}
            placeholder="再次输入密码"
            autoComplete="new-password"
          />
        </div>
        <p className={styles.hint}>
          安装会创建首个管理员并自动登录。并发初始化以后端结果为准：先完成的一次生效，其余请求会返回“站点已初始化”。
        </p>
        <Button type="primary" htmlType="submit" long loading={busy} className={styles.submit}>
          {busy ? '正在安装…' : '开始安装'}
        </Button>
      </form>
      <p className={styles.footer}>
        已经安装过？<Link href="/login">直接登录</Link>
      </p>
    </div>
  );
}
