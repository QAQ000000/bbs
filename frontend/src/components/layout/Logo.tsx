export function Logo({ size = 28 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" role="img" aria-label="GoBBS" focusable="false">
      <rect width="32" height="32" rx="8" fill="var(--color-primary)" />
      <path
        d="M9 8.5h14a2.5 2.5 0 0 1 2.5 2.5v7.5A2.5 2.5 0 0 1 23 21h-7.1L11 24.4V21H9a2.5 2.5 0 0 1-2.5-2.5V11A2.5 2.5 0 0 1 9 8.5Z"
        fill="#fff"
      />
      <circle cx="12.4" cy="15" r="1.3" fill="var(--color-primary)" />
      <circle cx="16" cy="15" r="1.3" fill="var(--color-primary)" />
      <circle cx="19.6" cy="15" r="1.3" fill="var(--color-primary)" />
    </svg>
  );
}
