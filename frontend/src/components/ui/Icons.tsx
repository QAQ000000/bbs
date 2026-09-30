// 轻量内联图标：仅用于 Server Component，避免把客户端组件库引入 SSR 树。
interface Props {
  size?: number;
  className?: string;
}

function base(size: number) {
  return {
    width: size,
    height: size,
    viewBox: '0 0 24 24',
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.7,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
    'aria-hidden': true,
  };
}

export function IconComment({ size = 15, className }: Props) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M20 14a3 3 0 0 1-3 3H8l-4 3V6a3 3 0 0 1 3-3h10a3 3 0 0 1 3 3Z" />
    </svg>
  );
}

export function IconEye({ size = 15, className }: Props) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12Z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  );
}

export function IconHeart({ size = 15, className }: Props) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M12 20s-7-4.3-7-9.3A4 4 0 0 1 12 8a4 4 0 0 1 7 2.7C19 15.7 12 20 12 20Z" />
    </svg>
  );
}

export function IconClock({ size = 14, className }: Props) {
  return (
    <svg {...base(size)} className={className}>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 7.5V12l3 1.8" />
    </svg>
  );
}

export function IconShare({ size = 15, className }: Props) {
  return (
    <svg {...base(size)} className={className}>
      <circle cx="6" cy="12" r="2.5" />
      <circle cx="18" cy="6" r="2.5" />
      <circle cx="18" cy="18" r="2.5" />
      <path d="m8.2 10.8 7.6-3.6M8.2 13.2l7.6 3.6" />
    </svg>
  );
}

export function IconStar({ size = 14, className }: Props) {
  return (
    <svg {...base(size)} className={className}>
      <path d="m12 4 2.4 5 5.6.8-4 3.9 1 5.5-5-2.7-5 2.7 1-5.5-4-3.9 5.6-.8Z" />
    </svg>
  );
}

export function IconPin({ size = 14, className }: Props) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M9 3h6l-1 6 3.5 3.5H5.5L9 9Z" />
      <path d="M12 12.5V21" />
    </svg>
  );
}

export function IconCheck({ size = 14, className }: Props) {
  return (
    <svg {...base(size)} className={className}>
      <path d="m5 12.5 4.5 4.5L19 7" />
    </svg>
  );
}

export function IconMegaphone({ size = 16, className }: Props) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M4 10v4a1 1 0 0 0 1 1h2l6 4V5L7 9H5a1 1 0 0 0-1 1Z" />
      <path d="M17 9a4 4 0 0 1 0 6" />
    </svg>
  );
}

export function IconChart({ size = 16, className }: Props) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M4 20V10M10 20V4M16 20v-7M22 20H2" />
    </svg>
  );
}

export function IconDownload({ size = 15, className }: Props) {
  return (
    <svg {...base(size)} className={className}>
      <path d="M12 3v11m0 0 4-4m-4 4-4-4" />
      <path d="M4 19h16" />
    </svg>
  );
}
