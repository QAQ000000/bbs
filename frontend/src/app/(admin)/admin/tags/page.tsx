import type { Metadata } from 'next';
import Link from 'next/link';
import { adminList } from '@/lib/admin.server';
import type { TagView } from '@/lib/api/types';
import { Pagination } from '@/components/ui/Pagination';
import { TagAdmin } from '@/components/admin/TagAdmin';
import styles from './tags-admin.module.css';

export const metadata: Metadata = {
  title: '标签管理',
  robots: { index: false, follow: false },
};

function parsePage(value?: string): number {
  const n = Number.parseInt(value ?? '1', 10);
  return Number.isFinite(n) && n > 0 ? n : 1;
}

export default async function AdminTagsPage({
  searchParams,
}: {
  searchParams: { q?: string; page?: string };
}) {
  const q = (searchParams.q ?? '').trim();
  const page = parsePage(searchParams.page);
  const result = await adminList<TagView[]>('/api/v1/admin/tags', { q: q || undefined, page });

  if (!result.ok) {
    return (
      <div className={styles.forbidden}>
        <h1>无访问权限</h1>
        <p>标签管理需要管理员权限与 tags.configure，并完成初始密码修改。</p>
        <Link href="/admin">返回概览</Link>
      </div>
    );
  }

  const tags = result.data.data ?? [];
  return (
    <div>
      <div className={styles.pageHead}>
        <h1 className={styles.pageTitle}>标签管理</h1>
        <span className={styles.pageMeta}>保存需携带版本；冲突时先重新读取，别名与绑定主题数只在接口提供时展示。</span>
      </div>
      <TagAdmin tags={tags} query={q} />
      <div className={styles.pagination}>
        <Pagination
          page={result.data.meta?.page ?? 1}
          totalPages={result.data.meta?.totalPages ?? 0}
          basePath="/admin/tags"
          query={{ q }}
          ariaLabel="标签分页"
        />
      </div>
    </div>
  );
}
