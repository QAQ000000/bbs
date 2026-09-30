'use client';

import { Message } from '@arco-design/web-react';
import { ApiError } from '@/lib/api/errors';

export function toastSuccess(content: string): void {
  Message.success(content);
}

export function toastError(error: unknown): void {
  let content = '操作失败，请稍后重试';
  if (error instanceof ApiError) content = error.message;
  else if (error instanceof Error) content = error.message;
  Message.error(content);
}

export function toastInfo(content: string): void {
  Message.info(content);
}
