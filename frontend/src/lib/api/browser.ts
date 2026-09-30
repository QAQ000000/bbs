'use client';

import { ApiError } from './errors';
import type { ListEnvelope } from './types';

export const API_BASE = process.env.NEXT_PUBLIC_API_BASE || '/api/v1';

// CSRF token 只保存在内存中，由 SessionProvider 登录 / 初始化后同步；不写入 localStorage。
let csrfToken: string | null = null;
let csrfPromise: Promise<string> | null = null;

export function setCsrfToken(token: string | null): void {
  csrfToken = token;
}
export function getCsrfToken(): string | null {
  return csrfToken;
}

/**
 * 写请求前确保拿到当前会话的 CSRF。
 * 组件挂载即触发的写入可能早于 SessionProvider 初始化，这里惰性补一次 /session，
 * 避免首个写请求因空 token 被 403 拒绝。
 */
async function ensureCsrfToken(): Promise<string> {
  if (csrfToken) return csrfToken;
  if (!csrfPromise) {
    csrfPromise = (async () => {
      try {
        const res = await fetch(API_BASE + '/session', {
          headers: { accept: 'application/json' },
          credentials: 'same-origin',
          cache: 'no-store',
        });
        const body = (await res.json()) as { data?: { csrfToken?: string } };
        const token = body?.data?.csrfToken ?? '';
        if (token) setCsrfToken(token);
        return token;
      } catch {
        return '';
      } finally {
        csrfPromise = null;
      }
    })();
  }
  return csrfPromise;
}

export type QueryValue = string | number | boolean | undefined | null;

export interface BrowserRequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
  /** JSON 请求体。非 GET 且未提供 form 时默认发送 {}。 */
  body?: unknown;
  /** multipart 请求体，优先级高于 body。 */
  form?: FormData;
  query?: Record<string, QueryValue>;
  /** 是否附加 X-CSRF-Token，默认非 GET 请求为 true。 */
  csrf?: boolean;
  signal?: AbortSignal;
}

function buildUrl(path: string, query?: Record<string, QueryValue>): string {
  const qs = new URLSearchParams();
  if (query) {
    for (const [key, value] of Object.entries(query)) {
      if (value === undefined || value === null || value === '') continue;
      qs.set(key, String(value));
    }
  }
  const suffix = qs.toString();
  return suffix ? `${API_BASE}${path}?${suffix}` : `${API_BASE}${path}`;
}

export async function browserRequest<T>(path: string, options: BrowserRequestOptions = {}): Promise<ListEnvelope<T>> {
  const method = options.method ?? 'GET';
  const headers: Record<string, string> = { accept: 'application/json' };
  const needsCsrf = options.csrf ?? method !== 'GET';
  if (needsCsrf) {
    headers['X-CSRF-Token'] = await ensureCsrfToken();
  }
  let requestBody: BodyInit | undefined;
  if (options.form) {
    requestBody = options.form;
  } else if (method !== 'GET') {
    headers['content-type'] = 'application/json';
    requestBody = JSON.stringify(options.body ?? {});
  }

  let res: Response;
  try {
    res = await fetch(buildUrl(path, options.query), {
      method,
      headers,
      body: requestBody,
      credentials: 'same-origin',
      cache: 'no-store',
      signal: options.signal,
    });
  } catch (error) {
    throw new ApiError(0, 'NETWORK_ERROR', error instanceof Error ? error.message : '网络异常，请稍后重试');
  }

  const text = await res.text();
  let parsed: unknown = undefined;
  if (text) {
    try {
      parsed = JSON.parse(text);
    } catch {
      parsed = { error: { code: 'BAD_RESPONSE', message: '服务返回了无法解析的内容' } };
    }
  }
  if (!res.ok) {
    throw ApiError.fromBody(res.status, parsed);
  }
  if (parsed && typeof parsed === 'object' && 'error' in (parsed as object)) {
    throw ApiError.fromBody(res.status, parsed);
  }
  return (parsed ?? { data: undefined }) as ListEnvelope<T>;
}

export async function browserGet<T>(path: string, options: BrowserRequestOptions = {}): Promise<T> {
  const envelope = await browserRequest<T>(path, { ...options, method: 'GET' });
  return envelope.data;
}

export async function browserSend<T>(path: string, options: BrowserRequestOptions = {}): Promise<T> {
  const envelope = await browserRequest<T>(path, options);
  return envelope.data;
}

/** 读取当前会话；浏览器侧调用会携带 / 建立 HttpOnly 会话 Cookie。 */
export async function fetchSession(): Promise<import('./types').SessionView> {
  return browserGet<import('./types').SessionView>('/session');
}
