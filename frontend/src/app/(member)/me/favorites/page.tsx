import type { Metadata } from 'next';
import { safeGet, safeRequest } from '@/lib/api/server';
import type { CategoryWithForums, FavoriteView } from '@/lib/api/types';
import { forumNameMap } from '@/lib/thread';
import { ThreadCard } from '@/components/forum/ThreadCard';
import { Pagination } from '@/components/ui/Pagination';
import { EmptyState, ErrorState } from '@/components/ui/StateView';
import { requireMember } from '../../guard';
import styles from '../member-page.module.css';

export const metadata: Metadata = {
  title: '我的收藏',
  robots: { index: false, follow: false },
};

function parsePage(value?: string): number {
  const n = Number.parseInt(value ?? '1', 10);
  return Number.isFinite(n) && n > 0 ? n : 1;
}

export default async function FavoritesPage({ searchParams }: { searchParams: { page?: string } }) {
  await requireMember('/me/favorites');
  const page = parsePage(searchParams.page);
  const [envelope, categories] = await Promise.all([
    safeRequest<FavoriteView[]>('/api/v1/me/favorites', { query: { page } }),
    safeGet<CategoryWithForums[]>('/api/v1/forums'),
  ]);
  const items = envelope?.data ?? [];
  const names = forumNameMap(categories ?? []);

  return (
    <div>
      <h1 className={styles.title}>我的收藏</h1>
      <p className={styles.subtitle}>收藏按主题展示；收藏是切换操作，超时后请重新读取实际状态。</p>
      {envelope ? (
        items.length > 0 ? (
          <div>
            {items.map((item) => (
              <ThreadCard key={item.id} thread={item} forumName={names[item.forumId]} />
            ))}
            <div className={styles.pagination}>
              <Pagination
                page={envelope.meta?.page ?? 1}
                totalPages={envelope.meta?.totalPages ?? 0}
                basePath="/me/favorites"
                ariaLabel="收藏分页"
              />
            </div>
          </div>
        ) : (
          <div className="panel">
            <EmptyState title="还没有收藏" description="在主题页点击「收藏」，方便之后快速找到。" />
          </div>
        )
      ) : (
        <div className="panel">
          <ErrorState title="收藏加载失败" description="无法读取收藏列表，请稍后重试。" retryHref="/me/favorites" />
        </div>
      )}
    </div>
  );
}
