import type { Metadata } from 'next';
import Link from 'next/link';
import { adminGet } from '@/lib/admin.server';
import { serverGet } from '@/lib/api/server';
import type { CategoryWithForums, TitleDefinition } from '@/lib/api/types';
import { TitleAdmin } from '@/components/admin/TitleAdmin';
import { TitleGrants } from '@/components/admin/TitleGrants';
import styles from './titles-admin.module.css';

export const metadata: Metadata = {
  title: '称号与条件',
  robots: { index: false, follow: false },
};

export default async function AdminTitlesPage() {
  const [result, categories] = await Promise.all([
    adminGet<{ titles: TitleDefinition[]; metrics: string[] }>('/api/v1/admin/titles'),
    serverGet<CategoryWithForums[]>('/api/v1/forums').catch(() => [] as CategoryWithForums[]),
  ]);

  if (!result.ok) {
    return (
      <div className={styles.forbidden}>
        <h1>无访问权限</h1>
        <p>称号管理需要 title.view；发布条件、补发与授予 / 撤销还需要 title.configure / title.grant / title.revoke。</p>
        <Link href="/admin">返回概览</Link>
      </div>
    );
  }

  const forums = (categories ?? []).flatMap((category) =>
    (category.forums ?? []).map((forum) => ({ id: forum.id, name: forum.name })),
  );

  return (
    <div>
      <div className={styles.pageHead}>
        <h1 className={styles.pageTitle}>称号与条件</h1>
        <span className={styles.pageMeta}>
          条件事件来自后端固定指标；发布自动称号会入队补发，达成结果由后端计算，前端不复制算法。
        </span>
      </div>
      <TitleAdmin titles={result.data.titles ?? []} metrics={result.data.metrics ?? []} forums={forums} />
      <div className={styles.spacer} />
      <TitleGrants />
    </div>
  );
}
