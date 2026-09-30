/** sitemap 分片规格：每个分片最多 5 × 100 条主题，分片内部读取有界。 */
export const SITEMAP_SHARD_SIZE = 500;
export const SITEMAP_PAGES_PER_SHARD = 5;
export const SITEMAP_PER_PAGE = 100;
/** 标签接口每页 30 条；每片最多读取 10 页，不限制总分片数。 */
export const SITEMAP_TAG_PAGES_PER_SHARD = 10;

export function sitemapShardCount(total: number, size = SITEMAP_SHARD_SIZE): number {
  return Math.max(1, Math.ceil(total / size));
}
