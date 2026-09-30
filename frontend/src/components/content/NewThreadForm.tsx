'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { Button, Input, InputNumber, Select } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { EngagementRules, ForumSummary, PointsAccount, TagView } from '@/lib/api/types';
import type { SmileyMap } from '@/lib/markdown/smiley';
import { MarkdownEditor } from './MarkdownEditor';
import { useServerDraft } from './useDraft';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './NewThreadForm.module.css';

export interface NewThreadFormProps {
  forums: ForumSummary[];
  tags: TagView[];
  smileys: SmileyMap;
  initialForumId?: string;
  uploadEnabled: boolean;
  /** 公开互动规则快照；用于展示输入上限，写操作仍以后端校验为准。 */
  rules: EngagementRules | null;
  /** 当前用户积分账户；用于悬赏可用余额提示。 */
  account: PointsAccount | null;
}

export function NewThreadForm({
  forums,
  tags,
  smileys,
  initialForumId,
  uploadEnabled,
  rules,
  account,
}: NewThreadFormProps) {
  const router = useRouter();
  const defaultForum = initialForumId && forums.some((f) => f.id === initialForumId) ? initialForumId : forums[0]?.id ?? '';
  const [forumId, setForumId] = useState(defaultForum);
  const [subject, setSubject] = useState('');
  const [content, setContent] = useState('');
  const [tagIds, setTagIds] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  // 主题与附加活动分两次请求；记录已创建的主题以便失败时给出重试入口。
  const [createdThread, setCreatedThread] = useState<{ id: string; postId: string } | null>(null);
  const [attachPoll, setAttachPoll] = useState(false);
  const [pollQuestion, setPollQuestion] = useState('');
  const [pollOptions, setPollOptions] = useState<string[]>(['', '']);
  const [pollMaxChoices, setPollMaxChoices] = useState(1);
  const [pollDuration, setPollDuration] = useState(24);
  const [attachBounty, setAttachBounty] = useState(false);
  const [bountyAmount, setBountyAmount] = useState(100);
  const [bountyDuration, setBountyDuration] = useState(72);
  const { restored, save, clear } = useServerDraft('new:' + (forumId || '0'), Boolean(forumId));

  useEffect(() => {
    if (!restored) return;
    setContent((prev) => prev || restored.content);
    setSubject((prev) => prev || restored.subject);
  }, [restored]);

  useEffect(() => {
    if (content || subject) save(content, subject);
  }, [content, subject, save]);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    setNotice(null);
    const title = subject.trim();
    const body = content.trim();
    if (!forumId) {
      setError('请选择版块');
      return;
    }
    if (!title) {
      setError('请输入标题');
      return;
    }
    if (title.length > 80) {
      setError('标题不能超过 80 个字符');
      return;
    }
    if (!body) {
      setError('内容不能为空');
      return;
    }
    if (attachPoll) {
      if (!pollQuestion.trim()) {
        setError('请输入投票问题');
        return;
      }
      if (pollOptions.map((option) => option.trim()).filter(Boolean).length < 2) {
        setError('投票至少需要 2 个选项');
        return;
      }
    }
    if (attachBounty) {
      const min = rules?.bounty.minPoints ?? 1;
      const max = rules?.bounty.maxPoints ?? 10000;
      if (bountyAmount < min || bountyAmount > max) {
        setError('悬赏积分需在 ' + min + ' 到 ' + max + ' 之间');
        return;
      }
      if (account && bountyAmount > account.available) {
        setError('可用积分不足：当前可用 ' + account.available);
        return;
      }
    }
    setSubmitting(true);
    try {
      const result = await browserSend<{ threadId: string; postId: string; pending: boolean }>('/threads', {
        method: 'POST',
        body: { forumId, subject: title, content: body, tagIds },
      });
      clear();
      setContent('');
      setSubject('');
      // 主题已创建：附加活动失败不回滚主题，保留结果并给出主题页重试入口。
      const failures: string[] = [];
      if (attachPoll) {
        const cleaned = pollOptions.map((option) => option.trim()).filter(Boolean);
        try {
          await browserSend('/threads/' + result.threadId + '/poll', {
            method: 'POST',
            body: {
              question: pollQuestion.trim(),
              options: cleaned,
              maxChoices: pollMaxChoices,
              durationHours: pollDuration,
            },
          });
        } catch (caught) {
          failures.push('投票：' + (caught as ApiError).message);
        }
      }
      if (attachBounty) {
        try {
          await browserSend('/threads/' + result.threadId + '/bounty', {
            method: 'POST',
            body: { amount: bountyAmount, durationHours: bountyDuration },
          });
        } catch (caught) {
          failures.push('悬赏：' + (caught as ApiError).message);
        }
      }
      if (failures.length > 0) {
        setCreatedThread({ id: result.threadId, postId: result.postId });
        setNotice(
          '主题已' +
            (result.pending ? '提交待审' : '发布') +
            '，但附加活动创建失败：' +
            failures.join('；') +
            '。可前往主题页重试创建。',
        );
        toastError(new Error('主题已发布，附加活动未创建成功'));
        return;
      }
      if (result.pending) {
        setCreatedThread({ id: result.threadId, postId: result.postId });
        setNotice('主题已提交，等待审核通过后公开显示。');
        toastSuccess('已提交，等待审核');
        return;
      }
      toastSuccess('发布成功');
      router.push('/threads/' + result.threadId + '#p' + result.postId);
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      setError(err.message);
    } finally {
      setSubmitting(false);
    }
  }

  if (forums.length === 0) {
    return (
      <div className="panel" style={{ padding: 24 }}>
        <p style={{ margin: 0, color: 'var(--color-text-secondary)' }}>当前没有可发帖的版块。</p>
      </div>
    );
  }

  const selectedForum = forums.find((f) => f.id === forumId);

  return (
    <form className={styles.grid} onSubmit={submit}>
      <div className={styles.main}>
        <div className={['panel', styles.card].join(' ')}>
          <div className={styles.field}>
            <label className={styles.label} htmlFor="forum">
              所属版块
            </label>
            <Select id="forum" value={forumId} onChange={(value) => setForumId(value as string)} style={{ width: 260 }}>
              {forums.map((forum) => (
                <Select.Option key={forum.id} value={forum.id}>
                  {forum.name}
                </Select.Option>
              ))}
            </Select>
          </div>
          <div className={styles.field}>
            <label className={styles.label} htmlFor="subject">
              标题
            </label>
            <Input
              id="subject"
              value={subject}
              onChange={setSubject}
              maxLength={80}
              showWordLimit
              placeholder="一句话说明你的主题"
            />
          </div>
          {tags.length > 0 ? (
            <div className={styles.field}>
              <label className={styles.label} htmlFor="tags">
                标签
              </label>
              <Select
                id="tags"
                mode="multiple"
                value={tagIds}
                onChange={(value) => setTagIds((value as string[]).slice(0, 8))}
                placeholder="最多选择 8 个标签"
                style={{ width: '100%' }}
              >
                {tags.map((tag) => (
                  <Select.Option key={tag.id} value={tag.id}>
                    {tag.name}
                  </Select.Option>
                ))}
              </Select>
            </div>
          ) : null}
          <div className={styles.field}>
            <label className={styles.label} htmlFor="content">
              正文
            </label>
            <MarkdownEditor
              value={content}
              onChange={setContent}
              smileys={smileys}
              uploadEnabled={uploadEnabled}
              minRows={12}
              ariaLabel="主题正文"
              placeholder="支持 Markdown：标题、列表、代码块、引用、表格与图片…"
            />
          </div>
          {rules && (rules.poll.enabled || rules.bounty.enabled) ? (
            <div className={styles.attachBlock}>
              <div className={styles.attachRow}>
                {rules.poll.enabled ? (
                  <label className={styles.attachToggle}>
                    <input type="checkbox" checked={attachPoll} onChange={(event) => setAttachPoll(event.target.checked)} />
                    附加投票
                  </label>
                ) : null}
                {rules.bounty.enabled ? (
                  <label className={styles.attachToggle}>
                    <input type="checkbox" checked={attachBounty} onChange={(event) => setAttachBounty(event.target.checked)} />
                    附加悬赏
                  </label>
                ) : null}
              </div>
              {attachPoll ? (
                <div className={styles.attachPanel}>
                  <Input value={pollQuestion} onChange={setPollQuestion} maxLength={200} placeholder="投票问题" />
                  {pollOptions.map((value, index) => (
                    <Input
                      key={index}
                      value={value}
                      onChange={(v) => setPollOptions((prev) => prev.map((item, i) => (i === index ? v : item)))}
                      maxLength={200}
                      placeholder={'选项 ' + (index + 1)}
                    />
                  ))}
                  {pollOptions.length < rules.poll.maxOptions ? (
                    <Button size="small" type="text" onClick={() => setPollOptions((prev) => [...prev, ''])}>
                      + 添加选项
                    </Button>
                  ) : null}
                  <div className={styles.attachNumbers}>
                    <label className={styles.attachLabel}>
                      每人可选项
                      <InputNumber
                        min={1}
                        max={Math.max(2, pollOptions.filter((o) => o.trim()).length)}
                        value={pollMaxChoices}
                        onChange={(v) => setPollMaxChoices(Number(v) || 1)}
                      />
                    </label>
                    <label className={styles.attachLabel}>
                      有效小时数
                      <InputNumber
                        min={1}
                        max={rules.poll.maxDays * 24}
                        value={pollDuration}
                        onChange={(v) => setPollDuration(Number(v) || 24)}
                      />
                    </label>
                  </div>
                </div>
              ) : null}
              {attachBounty ? (
                <div className={styles.attachPanel}>
                  <div className={styles.attachNumbers}>
                    <label className={styles.attachLabel}>
                      悬赏积分
                      <InputNumber
                        min={rules.bounty.minPoints}
                        max={rules.bounty.maxPoints}
                        value={bountyAmount}
                        onChange={(v) => setBountyAmount(Number(v) || rules.bounty.minPoints)}
                      />
                    </label>
                    <label className={styles.attachLabel}>
                      有效小时数
                      <InputNumber
                        min={1}
                        max={rules.bounty.maxDays * 24}
                        value={bountyDuration}
                        onChange={(v) => setBountyDuration(Number(v) || 72)}
                      />
                    </label>
                  </div>
                  <p className={styles.attachHint}>
                    当前可用积分：{account ? account.available : '未知'}（发布后进入冻结，采纳时结算）。
                  </p>
                </div>
              ) : null}
              <p className={styles.attachHint}>
                主题与附加活动是两次请求：主题创建成功而附加失败时会保留主题，并提供主题页重试入口。
              </p>
            </div>
          ) : null}
          {notice ? (
            <p className={styles.notice}>
              {notice}
              {createdThread ? (
                <>
                  {' '}
                  <Link href={'/threads/' + createdThread.id}>前往主题</Link>
                </>
              ) : null}
            </p>
          ) : null}
          {error ? (
            <p className={styles.error} role="alert">
              {error}
            </p>
          ) : null}
          <div className={styles.actions}>
            <Button type="primary" htmlType="submit" loading={submitting} disabled={Boolean(createdThread)}>
              发布主题
            </Button>
            <Link className={styles.cancel} href="/">
              取消
            </Link>
          </div>
        </div>
      </div>
      <aside className={styles.side}>
        <section className={['panel', styles.sideCard].join(' ')}>
          <h2 className={styles.sideTitle}>发帖提示</h2>
          <ul className={styles.tips}>
            <li>标题避免营销化、堆砌关键词。</li>
            <li>正文使用 Markdown，代码请放进代码块。</li>
            <li>教程与分享建议附可复现步骤。</li>
            <li>提问前先搜索，避免重复发帖。</li>
          </ul>
        </section>
        {selectedForum ? (
          <section className={['panel', styles.sideCard].join(' ')}>
            <h2 className={styles.sideTitle}>{selectedForum.name}</h2>
            <p className={styles.sideDesc}>{selectedForum.description || '暂无版块说明'}</p>
            <Link className={styles.sideLink} href={'/forums/' + selectedForum.id}>
              查看本版主题
            </Link>
          </section>
        ) : null}
      </aside>
    </form>
  );
}
