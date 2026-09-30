'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { Button, Input } from '@arco-design/web-react';
import { browserGet, browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { PostView, ThreadSummary } from '@/lib/api/types';
import type { SmileyMap } from '@/lib/markdown/smiley';
import { MarkdownEditor } from './MarkdownEditor';
import { useServerDraft } from './useDraft';
import { DeletePostButton } from '../forum/DeletePostButton';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './EditPostForm.module.css';

export interface EditPostFormProps {
  post: PostView;
  threadTitle: string;
  forumId: string;
  smileys: SmileyMap;
  uploadEnabled: boolean;
}

/** 服务端快照：仅用于冲突对比，不代表本地内容所基于的版本。 */
interface Snapshot {
  version: number;
  content: string;
  subject: string;
}

export function EditPostForm({ post, threadTitle, forumId, smileys, uploadEnabled }: EditPostFormProps) {
  const router = useRouter();
  const isFirstFloor = post.floor === 1;

  const [subject, setSubject] = useState(threadTitle);
  const [content, setContent] = useState(post.content);
  /**
   * baseVersion 是“本地内容所基于的版本”，只有用户明确确认合并 / 覆盖，
   * 或保存成功后才会推进。刷新只更新 latest 对比数据，绝不静默推进 baseVersion，
   * 避免用新版本号提交旧内容覆盖他人修改。
   */
  const [baseVersion, setBaseVersion] = useState(post.version);
  const [latest, setLatest] = useState<Snapshot>({
    version: post.version,
    content: post.content,
    subject: threadTitle,
  });
  const [conflict, setConflict] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const { restored, save, clear } = useServerDraft('edit:' + post.id, true);

  useEffect(() => {
    if (restored?.content && restored.content !== post.content) {
      setContent((prev) => (prev === post.content ? restored.content : prev));
    }
  }, [restored, post.content]);

  useEffect(() => {
    if (content) save(content, isFirstFloor ? subject : '');
  }, [content, subject, isFirstFloor, save]);

  /** 冲突后拉取服务端权威快照；只更新对比数据，不动 baseVersion 与本地内容。 */
  async function refreshLatest() {
    const [freshPost, freshThread] = await Promise.all([
      browserGet<PostView>('/posts/' + post.id).catch(() => null),
      isFirstFloor
        ? browserGet<ThreadSummary>('/threads/' + post.threadId).catch(() => null)
        : Promise.resolve(null),
    ]);
    setLatest((prev) => ({
      version: freshPost?.version ?? prev.version,
      content: freshPost?.content ?? prev.content,
      subject: freshThread?.title ?? prev.subject,
    }));
    return freshPost;
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    setNotice(null);
    if (conflict) {
      setError('存在未处理的版本冲突，请先对比并确认合并或覆盖。');
      return;
    }
    const body = content.trim();
    const title = subject.trim();
    if (isFirstFloor && !title) {
      setError('请输入标题');
      return;
    }
    if (!body) {
      setError('内容不能为空');
      return;
    }
    setSubmitting(true);
    try {
      const result = await browserSend<{ version: number; pending: boolean }>('/posts/' + post.id, {
        method: 'PATCH',
        body: { version: baseVersion, subject: isFirstFloor ? title : '', content: body },
      });
      clear();
      setBaseVersion(result.version);
      if (result.pending) {
        setNotice('修改已提交，等待审核通过后公开显示。');
        toastSuccess('已提交，等待审核');
        return;
      }
      toastSuccess('保存成功');
      router.push('/threads/' + post.threadId + '#p' + post.id);
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      if (err.status === 409) {
        const fresh = await refreshLatest();
        setConflict(true);
        setError(
          '内容已被他人更新（服务端当前版本 v' +
            (fresh?.version ?? '未知') +
            '，你基于版本 v' +
            baseVersion +
            '）。本地修改仍然保留，请对比后确认。',
        );
      } else {
        setError(err.message);
      }
      toastError(err);
    } finally {
      setSubmitting(false);
    }
  }

  /** 明确确认：本地内容已合并，采用服务端最新版本作为新的基础版本。 */
  function adoptLatestAsBase() {
    setBaseVersion(latest.version);
    setConflict(false);
    setError(null);
    setNotice('已采用服务端最新版本 v' + latest.version + ' 作为基础版本，请确认内容后保存。');
  }

  /** 明确放弃本地修改：载入服务端最新内容与标题。 */
  function discardLocal() {
    setContent(latest.content);
    setSubject(latest.subject);
    setBaseVersion(latest.version);
    setConflict(false);
    setError(null);
    setNotice('已载入服务端最新版本 v' + latest.version + '。');
  }

  const titleChanged = isFirstFloor && latest.subject !== subject;

  return (
    <form className={styles.grid} onSubmit={submit}>
      <div className={styles.main}>
        <div className={['panel', styles.card].join(' ')}>
          {conflict ? (
            <div className={styles.conflict}>
              <p className={styles.conflictTitle}>内容已被他人更新</p>
              <p className={styles.conflictText}>
                服务端当前版本 v{latest.version}，你的本地内容基于 v{baseVersion}。本地修改仍然保留；
                请先对比，确认合并或覆盖后才会用新版本提交。
              </p>
              <div className={styles.compare}>
                <div className={styles.compareCol}>
                  <p className={styles.compareHead}>我的内容（基于 v{baseVersion}）</p>
                  <pre className={styles.compareBody}>{content}</pre>
                </div>
                <div className={styles.compareCol}>
                  <p className={styles.compareHead}>服务端最新（v{latest.version}）</p>
                  <pre className={styles.compareBody}>{latest.content}</pre>
                </div>
              </div>
              {titleChanged ? (
                <p className={styles.titleDiff}>
                  标题差异：我的「{subject || '（空）'}」/ 服务端「{latest.subject || '（空）'}」
                </p>
              ) : null}
              <div className={styles.conflictActions}>
                <Button size="small" type="primary" onClick={adoptLatestAsBase}>
                  我已合并，采用最新版本
                </Button>
                <Button size="small" type="secondary" onClick={discardLocal}>
                  放弃本地修改，载入服务端最新
                </Button>
              </div>
            </div>
          ) : null}

          {isFirstFloor ? (
            <div className={styles.field}>
              <label className={styles.label} htmlFor="edit-subject">
                标题
              </label>
              <Input id="edit-subject" value={subject} onChange={setSubject} maxLength={80} showWordLimit />
            </div>
          ) : null}

          <div className={styles.field}>
            <label className={styles.label} htmlFor="edit-content">
              正文（基础版本 v{baseVersion}）
            </label>
            <MarkdownEditor
              value={content}
              onChange={setContent}
              smileys={smileys}
              uploadEnabled={uploadEnabled}
              minRows={12}
              ariaLabel="编辑正文"
            />
          </div>

          {notice ? <p className={styles.notice}>{notice}</p> : null}
          {error ? (
            <p className={styles.error} role="alert">
              {error}
            </p>
          ) : null}

          <div className={styles.actions}>
            <Button
              type="primary"
              htmlType="submit"
              loading={submitting}
              disabled={conflict}
              title={conflict ? '请先处理版本冲突' : undefined}
            >
              保存修改
            </Button>
            <Link className={styles.cancel} href={'/threads/' + post.threadId + '#p' + post.id}>
              取消
            </Link>
            {post.capabilities.canDelete ? (
              <span className={styles.deleteWrap}>
                <DeletePostButton
                  postId={post.id}
                  floor={post.floor}
                  threadId={post.threadId}
                  forumId={forumId}
                  size="small"
                />
              </span>
            ) : null}
          </div>
        </div>
      </div>
      <aside className={styles.side}>
        <section className={['panel', styles.sideCard].join(' ')}>
          <h2 className={styles.sideTitle}>编辑提示</h2>
          <ul className={styles.tips}>
            <li>版本冲突时本地内容不会丢失；请对比后再确认合并或覆盖。</li>
            <li>确认合并后才采用服务端最新版本提交，不会静默覆盖他人更新。</li>
            <li>编辑会记录历史版本，包含链接或审核开启时可能进入待审。</li>
          </ul>
        </section>
      </aside>
    </form>
  );
}
