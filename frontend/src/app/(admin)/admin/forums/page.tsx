import type { Metadata } from 'next';
import Link from 'next/link';
import { adminGet } from '@/lib/admin.server';
import type { CategoryWithForums } from '@/lib/api/types';
import { ForumAdmin } from '@/components/admin/ForumAdmin';
import styles from './forums-admin.module.css';

export const metadata: Metadata = {
  title: '分类与版块',
  robots: { index: false, follow: false },
};

export default async function AdminForumsPage() {
  const result = await adminGet<CategoryWithForums[]>('/api/v1/admin/forums');

  if (!result.ok) {
    return (
      <div className={styles.forbidden}>
        <h1>无访问权限</h1>
        <p>分类与版块维护需要管理员权限，并完成初始密码修改。</p>
        <Link href="/admin">返回概览</Link>
      </div>
    );
  }

  return (
    <div>
      <div className={styles.pageHead}>
        <h1 className={styles.pageTitle}>分类与版块</h1>
        <span className={styles.pageMeta}>删除与排序调整会记录审计日志，危险操作需二次确认。</span>
      </div>
      <ForumAdmin categories={result.data} />
    </div>
  );
}
