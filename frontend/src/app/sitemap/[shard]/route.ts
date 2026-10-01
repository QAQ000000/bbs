import { tagPath } from '@/lib/tag';
import { NextResponse } from 'next/server';
import { serverRequest } from '@/lib/api/server';
import {
  SITEMAP_PER_PAGE,
  SITEMAP_PAGES_PER_SHARD,
  SITEMAP_TAG_PAGES_PER_SHARD,
  sitemapShardCount,
} from '@/lib/sitemap';
import { SITE_URL } from '@/lib/site-url';
import { XML_CONTENT_TYPE, XML_HEADER, escapeXml } from '@/lib/xml';

export const dynamic = 'force-dynamic';

const PER_PAGE = SITEMAP_PER_PAGE;
const PAGES_PER_SHARD = SITEMAP_PAGES_PER_SHARD;

interface ThreadIndexRow {
  id: string;
  updatedAt: string;
  lastPostAt: string;
}

interface ThreadIndexPayload {
  threads: ThreadIndexRow[];
}

const STATIC_PATHS = ['/', '/forums', '/tags', '/leaderboard', '/terms', '/privacy'];

function urlEntry(loc: string, lastModified?: string | null): string {
  const date = lastModified ? new Date(lastModified) : null;
  const stamp = date && !Number.isNaN(date.getTime()) ? date.toISOString() : '';
  return (
    '  <url><loc>' +
    escapeXml(loc) +
    '</loc>' +
    (stamp ? '<lastmod>' + stamp + '</lastmod>' : '') +
    '</url>'
  );
}

function failure(message: string) {
  return new NextResponse(message, {
    status: 503,
    headers: { 'content-type': 'text/plain; charset=utf-8', 'cache-control': 'no-store' },
  });
}

function notFound(message: string) {
  return new NextResponse(message, {
    status: 404,
    headers: { 'content-type': 'text/plain; charset=utf-8', 'cache-control': 'no-store' },
  });
}

function xml(entries: string[]) {
  const body =
    XML_HEADER +
    '\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n' +
    entries.join('\n') +
    '\n</urlset>\n';
  return new NextResponse(body, {
    status: 200,
    headers: { 'content-type': XML_CONTENT_TYPE, 'cache-control': 'no-store' },
  });
}

/** 目录分片仅包含静态入口与版块；标签由独立的 tags-{i}.xml 分片覆盖。 */
async function directoryShard() {
  const entries: string[] = STATIC_PATHS.map((path) => urlEntry(SITE_URL + path));
  try {
    const categories = await serverRequest<{ forums?: { id: string; lastPostAt?: string }[] }[]>('/api/v1/forums', {
      forwardCookies: false,
    });
    for (const category of categories.data ?? []) {
      for (const forum of category.forums ?? []) {
        entries.push(urlEntry(SITE_URL + '/forums/' + forum.id, forum.lastPostAt));
      }
    }
  } catch {
    return failure('版块目录读取失败，请稍后重试。');
  }
  return xml(entries);
}

async function tagShard(index: number, startPage: number) {
  const entries: string[] = [];
  try {
    // 先读取第一页确认分片边界，避免将任意大编号直接传给后端分页。
    const first = await serverRequest<{ id: string; slug?: string }[]>('/api/v1/tags', {
      query: { page: 1 },
      forwardCookies: false,
    });
    const totalPages = first.meta?.totalPages ?? 0;
    if (index >= sitemapShardCount(totalPages, SITEMAP_TAG_PAGES_PER_SHARD)) {
      return notFound('标签 sitemap 分片超出范围。');
    }
    const endPage = Math.min(totalPages, startPage + SITEMAP_TAG_PAGES_PER_SHARD - 1);
    for (let page = startPage; page <= endPage; page += 1) {
      const tags = page === 1 ? first : await serverRequest<{ id: string; slug?: string }[]>('/api/v1/tags', {
        query: { page },
        forwardCookies: false,
      });
      for (const tag of tags.data ?? []) {
        entries.push(urlEntry(SITE_URL + tagPath(tag)));
      }
      if ((tags.data ?? []).length === 0) break;
    }
  } catch {
    return failure('标签目录读取失败，请稍后重试。');
  }
  return xml(entries);
}

export async function GET(_request: Request, { params }: { params: { shard: string } }) {
  const shard = (params.shard ?? '').replace(/\.xml$/, '');
  if (shard === 'pages') return directoryShard();
  const match = /^(threads|tags)-(0|[1-9]\d*)$/.exec(shard);
  if (!match) return notFound('未知的 sitemap 分片。');
  const index = Number(match[2]);
  const pagesPerShard = match[1] === 'tags' ? SITEMAP_TAG_PAGES_PER_SHARD : PAGES_PER_SHARD;
  const startPage = index * pagesPerShard + 1;
  if (!Number.isSafeInteger(index) || !Number.isSafeInteger(startPage + pagesPerShard - 1)) {
    return notFound('无效的 sitemap 分片编号。');
  }
  if (match[1] === 'tags') return tagShard(index, startPage);

  const rows: ThreadIndexRow[] = [];
  try {
    const first = await serverRequest<ThreadIndexPayload>('/api/v1/index/threads', {
      query: { sort: 'updated', page: 1, size: PER_PAGE },
      forwardCookies: false,
    });
    if (index >= sitemapShardCount(first.meta?.total ?? 0)) {
      return notFound('主题 sitemap 分片超出范围。');
    }
    const totalPages = first.meta?.totalPages ?? 0;
    const endPage = Math.min(totalPages, startPage + PAGES_PER_SHARD - 1);
    for (let current = startPage; current <= endPage; current += 1) {
      const page = current === 1 ? first : await serverRequest<ThreadIndexPayload>('/api/v1/index/threads', {
        query: { sort: 'updated', page: current, size: PER_PAGE },
        forwardCookies: false,
      });
      const batch = page.data?.threads ?? [];
      rows.push(...batch);
      if (batch.length < PER_PAGE) break;
    }
  } catch {
    return failure('主题索引读取失败，请稍后重试。');
  }

  return xml(rows.map((row) => urlEntry(SITE_URL + '/threads/' + row.id, row.updatedAt || row.lastPostAt)));
}
