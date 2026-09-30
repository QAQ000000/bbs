import type { Metadata } from 'next';
import Link from 'next/link';
import { adminList } from '@/lib/admin.server';
import type { AdminUserView } from '@/lib/api/types';
import { Pagination } from '@/components/ui/Pagination';
import { UserAdmin } from '@/components/admin/UserAdmin';
import styles from './users-admin.module.css';

export const metadata: Metadata = {
  title: '用户与封禁',
  robots: { index: false, follow: false },
};

function parsePage(value?: string): number {
  const n = Number.parseInt(value ?? '1', 10);
  return Number.isFinite(n) && n > 0 ? n : 1;
}

export default async function AdminUsersPage({
  searchParams,
}: {
  searchParams: { q?: string; page?: string };
}) {
  const q = (searchParams.q ?? '').trim();
  const page = parsePage(searchParams.page);
  const result = await adminList<AdminUserView[]>('/api/v1/admin/users', { q: q || undefined, page });

  if (!result.ok) {
    return (
      <div className={styles.forbidden}>
        <h1>无访问权限</h1>
        <p>用户管理需要管理员权限；封禁与用户组调整还受独立权限点约束，并需完成初始密码修改。</p>
        <Link href="/admin">返回概览</Link>
      </div>
    );
  }

  const users = result.data.data ?? [];
  return (
    <div>
      <div className={styles.pageHead}>
        <h1 className={styles.pageTitle}>用户与封禁</h1>
        <span className={styles.pageMeta}>
          邮箱仅在管理接口返回；危险操作会记录审计日志。删除账号不在本批范围。
        </span>
      </div>
      <UserAdmin users={users} query={q} />
      <div className={styles.pagination}>
        <Pagination
          page={result.data.meta?.page ?? 1}
          totalPages={result.data.meta?.totalPages ?? 0}
          basePath="/admin/users"
          query={{ q }}
          ariaLabel="用户分页"
        />
      </div>
    </div>
  );
}
