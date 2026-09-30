import type { Metadata } from 'next';
import { getSite } from '@/lib/site.server';
import Markdown from '@/lib/markdown/Markdown';
import { Breadcrumb } from '@/components/ui/Breadcrumb';

export const metadata: Metadata = {
  title: '隐私政策',
  description: 'GoBBS 社区隐私政策与信息处理说明。',
  alternates: { canonical: '/privacy' },
};

export default async function PrivacyPage() {
  const site = await getSite();
  return (
    <div className="container page">
      <Breadcrumb items={[{ label: '首页', href: '/' }, { label: '隐私政策' }]} />
      <article className="panel" style={{ padding: 24 }}>
        <Markdown content={site?.privacyContent || '站点尚未配置隐私政策内容。'} />
      </article>
    </div>
  );
}
