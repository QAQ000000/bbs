'use client';

import { useState } from 'react';

/** 封面：加载失败时直接隐藏，回到纯文字信息流，避免出现破图。 */
export function CoverImage({ src, alt = '' }: { src: string; alt?: string }) {
  const [failed, setFailed] = useState(false);
  if (failed) return null;
  // eslint-disable-next-line @next/next/no-img-element
  return <img src={src} alt={alt} loading="lazy" onError={() => setFailed(true)} />;
}
