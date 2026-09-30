/** XML 文本转义：所有对外输出的 sitemap / RSS 共用。 */
export function escapeXml(value: string): string {
  return (value ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;');
}

export const XML_HEADER = '<?xml version="1.0" encoding="UTF-8"?>';

export const XML_CONTENT_TYPE = 'application/xml; charset=utf-8';
