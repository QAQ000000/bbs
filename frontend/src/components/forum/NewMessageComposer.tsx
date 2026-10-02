'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { toastError } from '../ui/feedback';
import styles from './NewMessageComposer.module.css';

export function NewMessageComposer({ userId, username }: { userId: string; username: string }) {
  const router = useRouter();
  const [body, setBody] = useState('');
  const [sending, setSending] = useState(false);

  async function send() {
    const message = body.trim();
    if (!message) {
      toastError(new Error('消息内容不能为空'));
      return;
    }
    setSending(true);
    try {
      const result = await browserSend<{ conversationId: string }>('/users/' + userId + '/messages', {
        method: 'POST',
        body: { body: message },
      });
      router.replace('/me/messages?cid=' + encodeURIComponent(result.conversationId));
      router.refresh();
    } catch (error) {
      toastError(error);
    } finally {
      setSending(false);
    }
  }

  return (
    <div className={styles.composer}>
      <h2>发私信给 {username}</h2>
      <p className={styles.note}>发送后会建立会话；对方回复前，你只能发送这一条。</p>
      <textarea
        className={styles.textarea}
        value={body}
        onChange={(event) => setBody(event.target.value)}
        rows={5}
        maxLength={5000}
        placeholder="输入消息内容"
        aria-label={'发给 ' + username + ' 的私信内容'}
      />
      <div className={styles.actions}>
        <span>{body.length}/5000</span>
        <Button type="primary" loading={sending} onClick={() => void send()}>
          发送
        </Button>
      </div>
    </div>
  );
}
