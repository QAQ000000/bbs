import type { ApiErrorBody } from './types';

/** 统一的 API 错误：保留 HTTP 状态与后端稳定 error.code。 */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly fields?: Record<string, string>;
  readonly requestId?: string;
  /** 原始响应体：部分状态（如 MFA_REQUIRED）不使用标准 error 信封。 */
  readonly raw?: unknown;

  constructor(
    status: number,
    code: string,
    message: string,
    fields?: Record<string, string>,
    requestId?: string,
    raw?: unknown,
  ) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.fields = fields;
    this.requestId = requestId;
    this.raw = raw;
  }

  static fromBody(status: number, body: unknown): ApiError {
    const parsed = body as ApiErrorBody | undefined;
    const err = parsed?.error;
    return new ApiError(
      status,
      err?.code || `HTTP_${status}`,
      err?.message || '请求失败，请稍后重试',
      err?.fields,
      parsed?.requestId,
      body,
    );
  }

  get isUnauthorized(): boolean {
    return this.status === 401;
  }
  get isForbidden(): boolean {
    return this.status === 403;
  }
  get isNotFound(): boolean {
    return this.status === 404;
  }
  get isConflict(): boolean {
    return this.status === 409;
  }
  get isRateLimited(): boolean {
    return this.status === 429;
  }
}

/**
 * Next.js 用带 DYNAMIC_SERVER_USAGE digest 的异常把页面标记为动态渲染；
 * 任何取数封装的 catch 都必须原样抛出，否则会把动态页面误判为静态预渲染失败。
 */
export function isDynamicUsageError(error: unknown): boolean {
  return Boolean(
    error && typeof error === 'object' && (error as { digest?: string }).digest === 'DYNAMIC_SERVER_USAGE',
  );
}

export function toApiError(error: unknown): ApiError {
  if (error instanceof ApiError) return error;
  if (error instanceof Error) return new ApiError(0, 'NETWORK_ERROR', error.message);
  return new ApiError(0, 'NETWORK_ERROR', '网络异常，请稍后重试');
}
