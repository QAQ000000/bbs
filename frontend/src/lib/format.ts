// 时间与数字格式化：SSR 与浏览器输出一致，避免 hydration 差异。

const DATE_TIME = new Intl.DateTimeFormat('zh-CN', {
  timeZone: 'Asia/Shanghai',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
});

const MONTH_DAY = new Intl.DateTimeFormat('zh-CN', {
  timeZone: 'Asia/Shanghai',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
});

/** 后端零值时间（1970）表示“无”，统一返回空字符串。 */
export function isEmptyTime(value?: string | null): boolean {
  if (!value) return true;
  const t = Date.parse(value);
  return Number.isNaN(t) || t <= 0 || new Date(t).getUTCFullYear() <= 1970;
}

export function formatDateTime(value?: string | null): string {
  if (isEmptyTime(value)) return '';
  return DATE_TIME.format(new Date(value as string)).replace(/\//g, '-');
}

export function formatRelative(value?: string | null): string {
  if (isEmptyTime(value)) return '';
  const then = Date.parse(value as string);
  const diff = Date.now() - then;
  if (diff < 0) return formatDateTime(value);
  const min = Math.floor(diff / 60000);
  if (min < 1) return '刚刚';
  if (min < 60) return `${min} 分钟前`;
  const hour = Math.floor(min / 60);
  if (hour < 24) return `${hour} 小时前`;
  const day = Math.floor(hour / 24);
  if (day < 7) return `${day} 天前`;
  return MONTH_DAY.format(new Date(then)).replace(/\//g, '-');
}

export function formatCount(value?: number | null): string {
  const n = value ?? 0;
  if (n < 1000) return String(n);
  if (n < 10000) return `${(n / 1000).toFixed(1).replace(/\.0$/, '')}k`;
  if (n < 100000000) return `${(n / 10000).toFixed(1).replace(/\.0$/, '')}w`;
  return `${(n / 100000000).toFixed(1).replace(/\.0$/, '')}亿`;
}

export function formatSize(bytes?: number | null): string {
  const n = bytes ?? 0;
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

/** 资源 ID 一律按十进制字符串处理，避免 Number 精度丢失。 */
export function idText(value: string | number | null | undefined): string {
  return value === null || value === undefined ? '' : String(value);
}
