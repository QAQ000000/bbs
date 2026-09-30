import Link from 'next/link';
import styles from './Avatar.module.css';

export interface AvatarProps {
  userId?: string | null;
  name?: string | null;
  size?: number;
  href?: string;
  className?: string;
}

/** 头像统一走后端受控 /avatar/{id}；缺省时退回用户名首字。 */
export function Avatar({ userId, name, size = 32, href, className }: AvatarProps) {
  const label = (name ?? '').trim();
  const src = userId ? `/avatar/${userId}` : '';
  const inner = src ? (
    // eslint-disable-next-line @next/next/no-img-element
    <img
      className={styles.image}
      src={src}
      alt={label ? `${label} 的头像` : '用户头像'}
      width={size}
      height={size}
      loading="lazy"
    />
  ) : (
    <span className={styles.fallback} aria-hidden="true">
      {label ? label.slice(0, 1) : '?'}
    </span>
  );
  const content = (
    <span className={styles.root} style={{ width: size, height: size }}>
      {inner}
    </span>
  );
  const cls = [styles.link, className].filter(Boolean).join(' ');
  if (href) {
    return (
      <Link href={href} className={cls} aria-label={label ? `查看 ${label} 的主页` : '用户主页'}>
        {content}
      </Link>
    );
  }
  return <span className={cls}>{content}</span>;
}
