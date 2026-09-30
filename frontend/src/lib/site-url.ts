// 可信公开站点基址：只来自部署配置，绝不从请求 Host / URL 参数推导。
// 与 Go 的 FORUM_SITE_URL、前端 canonical 使用同一个值。
const RAW = process.env.FORUM_SITE_URL || process.env.NEXT_PUBLIC_SITE_URL || 'http://127.0.0.1:3000';

export const SITE_URL = RAW.replace(/\/+$/, '');

/** 把站内相对路径转成绝对地址；已经是绝对地址的原样返回。 */
export function absoluteUrl(path: string): string {
  if (!path) return SITE_URL;
  if (/^https?:\/\//i.test(path)) return path;
  return SITE_URL + (path.startsWith('/') ? path : '/' + path);
}

/** 站内页面的规范绝对地址。 */
export function canonicalUrl(path: string): string {
  return absoluteUrl(path);
}
