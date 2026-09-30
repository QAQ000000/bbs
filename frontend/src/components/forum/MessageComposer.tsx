'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { ConversationView } from '@/lib/api/types';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './MessageComposer.module.css';

export interface MessageComposerProps {
  conversation: ConversationView;
  latestMessageId?: string;
}

export function MessageComposer({ conversation, latestMessageId }: MessageComposerProps) {
  const router = useRouter();
  const [body, setBody] = useState('');
  const [sending, setSending] = useState(false);
  const [blocking, setBlocking] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);

  // 读取会话本身不标记已读，进入页面后显式上报最新消息游标。
  useEffect(() => {
    if (conversation.unread > 0 && latestMessageId) {
      void browserSend('/conversations/' + conversation.id + '/read', {
        method: 'POST',
        body: { messageId: latestMessageId },
      }).catch(() => {});
    }
  }, [conversation.id, conversation.unread, latestMessageId]);

  async function send() {
    const value = body.trim();
    if (!value) {
      toastError(new Error('消息内容不能为空'));
      return;
    }
    setSending(true);
    setNotice(null);
    try {
      await browserSend('/users/' + conversation.otherId + '/messages', { method: 'POST', body: { body: value } });
      setBody('');
      toastSuccess('已发送');
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      // 首条限制与屏蔽错误原样转成中文状态，不推断对方设置。
      setNotice(err.message);
      toastError(err);
    } finally {
      setSending(false);
    }
  }

  async function toggleBlock() {
    setBlocking(true);
    try {
      if (conversation.blocked) {
        await browserSend('/conversations/' + conversation.id + '/block', { method: 'DELETE' });
        toastSuccess('已解除屏蔽');
      } else {
        await browserSend('/conversations/' + conversation.id + '/block', { method: 'POST' });
        toastSuccess('已屏蔽该会话');
      }
      router.refresh();
    } catch (error) {
      toastError(error);
    } finally {
      setBlocking(false);
    }
  }

  const disabled = conversation.blocked || conversation.waitingForReply;

  return (
    <div className={styles.composer}>
      {conversation.blocked ? (
        <p className={styles.banner}>
          你已屏蔽该会话，双方都无法继续发送消息。解除屏蔽不会恢复关注，也不会清除首条等待状态。
        </p>
      ) : null}
      {!conversation.blocked && conversation.waitingForReply ? (
        <p className={styles.banner}>你已发送首条消息，等待对方回复后才能继续发送。</p>
      ) : null}
      {notice ? <p className={styles.error}>{notice}</p> : null}
      <textarea
        className={styles.textarea}
        value={body}
        onChange={(event) => setBody(event.target.value)}
        rows={3}
        maxLength={5000}
        placeholder={disabled ? '当前无法发送消息' : '输入消息内容…'}
        disabled={disabled}
        aria-label="私信内容"
      />
      <div className={styles.actions}>
        <Button size="small" type={conversation.blocked ? 'secondary' : 'text'} loading={blocking} onClick={() => void toggleBlock()}>
          {conversation.blocked ? '解除屏蔽' : '屏蔽会话'}
        </Button>
        <Button type="primary" loading={sending} disabled={disabled} onClick={() => void send()}>
          发送
        </Button>
      </div>
    </div>
  );
}
