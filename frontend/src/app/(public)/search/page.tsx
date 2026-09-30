import type { Metadata } from 'next';
import Link from 'next/link';
import { safeRequest } from '@/lib/api/server';
import type { SearchHit } from '@/lib/api/types';
import { formatRelative } from '@/lib/format';
import { Pagination } from '@/components/ui/Pagination';
import { EmptyState, ErrorState } from '@/components/ui/StateView';
import styles from './search.module.css';

export const metadata: Metadata = {
  title: '搜索',
  description: '搜索 GoBBS 社区主题。',
  robots: { index: false, follow: true },
};

function parsePage(value?: string): number {
  const n = Number.parseInt(value ?? '1', 10);
  return Number.isFinite(n) && n > 0 ? n : 1;
}

export default async function SearchPage({
  searchParams,
}: {
  searchParams: { q?: string; page?: string; forumId?: string };
}) {
  const q = (searchParams.q ?? '').trim();
  const page = parsePage(searchParams.page);
  const envelope = q
    ? await safeRequest<SearchHit[]>('/api/v1/search', { query: { q, page, forumId: searchParams.forumId } })
    : null;
  const hits = envelope?.data ?? [];

  return (
    <div className="container page">
      <h1 className={styles.title}>搜索</h1>
      <form className={styles.search} action="/search" method="get" role="search">
        <input className={styles.input} type="search" name="q" defaultValue={q} placeholder="搜索主题标题与正文" aria-label="搜索主题" />
        <button className={styles.button} type="submit">
          搜索
        </button>
      </form>

      {!q ? (
        <div className="panel">
          <EmptyState title="输入关键词开始搜索" description="支持按主题标题与正文匹配；结果默认不被搜索引擎索引。" />
        </div>
      ) : envelope ? (
        hits.length > 0 ? (
          <>
            <p className={styles.summary}>关键词「{q}」共 {envelope.meta?.total ?? hits.length} 条结果</p>
            <ul className={styles.list}>
              {hits.map((hit) => (
                <li key={hit.threadId} className={['panel', styles.item].join(' ')}>
                  <Link className={styles.itemTitle} href={'/threads/' + hit.threadId}>
                    {hit.title}
                  </Link>
                  <p className={styles.excerpt}>{hit.excerpt}</p>
                  <span className={styles.meta}>
                    {hit.authorName} · {hit.forumName} · {formatRelative(hit.createdAt)}
                  </span>
                </li>
              ))}
            </ul>
            <div className={styles.pagination}>
              <Pagination
                page={envelope.meta?.page ?? 1}
                totalPages={envelope.meta?.totalPages ?? 0}
                basePath="/search"
                query={{ q, forumId: searchParams.forumId }}
                ariaLabel="搜索分页"
              />
            </div>
          </>
        ) : (
          <div className="panel">
            <EmptyState title="没有找到相关主题" description="换个关键词，或先浏览版块。" action={<Link className={styles.link} href="/forums">浏览版块</Link>} />
          </div>
        )
      ) : (
        <div className="panel">
          <ErrorState title="搜索失败" description="搜索服务暂时不可用，请稍后重试。" retryHref={'/search?q=' + encodeURIComponent(q)} />
        </div>
      )}
    </div>
  );
}
