import { NextResponse, type NextRequest } from 'next/server';
import { ApiError, isDynamicUsageError } from '@/lib/api/errors';
import { serverGet, serverRequest } from '@/lib/api/server';
import type { PostView, SiteView, ThreadSummary } from '@/lib/api/types';
import { renderThreadMarkdown } from '@/lib/markdown/doc';
import { SITE_URL } from '@/lib/site-url';

export const dynamic = 'force-dynamic';

const MARKDOWN_HEADERS = {
  'content-type': 'text/markdown; charset=utf-8',
  // 同一 URL 可能按 Accept 返回 HTML 或 Markdown：必须声明 Vary，且两种表示都 no-store。
  vary: 'Accept',
  'cache-control': 'no-store',
  'x-content-type-options': 'nosniff',
};

function parsePage(raw: string | null): number {
  const n = Number.parseInt(raw ?? '1', 10);
  return Number.isFinite(n) && n > 0 ? Math.min(n, 10000) : 1;
}

function errorResponse(status: number, message: string) {
  return new NextResponse('# ' + message + '\n', {
    status,
    headers: { ...MARKDOWN_HEADERS, 'x-error': String(status) },
  });
}

export async function GET(request: NextRequest, { params }: { params: { tid: string } }) {
  const tid = params.tid;
  if (!/^[1-9][0-9]*$/.test(tid)) return errorResponse(404, '主题不存在或不可见');

  const page = parsePage(request.nextUrl.searchParams.get('page'));
  // 固定游客身份取数：不转发请求者 Cookie，管理员访问也不会导出待审或受限内容。
  const guest = { forwardCookies: false } as const;
  try {
    const thread = await serverGet<ThreadSummary>('/api/v1/threads/' + tid, guest);
    const postsPage = await serverRequest<PostView[]>('/api/v1/threads/' + tid + '/posts', {
      query: { page },
      ...guest,
    });
    const site = await serverGet<SiteView>('/api/v1/site', guest).catch(() => null);
    const totalPages = Math.max(postsPage.meta?.totalPages ?? 1, 1);
    // 越界页不是“成功空文档”：与主题不存在一致返回 404。
    if (page > totalPages) return errorResponse(404, '页码超出范围');
    const body = renderThreadMarkdown({
      thread,
      posts: postsPage.data ?? [],
      page,
      totalPages,
      updatedAt: thread.updatedAt ?? thread.lastPostAt,
      siteName: site?.name ?? 'GoBBS 社区',
      siteUrl: SITE_URL,
    });
    // canonical 指向对应的 HTML 页面，后续页保留页码。
    const htmlUrl = SITE_URL + '/threads/' + tid + (page > 1 ? '?page=' + page : '');
    return new NextResponse(body, {
      status: 200,
      headers: {
        ...MARKDOWN_HEADERS,
        link: '<' + htmlUrl + '>; rel="canonical"',
      },
    });
  } catch (error) {
    if (isDynamicUsageError(error)) throw error;
    if (error instanceof ApiError && error.status === 404) return errorResponse(404, '主题不存在或不可见');
    return errorResponse(503, '内容暂时不可用');
  }
}
