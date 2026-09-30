import { NextResponse } from 'next/server';
import { serverGet } from '@/lib/api/server';
import type { ThreadSummary } from '@/lib/api/types';
import { SITE_URL } from '@/lib/site-url';
import { XML_HEADER, escapeXml } from '@/lib/xml';

export const dynamic = 'force-dynamic';

const FEED_SIZE = 20;

function rfc822(value: string | null | undefined): string {
  if (!value) return '';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '' : date.toUTCString();
}

interface IndexPage {
  threads: Pick<ThreadSummary, 'id' | 'title' | 'authorName' | 'createdAt'>[];
}

export async function GET() {
  let threads: IndexPage['threads'];
  try {
    const page = await serverGet<IndexPage>('/api/v1/index/threads', {
      query: { sort: 'created', page: 1, size: FEED_SIZE },
      forwardCookies: false,
    });
    threads = page?.threads ?? [];
  } catch {
    // 读取失败必须区别于“站点还没有公开主题”：返回 503，不伪装成空 feed。
    return new NextResponse('上游主题索引读取失败，请稍后重试。', {
      status: 503,
      headers: { 'content-type': 'text/plain; charset=utf-8', 'cache-control': 'no-store' },
    });
  }

  const items = threads
    .map((thread) => {
      const link = SITE_URL + '/threads/' + thread.id;
      return [
        '    <item>',
        '      <title>' + escapeXml(thread.title) + '</title>',
        '      <link>' + escapeXml(link) + '</link>',
        '      <guid isPermaLink="true">' + escapeXml(link) + '</guid>',
        '      <pubDate>' + escapeXml(rfc822(thread.createdAt)) + '</pubDate>',
        '      <author>' + escapeXml(thread.authorName ?? '') + '</author>',
        '    </item>',
      ].join('\n');
    })
    .join('\n');

  const latest = threads[0]?.createdAt;
  const body = [
    XML_HEADER,
    '<rss version="2.0">',
    '  <channel>',
    '    <title>GoBBS 社区 - 最新主题</title>',
    '    <link>' + escapeXml(SITE_URL) + '</link>',
    '    <description>全站最近发布的公开主题</description>',
    '    <language>zh-CN</language>',
    latest ? '    <lastBuildDate>' + escapeXml(rfc822(latest)) + '</lastBuildDate>' : '',
    items,
    '  </channel>',
    '</rss>',
    '',
  ]
    .filter((line) => line !== '')
    .join('\n');

  return new NextResponse(body, {
    status: 200,
    headers: {
      'content-type': 'application/rss+xml; charset=utf-8',
      'cache-control': 'no-store',
      'x-content-type-options': 'nosniff',
    },
  });
}
