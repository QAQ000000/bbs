import type { Metadata } from 'next';
import Link from 'next/link';
import { adminGetState, adminList } from '@/lib/admin.server';
import type { AdminDiagnosticsView, MemberLogRow } from '@/lib/api/types';
import { DiagnosticsPanel } from '@/components/admin/DiagnosticsPanel';
import styles from './diagnostics-admin.module.css';

export const metadata: Metadata = {
  title: '系统与会员诊断',
  robots: { index: false, follow: false },
};

export default async function AdminDiagnosticsPage() {
  const [diagnostics, logs] = await Promise.all([
    adminGetState<AdminDiagnosticsView>('/api/v1/admin/diagnostics'),
    adminList<MemberLogRow[]>('/api/v1/admin/membership/logs', { page: 1 }),
  ]);

  if (diagnostics.status === 'forbidden' && !logs.ok) {
    return (
      <div className={styles.forbidden}>
        <h1>无访问权限</h1>
        <p>系统诊断需要管理员权限；会员审计需要 membership.logs。</p>
        <Link href="/admin">返回概览</Link>
      </div>
    );
  }

  return (
    <div>
      <div className={styles.pageHead}>
        <h1 className={styles.pageTitle}>系统与会员诊断</h1>
        <span className={styles.pageMeta}>
          以只读诊断为主：区分接口读取失败与后端明确报告的异常，不提供改数据库或执行命令的入口。
        </span>
      </div>
      <DiagnosticsPanel
        initial={diagnostics.status === 'ok' ? diagnostics.data : null}
        initialError={diagnostics.status === 'error'}
        logs={logs.ok ? (logs.data.data ?? []) : []}
      />
      {diagnostics.status === 'forbidden' || !logs.ok ? (
        <p className={styles.hint}>
          会员审计日志读取受限（需要 membership.logs）；其余诊断内容不受影响。
        </p>
      ) : null}
    </div>
  );
}
