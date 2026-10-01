import { tagPath } from '@/lib/tag';
import type { Metadata } from 'next';
import Link from 'next/link';
import { safeRequest } from '@/lib/api/server';
import type { TagView } from '@/lib/api/types';
import { formatCount } from '@/lib/format';
import { Breadcrumb } from '@/components/ui/Breadcrumb';
import { Pagination } from '@/components/ui/Pagination';
import { EmptyState, ErrorState } from '@/components/ui/StateView';
import styles from './tags.module.css';

export const metadata: Metadata = {
  title: '标签目录',
  description: '按标签浏览 GoBBS 社区主题。',
  alternates: { canonical: '/tags' },
};

function parsePage(value?: string): number {
  const n = Number.parseInt(value ?? '1', 10);
  return Number.isFinite(n) && n > 0 ? n : 1;
}

export default async function TagsPage({
  searchParams,
}: {
  searchParams: { page?: string; q?: string };
}) {
  const page = parsePage(searchParams.page);
  const q = (searchParams.q ?? '').trim();
  const envelope = await safeRequest<TagView[]>('/api/v1/tags', { query: { page, q: q || undefined } });
  const tags = envelope?.data ?? [];

  return (
    <div className={styles.page}>
      <Breadcrumb items={[{ label: '首页', href: '/' }, { label: '标签目录' }]} />
      <header className={styles.header}>
      <div><h1 className={styles.title}>标签目录</h1>
      <p className={styles.subtitle}>浏览社区标签，找到感兴趣的话题。</p></div>
      <form className={styles.search} action="/tags" method="get" role="search">
        <input className={styles.input} type="search" name="q" defaultValue={q} placeholder="搜索标签" aria-label="搜索标签" />
        <button className={styles.button} type="submit">
          搜索
        </button>
      </form>
      </header>
      {envelope ? (
        tags.length > 0 ? (
          <>
            <div className={['panel', styles.grid].join(' ')}>
              {tags.map((tag) => (
                <Link key={tag.id} href={tagPath(tag)} className={styles.tag}>
                  <i className={styles.dot} style={tag.color ? { backgroundColor: tag.color } : undefined} aria-hidden="true" />
                  <span className={styles.tagName}>
                    {tag.name}
                  </span>
                  <span className={styles.tagCount}>{formatCount(tag.threadCount)} 主题</span>
                </Link>
              ))}
            </div>
            <div className={styles.pagination}>
              <Pagination
                page={envelope.meta?.page ?? 1}
                totalPages={envelope.meta?.totalPages ?? 0}
                basePath="/tags"
                query={{ q }}
                ariaLabel="标签分页"
              />
            </div>
          </>
        ) : (
          <div className="panel">
            <EmptyState title="没有匹配的标签" description={q ? '换个关键词试试。' : '站点还没有配置标签。'} />
          </div>
        )
      ) : (
        <div className="panel">
          <ErrorState title="标签加载失败" description="无法读取标签目录，请稍后重试。" retryHref="/tags" />
        </div>
      )}
    </div>
  );
}
