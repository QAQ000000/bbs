import type { Metadata } from 'next';
import Link from 'next/link';
import { getSite } from '@/lib/site.server';
import { safeGet } from '@/lib/api/server';
import type { HomeView } from '@/lib/api/types';
import { formatCount } from '@/lib/format';
import { Breadcrumb } from '@/components/ui/Breadcrumb';
import styles from './about.module.css';

export const metadata: Metadata = {
  title: '关于我们',
  description: 'GoBBS 社区简介与站点信息。',
  alternates: { canonical: '/about' },
};

export default async function AboutPage() {
  const [site, home] = await Promise.all([getSite(), safeGet<HomeView>('/api/v1/home')]);
  const stats = home?.stats;
  return (
    <div className="container page">
      <Breadcrumb items={[{ label: '首页', href: '/' }, { label: '关于我们' }]} />
      <article className={['panel', styles.card].join(' ')}>
        <h1 className={styles.title}>{site?.name ?? 'GoBBS 社区'}</h1>
        <p className={styles.desc}>
          {site?.name ?? 'GoBBS'} 是一个面向开发者的技术社区，采用 Go 提供 API、Next.js 负责页面渲染。
          我们鼓励分享真实的踩坑经验与可复现的最佳实践。
        </p>
        {stats ? (
          <ul className={styles.stats}>
            <li>
              <strong>{formatCount(stats.totalThreads)}</strong> 主题
            </li>
            <li>
              <strong>{formatCount(stats.totalPosts)}</strong> 回复
            </li>
            <li>
              <strong>{formatCount(stats.members)}</strong> 会员
            </li>
          </ul>
        ) : null}
        <p className={styles.links}>
          <Link href="/terms">服务条款</Link>
          <Link href="/privacy">隐私政策</Link>
          <Link href="/help">帮助中心</Link>
          <Link href="/forums">浏览版块</Link>
        </p>
      </article>
    </div>
  );
}
