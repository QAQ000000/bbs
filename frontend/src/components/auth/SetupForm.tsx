'use client';

import { useRef, useState } from 'react';
import Link from 'next/link';
import { Button, Input } from '@arco-design/web-react';
import { IconEmail, IconLock, IconSafe, IconUser } from '@arco-design/web-react/icon';
import { browserGet, browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import { useSession } from '@/lib/auth/session';
import type { CurrentUser, SetupState } from '@/lib/api/types';
import styles from './auth-forms.module.css';
import wizard from './SetupForm.module.css';

const STEPS = ['数据库与站点', '管理员账号', '邮件与安全', '完成检查'];
type Phase = 'idle' | 'submitting' | 'success' | 'installed' | 'unknown';

export function SetupForm({ initialState }: { initialState: SetupState }) {
  const { refresh } = useSession();
  const [state, setState] = useState(initialState);
  const [step, setStep] = useState(0);
  const [siteName, setSiteName] = useState('');
  const [username, setUsername] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [phase, setPhase] = useState<Phase>('idle');
  const [checking, setChecking] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [destination, setDestination] = useState('/admin');
  const heading = useRef<HTMLHeadingElement>(null);
  const busy = phase === 'submitting' || checking;

  function go(next: number) {
    setError(null);
    setStep(next);
    requestAnimationFrame(() => heading.current?.focus());
  }

  function validate(stage: number): string | null {
    if (stage === 0 && (!siteName.trim() || [...siteName.trim()].length > 40)) return '站点名称不能为空且不超过 40 字';
    if (stage === 1) {
      if (!/^[\u4e00-\u9fa5a-zA-Z0-9_]{2,15}$/.test(username.trim())) return '用户名需为 2-15 位中文、字母、数字或下划线';
      if ([...password].length < 8) return '密码至少 8 位';
      if (new TextEncoder().encode(password).length > 72) return '密码不能超过 72 字节';
      if (password !== confirm) return '两次输入的密码不一致';
      if (email.trim() && !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email.trim())) return '邮箱格式不正确';
    }
    return null;
  }

  async function checkState(): Promise<SetupState | null> {
    setChecking(true);
    setError(null);
    try {
      const latest = await browserGet<SetupState>('/setup');
      setState(latest);
      if (!latest.required) setPhase('installed');
      else if (phase === 'unknown') setPhase('idle');
      return latest;
    } catch {
      setError('无法检查数据库与安装状态，请恢复连接后重新检查。');
      return null;
    } finally {
      setChecking(false);
    }
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (busy || phase === 'unknown') return;
    const invalid = validate(step === 0 ? 0 : 1);
    if (invalid) { setError(invalid); return; }
    if (step < 3) {
      if (step === 0) {
        const latest = await checkState();
        if (!latest?.required) return;
      }
      go(step + 1);
      return;
    }
    const invalidSite = validate(0);
    if (invalidSite) { go(0); setError(invalidSite); return; }
    setPhase('submitting');
    setError(null);
    try {
      const user = await browserSend<CurrentUser>('/setup', {
        method: 'POST',
        body: { site_name: siteName.trim(), username: username.trim(), email: email.trim(), password, confirm_password: confirm },
      });
      setPassword('');
      setConfirm('');
      setDestination(user?.mustChangePassword ? '/me/security' : '/admin');
      setPhase('success');
      // A session refresh failure cannot turn a committed installation into a retry.
      await refresh().catch(() => undefined);
    } catch (caught) {
      const err = caught as ApiError;
      if (err.code === 'ALREADY_INITIALIZED' || err.status === 409) { setPhase('installed'); return; }
      if (err.status === 0 || err.status >= 500) {
        setPhase('unknown');
        const latest = await checkState();
        if (latest?.required) {
          setPhase('idle');
          setError('上次请求未完成，已确认站点仍未初始化。可以重新提交。');
        }
        return;
      }
      setError(err.message);
      setPhase('idle');
    }
  }

  if (phase === 'installed' || phase === 'success') {
    return <div role="status">
      <h1 className={styles.title}>{phase === 'success' ? '安装完成' : '站点已初始化'}</h1>
      <p className={wizard.description}>{phase === 'success' ? '站点和初始管理员已创建，安装入口已关闭。' : '站点已完成安装，请登录后查看实际状态。'}</p>
      <Link href={phase === 'success' ? destination : '/login'} className={wizard.finish}>{phase === 'success' ? '进入管理后台' : '前往登录'}</Link>
    </div>;
  }

  return <div>
    <h1 className={styles.title}>安装 GoBBS</h1>
    <ol className={wizard.steps} aria-label="安装进度">
      {STEPS.map((label, index) => <li key={label} aria-current={step === index ? 'step' : undefined} className={index <= step ? wizard.active : ''}><span>{index + 1}</span>{label}</li>)}
    </ol>
    <h2 className={wizard.heading} tabIndex={-1} ref={heading}>第 {step + 1} 步 · {STEPS[step]}</h2>
    {error ? <p id="setup-error" className={[styles.error, wizard.alert].join(' ')} role="alert">{error}</p> : null}
    {phase === 'unknown' ? <p className={[styles.error, wizard.alert].join(' ')} role="alert">提交结果尚未确认。请先重新检查安装状态。</p> : null}
    <form onSubmit={submit} aria-describedby={error ? 'setup-error' : undefined}>
      <fieldset disabled={busy || phase === 'unknown'} className={wizard.fields}>
        {step === 0 ? <>
          <dl className={wizard.checks}>
            <div><dt>数据库</dt><dd>PostgreSQL · 已连接</dd></div>
            <div><dt>迁移版本</dt><dd>Schema {state.schema}</dd></div>
          </dl>
          <div className={styles.field}><label className={styles.label} htmlFor="setup-site">站点名称 <span className={styles.required}>*</span></label><Input id="setup-site" value={siteName} onChange={setSiteName} prefix={<IconSafe />} placeholder="不超过 40 字" autoComplete="organization" /></div>
        </> : null}
        {step === 1 ? <>
          <div className={styles.field}><label className={styles.label} htmlFor="setup-username">管理员用户名 <span className={styles.required}>*</span></label><Input id="setup-username" value={username} onChange={setUsername} prefix={<IconUser />} placeholder="2-15 位中文、字母、数字或下划线" autoComplete="username" /></div>
          <div className={styles.field}><label className={styles.label} htmlFor="setup-email">管理员邮箱</label><Input id="setup-email" value={email} onChange={setEmail} prefix={<IconEmail />} placeholder="选填，用于找回密码与通知" autoComplete="email" /></div>
          <div className={styles.field}><label className={styles.label} htmlFor="setup-password">管理员密码 <span className={styles.required}>*</span></label><Input id="setup-password" value={password} onChange={setPassword} type="password" prefix={<IconLock />} placeholder="至少 8 位，不超过 72 字节" autoComplete="new-password" /></div>
          <div className={styles.field}><label className={styles.label} htmlFor="setup-confirm">确认密码 <span className={styles.required}>*</span></label><Input id="setup-confirm" value={confirm} onChange={setConfirm} type="password" prefix={<IconLock />} placeholder="再次输入密码" autoComplete="new-password" /></div>
        </> : null}
        {step === 2 ? <>
          <dl className={wizard.checks}><div><dt>邮件服务</dt><dd>{state.smtpEnabled ? '已配置 SMTP' : '未配置 SMTP'}</dd></div><div><dt>安全 Cookie</dt><dd>{state.secureCookies ? '已启用' : '未启用（开发模式）'}</dd></div></dl>
          {!state.smtpEnabled ? <p className={wizard.description}>邮箱验证和密码找回需要部署 SMTP 后才能投递邮件。</p> : null}
          {!state.secureCookies ? <p className={wizard.description}>上线前请配置 HTTPS 并开启生产模式，保护管理员会话。</p> : null}
        </> : null}
        {step === 3 ? <>
          <dl className={wizard.checks}><div><dt>站点名称</dt><dd>{siteName.trim()}</dd></div><div><dt>管理员</dt><dd>{username.trim()}</dd></div><div><dt>邮箱</dt><dd>{email.trim() || '未填写'}</dd></div><div><dt>数据库</dt><dd>已连接 · Schema {state.schema}</dd></div></dl>
          <p className={wizard.description}>确认后创建初始管理员并登录。安装只能完成一次。</p>
        </> : null}
        <div className={wizard.actions}>
          {step > 0 ? <Button htmlType="button" onClick={() => go(step - 1)}>上一步</Button> : null}
          <Button type="primary" htmlType="submit" loading={busy}>{step === 3 ? '确认安装' : '下一步'}</Button>
        </div>
      </fieldset>
    </form>
    <div className={wizard.footer}><Button type="text" loading={checking} disabled={phase === 'submitting'} onClick={() => void checkState()}>重新检查环境</Button><Link href="/login">前往登录</Link></div>
  </div>;
}
