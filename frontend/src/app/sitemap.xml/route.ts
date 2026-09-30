import { NextResponse } from 'next/server';
import { serverRequest } from '@/lib/api/server';
import { sitemapShardCount, SITEMAP_TAG_PAGES_PER_SHARD } from '@/lib/sitemap';
import { SITE_URL } from '@/lib/site-url';
import { XML_CONTENT_TYPE, XML_HEADER, escapeXml } from '@/lib/xml';

export const dynamic = 'force-dynamic';

export async function GET() {
  let total = 0;
  let tagPages = 0;
  try {
    const [page, tags] = await Promise.all([
      serverRequest<unknown>('/api/v1/index/threads', {
        query: { sort: 'updated', page: 1, size: 1 },
        forwardCookies: false,
      }),
      serverRequest<unknown>('/api/v1/tags', { query: { page: 1 }, forwardCookies: false }),
    ]);
    total = page.meta?.total ?? 0;
    tagPages = tags.meta?.totalPages ?? 0;
  } catch {
    // 读取失败必须区别于“空站”：返回 503，不伪装成空 sitemap。
    return new NextResponse('上游站点索引读取失败，请稍后重试。', {
      status: 503,
      headers: { 'content-type': 'text/plain; charset=utf-8', 'cache-control': 'no-store' },
    });
  }

  const shards: string[] = [SITE_URL + '/sitemap/pages.xml'];
  for (let i = 0; i < sitemapShardCount(total); i += 1) {
    shards.push(SITE_URL + '/sitemap/threads-' + i + '.xml');
  }
  for (let i = 0; i < sitemapShardCount(tagPages, SITEMAP_TAG_PAGES_PER_SHARD); i += 1) {
    shards.push(SITE_URL + '/sitemap/tags-' + i + '.xml');
  }
  const body =
    XML_HEADER +
    '\n<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n' +
    shards.map((loc) => '  <sitemap><loc>' + escapeXml(loc) + '</loc></sitemap>').join('\n') +
    '\n</sitemapindex>\n';

  return new NextResponse(body, {
    status: 200,
    headers: { 'content-type': XML_CONTENT_TYPE, 'cache-control': 'no-store' },
  });
}
