import type { Metadata } from 'next';
import Link from 'next/link';
import { adminGet } from '@/lib/admin.server';
import { serverGet } from '@/lib/api/server';
import type { CategoryWithForums, MembershipConfig } from '@/lib/api/types';
import { MembershipAdmin } from '@/components/admin/MembershipAdmin';
import { MemberLookup } from '@/components/admin/MemberLookup';
import styles from './membership-admin.module.css';

export const metadata: Metadata = {
  title: '会员等级',
  robots: { index: false, follow: false },
};

export default async function AdminMembershipPage() {
  const [config, categories] = await Promise.all([
    adminGet<MembershipConfig>('/api/v1/admin/membership'),
    serverGet<CategoryWithForums[]>('/api/v1/forums').catch(() => [] as CategoryWithForums[]),
  ]);

  if (!config.ok) {
    return (
      <div className={styles.forbidden}>
        <h1>无访问权限</h1>
        <p>会员等级需要 member.view；发布配置与人工调整还需要 member.configure / member.adjust / experience.adjust。</p>
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
        <h1 className={styles.pageTitle}>会员等级</h1>
        <span className={styles.pageMeta}>
          经验来自成长规则，等级由后端按门槛计算；积分是独立账本。配置需先预览拿到令牌再发布。
        </span>
      </div>
      <MembershipAdmin initial={config.data} forums={forums} />
      <div className={styles.spacer} />
      <MemberLookup levels={(config.data.levels ?? []).map((level) => ({ id: level.id, name: level.name, rank: level.rank }))} />
    </div>
  );
}
