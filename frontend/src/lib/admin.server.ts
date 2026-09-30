import { ApiError, isDynamicUsageError } from './api/errors';
import { serverGet, serverRequest } from './api/server';
import type { ListEnvelope } from './api/types';

export type AdminResult<T> = { ok: true; data: T } | { ok: false; forbidden: true };

/** 后台取数：403 表示当前账号无该权限，交给页面渲染无权限状态。 */
export async function adminGet<T>(path: string, query?: Record<string, string | number | undefined>): Promise<AdminResult<T>> {
  try {
    return { ok: true, data: await serverGet<T>(path, { query }) };
  } catch (error) {
    if (isDynamicUsageError(error)) throw error;
    if (error instanceof ApiError && error.status === 403) return { ok: false, forbidden: true };
    throw error;
  }
}

/**
 * 后台只读取数：区分“不存在（404）”“无权限（403）”“接口失败”。
 * 报表快照用 404 表示尚未生成，不能被零值掩盖，所以需要单独区分。
 */
export type AdminState<T> =
  | { status: 'ok'; data: T }
  | { status: 'notfound' }
  | { status: 'forbidden' }
  | { status: 'error' };

export async function adminGetState<T>(
  path: string,
  query?: Record<string, string | number | undefined>,
): Promise<AdminState<T>> {
  try {
    return { status: 'ok', data: await serverGet<T>(path, { query }) };
  } catch (error) {
    if (isDynamicUsageError(error)) throw error;
    if (error instanceof ApiError && error.status === 404) return { status: 'notfound' };
    if (error instanceof ApiError && error.status === 403) return { status: 'forbidden' };
    return { status: 'error' };
  }
}

export async function adminList<T>(
  path: string,
  query?: Record<string, string | number | undefined>,
): Promise<AdminResult<ListEnvelope<T>>> {
  try {
    return { ok: true, data: await serverRequest<T>(path, { query }) };
  } catch (error) {
    if (isDynamicUsageError(error)) throw error;
    if (error instanceof ApiError && error.status === 403) return { ok: false, forbidden: true };
    throw error;
  }
}
