import type { Metadata } from 'next';
import { serverGet } from '@/lib/api/server';
import type { CategoryWithForums } from '@/lib/api/types';
import { ForumDirectory } from '@/components/forum/ForumDirectory';
import { Breadcrumb } from '@/components/ui/Breadcrumb';
import styles from './forums.module.css';

export const metadata: Metadata = {
  title: '版块目录',
  description: 'GoBBS 社区全部版块：按分类浏览技术交流、经验分享与休闲讨论区。',
  alternates: { canonical: '/forums' },
};

export default async function ForumsPage() {
  const categories = await serverGet<CategoryWithForums[]>('/api/v1/forums');
  return (
    <div className={styles.page}>
      <Breadcrumb items={[{ label: '首页', href: '/' }, { label: '版块目录' }]} />
      <ForumDirectory categories={categories} />
    </div>
  );
}
