import type { Metadata } from 'next';
import Link from 'next/link';
import { adminGet } from '@/lib/admin.server';
import type { EngagementRules, PointsConfig } from '@/lib/api/types';
import { EngagementConfigAdmin } from '@/components/admin/EngagementConfigAdmin';
import styles from './engagement-admin.module.css';

export const metadata: Metadata = {
  title: '互动配置',
  robots: { index: false, follow: false },
};

interface EngagementView {
  version: number;
  poll: { enabled: boolean; maxOptions: number; maxDays: number };
  bounty: { enabled: boolean; minPoints: number; maxPoints: number; maxDays: number };
  checkin: { enabled: boolean; experience: number; points: number; timeZone: string };
}

export default async function AdminEngagementPage() {
  const [points, engagement, rules] = await Promise.all([
    adminGet<PointsConfig>('/api/v1/admin/points/config'),
    adminGet<EngagementView>('/api/v1/admin/engagement/config'),
    adminGet<EngagementRules>('/api/v1/admin/engagement/rules').catch(() => ({ ok: false as const, forbidden: true as const })),
  ]);

  if (!points.ok || !engagement.ok) {
    return (
      <div className={styles.forbidden}>
        <h1>无访问权限</h1>
        <p>读取需要 points.view / engagement.view；保存需要 points.configure / engagement.configure。</p>
        <Link href="/admin">返回概览</Link>
      </div>
    );
  }

  return (
    <div>
      <div className={styles.pageHead}>
        <h1 className={styles.pageTitle}>互动配置</h1>
        <span className={styles.pageMeta}>
          配置均为整对象提交并携带版本；冲突时保留输入。字段范围以后端契约为准。
        </span>
      </div>
      <EngagementConfigAdmin
        points={points.data}
        engagement={engagement.data}
        rules={rules.ok ? rules.data : null}
      />
    </div>
  );
}
