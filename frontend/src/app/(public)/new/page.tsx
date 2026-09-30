import type { Metadata } from 'next';
import { redirect } from 'next/navigation';
import { safeGet, serverGet } from '@/lib/api/server';
import { getSession } from '@/lib/auth/server';
import { getSite } from '@/lib/site.server';
import { getSmileyMap } from '@/lib/markdown/smiley.server';
import { getEngagementRules, getPointsAccount } from '@/lib/data.server';
import type { CategoryWithForums, TagView } from '@/lib/api/types';
import { Breadcrumb } from '@/components/ui/Breadcrumb';
import { NewThreadForm } from '@/components/content/NewThreadForm';
import styles from './new.module.css';

export const metadata: Metadata = {
  title: '发布主题',
  robots: { index: false, follow: false },
};

export default async function NewThreadPage({ searchParams }: { searchParams: { forumId?: string } }) {
  const session = await getSession();
  const target = '/new' + (searchParams.forumId ? '?forumId=' + searchParams.forumId : '');
  if (!session.user) {
    redirect('/login?next=' + encodeURIComponent(target));
  }
  const [site, categories, tagPage, smileys, rules, account] = await Promise.all([
    getSite(),
    serverGet<CategoryWithForums[]>('/api/v1/forums'),
    safeGet<TagView[]>('/api/v1/tags', { query: { page: 1 } }),
    getSmileyMap(),
    getEngagementRules(),
    getPointsAccount(),
  ]);
  const forums = categories
    .flatMap((category) => category.forums)
    .filter((forum) => forum.capabilities?.canCreateThread);

  return (
    <div className="container page">
      <Breadcrumb items={[{ label: '首页', href: '/' }, { label: '发布主题' }]} />
      <h1 className={styles.title}>发布主题</h1>
      <p className={styles.subtitle}>选择版块并填写标题与正文，发布前可切换预览检查排版。</p>
      <NewThreadForm
        forums={forums}
        tags={tagPage ?? []}
        smileys={smileys}
        initialForumId={searchParams.forumId}
        uploadEnabled={Boolean(site?.uploadEnabled)}
        rules={rules}
        account={account}
      />
    </div>
  );
}
