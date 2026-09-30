// 侧栏专用轻量图标（Server Component 可用）。
interface Props {
  size?: number;
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

export function IconMegaphone({ size = 16 }: Props) {
  return (
    <svg {...base(size)}>
      <path d="M4 10v4a1 1 0 0 0 1 1h2l6 4V5L7 9H5a1 1 0 0 0-1 1Z" />
      <path d="M17 9a4 4 0 0 1 0 6" />
    </svg>
  );
}

export function IconCalendar({ size = 16 }: Props) {
  return (
    <svg {...base(size)}>
      <rect x="3.5" y="5" width="17" height="15" rx="2.5" />
      <path d="M3.5 9.5h17M8 3.5V6M16 3.5V6" />
    </svg>
  );
}

export function IconPen({ size = 16 }: Props) {
  return (
    <svg {...base(size)}>
      <path d="M4 20h4l10-10a2.1 2.1 0 0 0-3-3L5 17Z" />
      <path d="M14.5 6.5l3 3" />
    </svg>
  );
}

export function IconHeart({ size = 16 }: Props) {
  return (
    <svg {...base(size)}>
      <path d="M12 20s-7-4.3-7-9.3A4 4 0 0 1 12 8a4 4 0 0 1 7 2.7C19 15.7 12 20 12 20Z" />
    </svg>
  );
}

export function IconComment({ size = 16 }: Props) {
  return (
    <svg {...base(size)}>
      <path d="M20 14a3 3 0 0 1-3 3H8l-4 3V6a3 3 0 0 1 3-3h10a3 3 0 0 1 3 3Z" />
    </svg>
  );
}

export function IconChart({ size = 16 }: Props) {
  return (
    <svg {...base(size)}>
      <path d="M4 20V10M10 20V4M16 20v-7M22 20H2" />
    </svg>
  );
}
