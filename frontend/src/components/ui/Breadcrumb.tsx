import Link from 'next/link';
import styles from './Breadcrumb.module.css';

export interface Crumb {
  label: string;
  href?: string;
}

export function Breadcrumb({ items }: { items: Crumb[] }) {
  return (
    <nav className={styles.breadcrumb} aria-label="面包屑">
      {items.map((item, index) => (
        <span key={item.href ?? item.label + index} className={styles.item}>
          {index > 0 ? (
            <span className={styles.sep} aria-hidden="true">
              /
            </span>
          ) : null}
          {item.href ? (
            <Link href={item.href} className={styles.link}>
              {item.label}
            </Link>
          ) : (
            <span className={styles.current} aria-current="page">
              {item.label}
            </span>
          )}
        </span>
      ))}
    </nav>
  );
}
