import Link from 'next/link';
import styles from './Pagination.module.css';

export interface PaginationProps {
  page: number;
  totalPages: number;
  basePath: string;
  query?: Record<string, string | number | undefined>;
  /** 无 JS 时也保留真实翻页链接。 */
  ariaLabel?: string;
}

function hrefFor(basePath: string, query: Record<string, string | number | undefined>, page: number): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === '' || key === 'page') continue;
    params.set(key, String(value));
  }
  if (page > 1) params.set('page', String(page));
  const suffix = params.toString();
  return suffix ? `${basePath}?${suffix}` : basePath;
}

function pageWindow(page: number, totalPages: number): number[] {
  const pages = new Set<number>([1, totalPages]);
  for (let i = page - 2; i <= page + 2; i += 1) {
    if (i >= 1 && i <= totalPages) pages.add(i);
  }
  return [...pages].sort((a, b) => a - b);
}

export function Pagination({ page, totalPages, basePath, query = {}, ariaLabel = '分页' }: PaginationProps) {
  if (totalPages <= 1) return null;
  const current = Math.min(Math.max(page, 1), totalPages);
  const pages = pageWindow(current, totalPages);
  let previous = 0;
  return (
    <nav className={styles.pagination} aria-label={ariaLabel}>
      {current > 1 ? (
        <Link className={styles.page} href={hrefFor(basePath, query, current - 1)} rel="prev">
          上一页
        </Link>
      ) : (
        <span className={[styles.page, styles.disabled].join(' ')} aria-disabled="true">
          上一页
        </span>
      )}
      {pages.map((p) => {
        const gap = previous && p - previous > 1;
        previous = p;
        return (
          <span key={p} className={styles.item}>
            {gap ? <span className={styles.ellipsis}>…</span> : null}
            {p === current ? (
              <span className={[styles.page, styles.active].join(' ')} aria-current="page">
                {p}
              </span>
            ) : (
              <Link className={styles.page} href={hrefFor(basePath, query, p)}>
                {p}
              </Link>
            )}
          </span>
        );
      })}
      {current < totalPages ? (
        <Link className={styles.page} href={hrefFor(basePath, query, current + 1)} rel="next">
          下一页
        </Link>
      ) : (
        <span className={[styles.page, styles.disabled].join(' ')} aria-disabled="true">
          下一页
        </span>
      )}
    </nav>
  );
}
