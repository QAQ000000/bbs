import type { Metadata } from 'next';
import { getSite } from '@/lib/site.server';
import Markdown from '@/lib/markdown/Markdown';
import { Breadcrumb } from '@/components/ui/Breadcrumb';

export const metadata: Metadata = {
  title: '服务条款',
  description: 'GoBBS 社区服务条款与使用规则。',
  alternates: { canonical: '/terms' },
};

export default async function TermsPage() {
  const site = await getSite();
  return (
    <div className="container page">
      <Breadcrumb items={[{ label: '首页', href: '/' }, { label: '服务条款' }]} />
      <article className="panel" style={{ padding: 24 }}>
        <Markdown content={site?.termsContent || '站点尚未配置服务条款内容。'} />
      </article>
    </div>
  );
}
