import Link from 'next/link';
import styles from './SiteFooter.module.css';

const LINKS = [
  { href: '/about', label: '关于我们' },
  { href: '/terms', label: '社区协议' },
  { href: '/privacy', label: '隐私政策' },
  { href: '/help', label: '帮助中心' },
];

export function SiteFooter({ footerText }: { footerText?: string }) {
  const year = 2026;
  return (
    <footer className={styles.footer}>
      <div className={['container', styles.inner].join(' ')}>
        <span className={styles.copy}>Copyright © {year} GoBBS</span>
        <nav className={styles.links} aria-label="页脚链接">
          {LINKS.map((item) => (
            <Link key={item.href} href={item.href} className={styles.link}>
              {item.label}
            </Link>
          ))}
        </nav>
        {footerText ? <span className={styles.extra}>{footerText}</span> : null}
      </div>
    </footer>
  );
}
