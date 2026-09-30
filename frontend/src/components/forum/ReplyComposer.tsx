'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { Button } from '@arco-design/web-react';
import { browserGet, browserSend } from '@/lib/api/browser';
import type { SmileyMap } from '@/lib/markdown/smiley';
import { MarkdownEditor } from '../content/MarkdownEditor';
import { useServerDraft } from '../content/useDraft';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './ReplyComposer.module.css';

export interface ReplyComposerProps {
  threadId: string;
  smileys: SmileyMap;
  replyTo?: { id: string; floor: number; authorName: string } | null;
  canReply: boolean;
  closed: boolean;
  loggedIn: boolean;
  uploadEnabled: boolean;
}

export function ReplyComposer({
  threadId,
  smileys,
  replyTo,
  canReply,
  closed,
  loggedIn,
  uploadEnabled,
}: ReplyComposerProps) {
  const router = useRouter();
  const [content, setContent] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const { restored, save, clear } = useServerDraft('reply:' + threadId, loggedIn);

  useEffect(() => {
    if (restored?.content) {
      setContent((prev) => prev || restored.content);
    }
  }, [restored]);

  useEffect(() => {
    if (content) save(content);
  }, [content, save]);

  async function submit() {
    const value = content.trim();
    if (!value) {
      toastError(new Error('回复内容不能为空'));
      return;
    }
    setSubmitting(true);
    setNotice(null);
    try {
      const body: Record<string, unknown> = { content: value };
      if (replyTo) body.replyToPostId = replyTo.id;
      const result = await browserSend<{ postId: string; pending: boolean }>(
        '/threads/' + threadId + '/posts',
        { method: 'POST', body },
      );
      clear();
      setContent('');
      if (result.pending) {
        setNotice('回复已提交，等待审核通过后公开显示。');
        toastSuccess('已提交，等待审核');
        return;
      }
      toastSuccess('回复成功');
      const position = await browserGet<{ page: number }>('/posts/' + result.postId + '/position').catch(() => null);
      const targetPage = position?.page ?? 1;
      router.push('/threads/' + threadId + '?page=' + targetPage + '#p' + result.postId);
      router.refresh();
    } catch (error) {
      // 失败保留用户输入，用户可修正后重试。
      toastError(error);
    } finally {
      setSubmitting(false);
    }
  }

  if (!loggedIn) {
    return (
      <section id="reply" className={[styles.panel, 'panel'].join(' ')}>
        <p className={styles.tip}>登录后参与讨论，回复支持 Markdown。</p>
        <Link className={styles.loginLink} href={'/login?next=' + encodeURIComponent('/threads/' + threadId)}>
          去登录
        </Link>
      </section>
    );
  }

  if (closed) {
    return (
      <section id="reply" className={[styles.panel, 'panel'].join(' ')}>
        <p className={styles.tip}>该主题已关闭，暂不可回复。</p>
      </section>
    );
  }

  if (!canReply) {
    return (
      <section id="reply" className={[styles.panel, 'panel'].join(' ')}>
        <p className={styles.tip}>当前账号暂时不能回复该主题。</p>
      </section>
    );
  }

  return (
    <section id="reply" className={styles.composer} aria-label="回复主题">
      {replyTo ? (
        <div className={styles.replyTo}>
          <span>
            正在引用 {replyTo.floor} 楼 {replyTo.authorName}
          </span>
          <Link className={styles.cancel} href={'/threads/' + threadId + '#reply'}>
            取消引用
          </Link>
        </div>
      ) : null}
      {notice ? <p className={styles.notice}>{notice}</p> : null}
      <MarkdownEditor
        value={content}
        onChange={setContent}
        smileys={smileys}
        uploadEnabled={uploadEnabled}
        minRows={5}
        ariaLabel="回复内容"
        placeholder="写下你的回复…支持 Markdown 语法"
        footer={
          <Button type="primary" loading={submitting} onClick={() => void submit()}>
            发布回复
          </Button>
        }
      />
    </section>
  );
}
