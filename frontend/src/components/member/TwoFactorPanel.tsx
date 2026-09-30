'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Input } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { MfaStatus } from '@/lib/api/types';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './member-forms.module.css';

interface SetupResult {
  secret: string;
  setupId: string;
  otpauthUrl: string;
}

interface EnableResult {
  enabled: boolean;
  recoveryCodes?: string[];
}

export function TwoFactorPanel({ status }: { status: MfaStatus }) {
  const router = useRouter();
  const [enabled, setEnabled] = useState(status.enabled);
  const [password, setPassword] = useState('');
  const [setup, setSetup] = useState<SetupResult | null>(null);
  const [code, setCode] = useState('');
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function startSetup() {
    setError(null);
    setBusy(true);
    try {
      const result = await browserSend<SetupResult>('/me/2fa/setup', { method: 'POST', body: { password } });
      setSetup(result);
      setPassword('');
    } catch (caught) {
      setError((caught as ApiError).message);
    } finally {
      setBusy(false);
    }
  }

  async function enable() {
    if (!setup) return;
    setError(null);
    setBusy(true);
    try {
      const result = await browserSend<EnableResult>('/me/2fa/enable', {
        method: 'POST',
        body: { setupId: setup.setupId, code, password },
      });
      setEnabled(true);
      setSetup(null);
      setCode('');
      setPassword('');
      if (result.recoveryCodes && result.recoveryCodes.length > 0) {
        setRecoveryCodes(result.recoveryCodes);
      }
      toastSuccess('两步验证已开启');
      router.refresh();
    } catch (caught) {
      setError((caught as ApiError).message);
    } finally {
      setBusy(false);
    }
  }

  async function disable() {
    setError(null);
    setBusy(true);
    try {
      await browserSend('/me/2fa/disable', { method: 'POST', body: { password: password, code } });
      setEnabled(false);
      setPassword('');
      setCode('');
      setRecoveryCodes(null);
      toastSuccess('两步验证已关闭');
      router.refresh();
    } catch (caught) {
      setError((caught as ApiError).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <p className={styles.twoFaState}>
        当前状态：<strong>{enabled ? '已开启' : '未开启'}</strong>
        {enabled ? '，登录时需要额外的动态验证码。' : '，建议开启以提升账号安全。'}
      </p>
      {error ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : null}
      {recoveryCodes ? (
        <div className={styles.recovery}>
          <p className={styles.recoveryTitle}>请立即保存恢复码（只显示一次）</p>
          <ul className={styles.recoveryList}>
            {recoveryCodes.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </div>
      ) : null}

      {!enabled && !setup ? (
        <div className={styles.inlineForm}>
          <Input.Password
            value={password}
            onChange={setPassword}
            placeholder="输入当前密码以开始设置"
            style={{ maxWidth: 260 }}
          />
          <Button type="primary" loading={busy} onClick={() => void startSetup()}>
            开启两步验证
          </Button>
        </div>
      ) : null}

      {!enabled && setup ? (
        <div className={styles.setupBox}>
          <p className={styles.setupHint}>
            在验证器应用中添加密钥（或手动输入 otpauth 链接），然后输入生成的 6 位验证码。
          </p>
          <p className={styles.secret}>{setup.secret}</p>
          <p className={styles.otpauth}>{setup.otpauthUrl}</p>
          <div className={styles.inlineForm}>
            <Input value={code} onChange={setCode} placeholder="6 位验证码" maxLength={6} style={{ maxWidth: 160 }} />
            <Button type="primary" loading={busy} onClick={() => void enable()}>
              确认开启
            </Button>
            <Button type="secondary" onClick={() => setSetup(null)}>
              取消
            </Button>
          </div>
        </div>
      ) : null}

      {enabled ? (
        <div className={styles.inlineForm}>
          <Input.Password value={password} onChange={setPassword} placeholder="当前密码" style={{ maxWidth: 200 }} />
          <Input value={code} onChange={setCode} placeholder="动态码 / 恢复码" style={{ maxWidth: 180 }} />
          <Button type="secondary" loading={busy} onClick={() => void disable()}>
            关闭两步验证
          </Button>
        </div>
      ) : null}
    </div>
  );
}
