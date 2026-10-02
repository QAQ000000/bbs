import Link from 'next/link';
import { Logo } from '@/components/layout/Logo';

export default function NotFound() {
  return (
    <main
      style={{
        minHeight: '100vh',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 12,
        padding: 24,
        textAlign: 'center',
      }}
    >
      <Logo size={40} />
      <h1 style={{ margin: '12px 0 0', fontSize: 22 }}>页面不存在</h1>
      <p style={{ margin: 0, color: 'var(--color-text-tertiary)' }}>
        你访问的内容可能已被删除、隐藏，或链接有误。
      </p>
      <Link
        href="/"
        style={{
          marginTop: 12,
          display: 'inline-flex',
          alignItems: 'center',
          height: 36,
          padding: '0 20px',
          borderRadius: 6,
          background: 'var(--color-primary)',
          color: '#fff',
        }}
      >
        返回首页
      </Link>
    </main>
  );
}
