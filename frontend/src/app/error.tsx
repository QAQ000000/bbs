'use client';

export default function GlobalError({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <div
      style={{
        minHeight: '60vh',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 12,
        padding: 24,
        textAlign: 'center',
      }}
    >
      <h1 style={{ margin: 0, fontSize: 20 }}>页面加载失败</h1>
      <p style={{ margin: 0, color: 'var(--color-text-tertiary)' }}>
        服务暂时不可用，请稍后重试。已加载的内容不会丢失。
      </p>
      <button
        type="button"
        onClick={reset}
        style={{
          marginTop: 12,
          height: 36,
          padding: '0 20px',
          borderRadius: 6,
          border: '1px solid var(--color-primary)',
          background: 'transparent',
          color: 'var(--color-primary)',
          cursor: 'pointer',
        }}
      >
        重新加载
      </button>
    </div>
  );
}
