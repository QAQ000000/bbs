import type { Metadata } from 'next';
import { notFound } from 'next/navigation';
import { getTag } from '@/lib/data.server';
import { safeRequest } from '@/lib/api/server';
import { getSession } from '@/lib/auth/server';
import type { ThreadListData } from '@/lib/api/types';
import { formatCount } from '@/lib/format';
import { Breadcrumb } from '@/components/ui/Breadcrumb';
import { ThreadList } from '@/components/forum/ThreadList';
import { Pagination } from '@/components/ui/Pagination';
import { ErrorState } from '@/components/ui/StateView';
import { SubscribeButton } from '@/components/forum/SubscribeButton';
import styles from './tag.module.css';

function parsePage(value?: string): number {
  const n = Number.parseInt(value ?? '1', 10);
  return Number.isFinite(n) && n > 0 ? n : 1;
}

export async function generateMetadata({ params }: { params: { slug: string } }): Promise<Metadata> {
  const tag = await getTag(params.slug);
  if (!tag) return { title: '标签不存在' };
  return {
    title: tag.name,
    description: tag.description || tag.name + ' 标签下的主题',
    alternates: { canonical: '/tags/' + (tag.slug || tag.id) },
  };
}

export default async function TagDetailPage({
  params,
  searchParams,
}: {
  params: { slug: string };
  searchParams: { page?: string };
}) {
  const tag = await getTag(params.slug);
  if (!tag) notFound();
  const page = parsePage(searchParams.page);
  const [listPage, session] = await Promise.all([
    safeRequest<ThreadListData>('/api/v1/tags/' + tag.id + '/threads', { query: { page } }),
    getSession(),
  ]);
  const data = listPage?.data;
  const meta = listPage?.meta;

  return (
    <div className="container page">
      <Breadcrumb
        items={[{ label: '首页', href: '/' }, { label: '标签目录', href: '/tags' }, { label: tag.name }]}
      />
      <section className={['panel', styles.header].join(' ')}>
        <div>
          <h1 className={styles.name} style={tag.color ? { color: tag.color } : undefined}>
            {tag.name}
          </h1>
          <p className={styles.desc}>{tag.description || '该标签暂无说明'}</p>
          <p className={styles.meta}>
            {formatCount(tag.threadCount)} 个公开主题
            {tag.status !== 'active' ? ' · 该标签已禁用，仅可查看' : ''}
          </p>
        </div>
        {session.user && tag.status === 'active' ? (
          <SubscribeButton kind="tag" id={tag.id} initialSubscribed={tag.subscribed ?? null} />
        ) : null}
      </section>

      {data ? (
        <ThreadList
          threads={data.threads}
          stickies={data.stickies}
          emptyTitle="该标签下暂无主题"
          emptyDescription="换个标签浏览，或发布一个带此标签的主题。"
        />
      ) : (
        <div className="panel">
          <ErrorState title="主题加载失败" description="无法读取该标签的主题，请稍后重试。" retryHref={'/tags/' + (tag.slug || tag.id)} />
        </div>
      )}

      {meta ? (
        <div className={styles.pagination}>
          <Pagination
            page={meta.page}
            totalPages={meta.totalPages}
            basePath={'/tags/' + (tag.slug || tag.id)}
            ariaLabel="标签主题分页"
          />
        </div>
      ) : null}
    </div>
  );
}
