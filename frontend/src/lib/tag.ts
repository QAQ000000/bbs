/** Numeric route segments are tag IDs; a numeric slug must use its ID link. */
export function tagPath(tag: { id: string; slug?: string }): string {
  const segment = tag.slug && !/^[0-9]+$/.test(tag.slug) ? tag.slug : tag.id;
  return '/tags/' + encodeURIComponent(segment);
}
