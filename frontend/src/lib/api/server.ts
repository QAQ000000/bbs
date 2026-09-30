import { cookies } from 'next/headers';
import { ApiError, isDynamicUsageError } from './errors';
import type { ListEnvelope, PageMeta } from './types';

// SSR 使用仅服务端可见的固定内部地址；不接受请求 Host 或用户参数拼接。
export const API_INTERNAL_URL = (process.env.API_INTERNAL_URL || 'http://127.0.0.1:8090').replace(/\/+$/, '');

export type QueryValue = string | number | boolean | undefined | null;

export interface ServerRequestOptions {
  query?: Record<string, QueryValue>;
  /** 显式 Cookie 头；默认转发当前请求的 Cookie。 */
  cookie?: string | null;
  /** 是否转发当前请求 Cookie，默认 true。 */
  forwardCookies?: boolean;
  /** 默认 no-store：第一版关闭共享整页缓存，保证按请求 / 账号隔离。 */
  cache?: RequestCache;
}

function buildUrl(path: string, query?: Record<string, QueryValue>): string {
  const url = new URL(API_INTERNAL_URL + path);
  if (query) {
    for (const [key, value] of Object.entries(query)) {
      if (value === undefined || value === null || value === '') continue;
      url.searchParams.set(key, String(value));
    }
  }
  return url.toString();
}

function requestCookieHeader(): string {
  // cookies() 在静态预渲染阶段会抛出动态渲染信号，必须让它向上传播。
  return cookies().toString();
}

async function parse<T>(res: Response): Promise<T> {
  const text = await res.text();
  let body: unknown = undefined;
  if (text) {
    try {
      body = JSON.parse(text);
    } catch {
      body = { error: { code: 'BAD_RESPONSE', message: '服务返回了无法解析的内容' } };
    }
  }
  if (!res.ok) {
    throw ApiError.fromBody(res.status, body);
  }
  const envelope = body as { data?: T; error?: unknown } | undefined;
  if (!envelope || envelope.error) {
    throw ApiError.fromBody(res.status, body);
  }
  return body as T;
}

/** 返回完整信封（含分页 meta），供列表页使用。 */
export async function serverRequest<T>(path: string, options: ServerRequestOptions = {}): Promise<ListEnvelope<T>> {
  const headers: Record<string, string> = { accept: 'application/json' };
  if (options.forwardCookies !== false) {
    const cookie = options.cookie !== undefined ? options.cookie : requestCookieHeader();
    if (cookie) headers.cookie = cookie;
  }
  let res: Response;
  try {
    res = await fetch(buildUrl(path, options.query), {
      headers,
      cache: options.cache ?? 'no-store',
    });
  } catch (error) {
    if (isDynamicUsageError(error)) throw error;
    throw new ApiError(0, 'NETWORK_ERROR', error instanceof Error ? error.message : '无法连接后端服务');
  }
  return parse<ListEnvelope<T>>(res);
}

/** 仅需要 data 的场景。 */
export async function serverGet<T>(path: string, options: ServerRequestOptions = {}): Promise<T> {
  const envelope = await serverRequest<T>(path, options);
  return envelope.data;
}

export function metaTotalPages(meta?: PageMeta): number {
  return meta?.totalPages ?? 0;
}

/** 可选区块取数：失败返回 null，但不吞掉 Next 动态渲染信号。 */
export async function safeRequest<T>(path: string, options: ServerRequestOptions = {}): Promise<ListEnvelope<T> | null> {
  try {
    return await serverRequest<T>(path, options);
  } catch (error) {
    if (isDynamicUsageError(error)) throw error;
    return null;
  }
}

export async function safeGet<T>(path: string, options: ServerRequestOptions = {}): Promise<T | null> {
  try {
    return await serverGet<T>(path, options);
  } catch (error) {
    if (isDynamicUsageError(error)) throw error;
    return null;
  }
}
