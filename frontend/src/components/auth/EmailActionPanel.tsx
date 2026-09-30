'use client';

import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { Button } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import styles from './email-action.module.css';

type State = 'idle' | 'working' | 'ok' | 'error';

/**
 * 邮箱验证 / 换绑确认共用的动作面板。
 * 令牌一次性消费，自动提交一次；失败后不再自动重试，避免重复消费。
 */
export function EmailActionPanel({
  endpoint,
  token,
  okFallback,
  workingText,
}: {
  endpoint: string;
  token: string;
  okFallback: string;
  workingText: string;
}) {
  const [state, setState] = useState<State>(token ? 'working' : 'idle');
  const [message, setMessage] = useState('');
  const submitted = useRef(false);

  async function run() {
    setState('working');
    try {
      const result = await browserSend<{ message?: string }>(endpoint, { method: 'POST', body: { token } });
      setMessage(result?.message || okFallback);
      setState('ok');
    } catch (caught) {
      setMessage((caught as ApiError).message);
      setState('error');
    }
  }

  useEffect(() => {
    if (!token || submitted.current) return;
    submitted.current = true;
    void run();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  if (!token) {
    return (
      <div className={styles.panel}>
        <h1 className={styles.title}>链接无效</h1>
        <p className={styles.text}>缺少必要的令牌参数。请从邮件中的完整链接进入，或重新申请。</p>
        <p className={styles.links}>
          <Link href="/me/security">前往安全设置</Link>
        </p>
      </div>
    );
  }

  return (
    <div className={styles.panel}>
      {state === 'working' ? <p className={styles.text}>{workingText}</p> : null}
      {state === 'ok' ? (
        <>
          <h1 className={styles.title}>操作成功</h1>
          <p className={styles.text}>{message}</p>
          <p className={styles.links}>
            <Link href="/login">去登录</Link>
            <Link href="/">返回首页</Link>
          </p>
        </>
      ) : null}
      {state === 'error' ? (
        <>
          <h1 className={styles.title}>操作未完成</h1>
          <p className={styles.error} role="alert">
            {message}
          </p>
          <p className={styles.text}>链接可能已过期、已被使用，或不是发起换绑的设备。可重新申请后再试。</p>
          <p className={styles.links}>
            <Button size="small" type="secondary" onClick={() => void run()}>
              重试
            </Button>
            <Link href="/me/security">安全设置</Link>
            <Link href="/">返回首页</Link>
          </p>
        </>
      ) : null}
    </div>
  );
}
