import type { Metadata } from 'next';
import Link from 'next/link';
import { adminGet } from '@/lib/admin.server';
import { getSession } from '@/lib/auth/server';
import type { AdminPermsView } from '@/lib/api/types';
import { PermissionsAdmin } from '@/components/admin/PermissionsAdmin';
import { AdminSessionsPanel } from '@/components/admin/AdminSessionsPanel';
import styles from './permissions-admin.module.css';

export const metadata: Metadata = {
  title: '权限与设备会话',
  robots: { index: false, follow: false },
};

export default async function AdminPermissionsPage() {
  const [perms, session] = await Promise.all([
    adminGet<AdminPermsView>('/api/v1/admin/perms'),
    getSession(),
  ]);

  if (!perms.ok) {
    return (
      <div className={styles.forbidden}>
        <h1>无访问权限</h1>
        <p>权限矩阵需要 permissions.edit；设备会话需要 sessions.manage。管理员对这两项有硬保护。</p>
        <Link href="/admin">返回概览</Link>
      </div>
    );
  }

  return (
    <div>
      <div className={styles.pageHead}>
        <h1 className={styles.pageTitle}>权限与设备会话</h1>
        <span className={styles.pageMeta}>
          权限点、用户组与会员等级权限是三套独立规则；页面只发后端已有的操作，不绕过任何限制。
        </span>
      </div>
      <PermissionsAdmin points={perms.data.points ?? []} matrix={perms.data.matrix ?? {}} />
      <div className={styles.spacer} />
      <AdminSessionsPanel currentUserId={session.user?.id ?? ''} />
    </div>
  );
}
