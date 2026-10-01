import type { Metadata } from 'next';
import Link from 'next/link';
import { safeGet } from '@/lib/api/server';
import { formatRelative } from '@/lib/format';
import { EmptyState } from '@/components/ui/StateView';
import { DraftActions } from '@/components/forum/DraftActions';
import { requireMember } from '../../guard';
import styles from '../member-page.module.css';
import list from './drafts.module.css';

export const metadata: Metadata = {
  title: '草稿箱',
  robots: { index: false, follow: false },
};

interface DraftRow {
  context: string;
  subject: string;
  content: string;
  updatedAt: string;
}

function continueHref(context: string): string {
  const [kind, id] = context.split(':');
  if (kind === 'new') return '/new?forumId=' + id;
  if (kind === 'reply') return '/threads/' + id + '#reply';
  return '/';
}

export default async function DraftsPage() {
  await requireMember('/me/drafts');
  const drafts = (await safeGet<DraftRow[]>('/api/v1/me/drafts')) ?? [];

  return (
    <div>
      <h1 className={styles.title}>草稿箱</h1>
      <p className={styles.subtitle}>草稿按上下文保存；服务端草稿与本地内容不一致时以服务端返回为准。</p>
      {drafts.length > 0 ? (
        <div className={styles.panel}>
          <ul className={list.items}>
            {drafts.map((draft) => (
              <li key={draft.context} className={list.item}>
                <div className={list.body}>
                  <p className={list.title}>{draft.subject || (draft.context.startsWith('reply') ? '回复草稿' : '未命名草稿')}</p>
                  <p className={list.excerpt}>{draft.content.slice(0, 160) || '（空内容）'}</p>
                  <span className={list.meta}>
                    {draft.context} · 更新于 {formatRelative(draft.updatedAt)}
                  </span>
                </div>
                <div className={list.actions}>
                  <Link className={list.continue} href={continueHref(draft.context)}>
                    继续编辑
                  </Link>
                  <DraftActions context={draft.context} />
                </div>
              </li>
            ))}
          </ul>
        </div>
      ) : (
        <div className={styles.panel}>
          <EmptyState title="草稿箱是空的" description="编辑主题或回复时内容会自动保存到草稿箱。" />
        </div>
      )}
    </div>
  );
}
