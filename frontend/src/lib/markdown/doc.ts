import type { PostView, ThreadSummary } from '../api/types';

export interface ThreadMarkdownInput {
  thread: ThreadSummary;
  posts: PostView[];
  page: number;
  totalPages: number;
  /** 真实内容更新时间（含楼层编辑）；缺省时退回最后回复时间。 */
  updatedAt?: string | null;
  siteName: string;
  siteUrl: string;
}

/** 转义 Markdown 行内可能被解析的字符（标题、作者名等可信性不足的文本）。 */
function escapeInline(value: string): string {
  return (value ?? '').replace(/[\\`*_{}\[\]()#+.!|>-]/g, (char) => '\\' + char);
}

function iso(value: string | null | undefined): string {
  if (!value) return '';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '' : date.toISOString();
}

/** 把正文里的站内相对链接 / 图片改成绝对地址，不改写外链。 */
export function absolutizeLinks(markdown: string, siteUrl: string): string {
  return markdown.replace(/(!?\[[^\]]*\]\()(\/[^)\s]+)(\))/g, (_match, open: string, path: string, close: string) => {
    return open + siteUrl + path + close;
  });
}

/**
 * 生成主题的公开 Markdown 文档：标题、规范 HTML 地址、作者与时间、
 * 正文、当前页回复、页码与下一页链接。只组织游客可见的数据。
 */
export function renderThreadMarkdown(input: ThreadMarkdownInput): string {
  const { thread, posts, page, totalPages, siteName, siteUrl } = input;
  // 后续页的规范地址与 Markdown 地址都保留页码。
  const pageQuery = page > 1 ? '?page=' + page : '';
  const htmlUrl = siteUrl + '/threads/' + thread.id + pageQuery;
  const mdUrl = siteUrl + '/content/threads/' + thread.id + '.md';
  const pages = Math.max(totalPages, 1);
  const lines: string[] = [];

  lines.push('# ' + escapeInline(thread.title));
  lines.push('');
  lines.push('> ' + (siteName || 'GoBBS') + ' · 主题 #' + thread.id);
  lines.push('');
  lines.push('- 规范地址: ' + htmlUrl);
  lines.push('- Markdown: ' + mdUrl + pageQuery);
  lines.push('- 作者: ' + escapeInline(thread.authorName));
  lines.push('- 发布时间: ' + iso(thread.createdAt));
  lines.push('- 最后更新: ' + iso(input.updatedAt ?? thread.lastPostAt));
  lines.push('- 页码: ' + page + ' / ' + pages);
  lines.push('');
  lines.push('---');
  lines.push('');

  const bodyPost = page === 1 ? posts[0] : undefined;
  const replies = page === 1 ? posts.slice(1) : posts;
  if (bodyPost) {
    lines.push('## 正文');
    lines.push('');
    lines.push(absolutizeLinks((bodyPost.content ?? '').trim(), siteUrl));
    lines.push('');
  }
  lines.push('## 回复（第 ' + page + ' 页，共 ' + replies.length + ' 条）');
  lines.push('');
  if (replies.length === 0) {
    lines.push('_本页没有回复。_');
    lines.push('');
  }
  for (const post of replies) {
    lines.push('### ' + post.floor + ' 楼 · ' + escapeInline(post.authorName) + ' · ' + iso(post.createdAt));
    lines.push('');
    lines.push(absolutizeLinks((post.content ?? '').trim(), siteUrl));
    lines.push('');
  }
  lines.push('---');
  lines.push('');
  if (page > 1) {
    lines.push('上一页: ' + mdUrl + (page - 1 > 1 ? '?page=' + (page - 1) : ''));
  }
  if (page < pages) {
    lines.push('下一页: ' + mdUrl + '?page=' + (page + 1));
  } else {
    lines.push('已是最后一页。');
  }
  lines.push('');
  return lines.join('\n');
}
