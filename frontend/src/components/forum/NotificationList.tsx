'use client';

import { useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { Button } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import type { NotificationView } from '@/lib/api/types';
import { formatRelative } from '@/lib/format';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './NotificationList.module.css';

const LABELS: Record<string, string> = {
  reply: '回复了你的主题',
  mention: '在内容中提到了你',
  acceptance: '采纳了你的回复',
  accept: '采纳了你的回复',
  membership: '会员等级发生变化',
  title: '称号发生变化',
  moderation: '内容审核有了结果',
  report: '举报已处理',
  'report.resolved': '举报已处理',
  subscription: '订阅有新的内容',
  like: '赞了你的内容',
  follow: '关注了你',
};

function typeLabel(type: string): string {
  return LABELS[type] ?? '新的通知';
}

function targetHref(item: NotificationView): string | null {
  if (!item.threadId || item.threadId === '0') return null;
  return '/threads/' + item.threadId + (item.postId && item.postId !== '0' ? '#p' + item.postId : '');
}

export function NotificationList({ items }: { items: NotificationView[] }) {
  const router = useRouter();
  const [marking, setMarking] = useState(false);

  async function markAll() {
    setMarking(true);
    try {
      await browserSend('/me/notifications/read', { method: 'POST', body: { all: true } });
      toastSuccess('已全部标为已读');
      router.refresh();
    } catch (error) {
      toastError(error);
    } finally {
      setMarking(false);
    }
  }

  function markOne(item: NotificationView) {
    if (item.read) return;
    // 阅读上报为独立动作，不阻塞跳转；失败不影响阅读。
    void browserSend('/me/notifications/read', { method: 'POST', body: { ids: [item.id] } }).catch(() => {});
  }

  const hasUnread = items.some((item) => !item.read);

  return (
    <div className={styles.panel}>
      <div className={styles.header}>
        <span className={styles.count}>共 {items.length} 条</span>
        <Button size="small" type="secondary" loading={marking} disabled={!hasUnread} onClick={() => void markAll()}>
          全部标为已读
        </Button>
      </div>
      <ul className={styles.list}>
        {items.map((item) => {
          const href = targetHref(item);
          const content = (
            <>
              <span className={!item.read ? styles.dot : styles.dotRead} aria-hidden="true" />
              <div className={styles.body}>
                <p className={styles.line}>
                  {item.fromName ? <strong className={styles.from}>{item.fromName}</strong> : null}
                  <span className={styles.action}>{typeLabel(item.type)}</span>
                  {!item.read ? <span className={styles.unread}>未读</span> : null}
                </p>
                {item.excerpt ? <p className={styles.excerpt}>{item.excerpt}</p> : null}
                <span className={styles.time}>{formatRelative(item.createdAt)}</span>
              </div>
            </>
          );
          return (
            <li key={item.id} className={!item.read ? styles.itemUnread : styles.item}>
              {href ? (
                <Link className={styles.link} href={href} onClick={() => markOne(item)}>
                  {content}
                </Link>
              ) : (
                <div className={styles.link}>{content}</div>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
