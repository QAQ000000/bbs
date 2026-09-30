import { NextResponse, type NextRequest } from 'next/server';

/**
 * 内容协商只处理“约定的公开主题页面”，且只在客户端明确优先接受 text/markdown 时切换。
 * 按 RFC 9110 §12.5.1 计算“有效权重”：每种表示先取最具体的匹配媒体范围，
 * 再用该范围的 q 值比较；通配符参与计算，不会被直接忽略，q=0 视为明确拒绝。
 */
interface MediaRange {
  type: string;
  q: number;
  order: number;
  specificity: number;
}

interface Weight {
  q: number;
  specificity: number;
  order: number;
}

function parseMediaRanges(header: string | null): MediaRange[] {
  const ranges: MediaRange[] = [];
  if (!header) return ranges;
  let order = 0;
  for (const rawPart of header.split(',')) {
    const part = rawPart.trim();
    if (!part) continue;
    const [typeRaw, ...params] = part.split(';').map((piece) => piece.trim());
    const type = (typeRaw ?? '').toLowerCase();
    let q = 1;
    for (const param of params) {
      const match = /^q=([0-9.]+)$/i.exec(param);
      if (match) {
        const value = Number(match[1]);
        q = Number.isFinite(value) ? Math.min(Math.max(value, 0), 1) : 0;
      }
    }
    const specificity = type === '*/*' ? 0 : type.endsWith('/*') ? 1 : 2;
    ranges.push({ type, q, order: order++, specificity });
  }
  return ranges;
}

/** 某个具体表示的有效权重：取其最具体匹配范围；同具体度取先出现者。 */
function effectiveWeight(ranges: MediaRange[], candidate: string): Weight | null {
  const [type] = candidate.split('/');
  let best: Weight | null = null;
  for (const range of ranges) {
    const matches = range.type === candidate || range.type === '*/*' || range.type === type + '/*';
    if (!matches) continue;
    if (
      !best ||
      range.specificity > best.specificity ||
      (range.specificity === best.specificity && range.order < best.order)
    ) {
      best = { q: range.q, specificity: range.specificity, order: range.order };
    }
  }
  return best;
}

export function prefersMarkdown(header: string | null): boolean {
  const ranges = parseMediaRanges(header);
  const markdown = effectiveWeight(ranges, 'text/markdown');
  const html = effectiveWeight(ranges, 'text/html');
  // 未出现 text/markdown 或明确 q=0：保持 HTML。
  if (!markdown || markdown.q <= 0) return false;
  // HTML 不可接受（未出现或 q=0）：切换到 Markdown。
  if (!html || html.q <= 0) return true;
  // 先比有效权重；再比具体程度；最后只有显式列出 text/markdown 时才按出现顺序。
  if (markdown.q !== html.q) return markdown.q > html.q;
  if (markdown.specificity !== html.specificity) return markdown.specificity > html.specificity;
  if (markdown.specificity < 2) return false;
  return markdown.order <= html.order;
}

// 追加而不是覆盖：框架可能已经写过 Vary（RSC 相关值），两者需要并存。
function appendVary(headers: Headers, value: string): void {
  const existing = headers.get('Vary');
  if (!existing) {
    headers.set('Vary', value);
    return;
  }
  const already = existing
    .split(',')
    .map((piece) => piece.trim().toLowerCase())
    .includes(value.toLowerCase());
  if (!already) headers.append('Vary', value);
}

export function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;

  // 显式 Markdown 地址：/content/threads/{id}.md -> /content/threads/{id}
  const explicit = /^\/content\/threads\/([^/]+)\.md$/.exec(pathname);
  if (explicit) {
    const url = request.nextUrl.clone();
    url.pathname = '/content/threads/' + explicit[1];
    const response = NextResponse.rewrite(url);
    appendVary(response.headers, 'Accept');
    return response;
  }

  // 公共主题页内容协商：只匹配 /threads/{id}（恰好一段，不含 /reply 等子路径）。
  const page = /^\/threads\/([^/]+)$/.exec(pathname);
  if (page) {
    if (prefersMarkdown(request.headers.get('accept'))) {
      const url = request.nextUrl.clone();
      url.pathname = '/content/threads/' + page[1];
      const response = NextResponse.rewrite(url);
      appendVary(response.headers, 'Accept');
      return response;
    }
    const response = NextResponse.next();
    appendVary(response.headers, 'Accept');
    return response;
  }

  return NextResponse.next();
}

export const config = {
  // 只在这些公开路径上运行；登录、后台、API、附件与 Next 内部请求不参与格式切换。
  matcher: ['/threads/:path*', '/content/threads/:path*'],
};
