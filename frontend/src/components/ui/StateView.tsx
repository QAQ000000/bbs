import Link from 'next/link';
import styles from './StateView.module.css';

export function EmptyState({
  title,
  description,
  action,
  icon,
}: {
  title: string;
  description?: string;
  action?: React.ReactNode;
  icon?: React.ReactNode;
}) {
  return (
    <div className={styles.state} role="status">
      <div className={styles.icon} aria-hidden="true">
        {icon ?? '🗒️'}
      </div>
      <p className={styles.title}>{title}</p>
      {description ? <p className={styles.desc}>{description}</p> : null}
      {action ? <div className={styles.action}>{action}</div> : null}
    </div>
  );
}

export function ErrorState({
  title = '加载失败',
  description = '内容暂时无法读取，请稍后重试。',
  retryHref,
  retryLabel = '重新加载',
}: {
  title?: string;
  description?: string;
  retryHref?: string;
  retryLabel?: string;
}) {
  return (
    <div className={styles.state} role="alert">
      <div className={[styles.icon, styles.errorIcon].join(' ')} aria-hidden="true">
        !
      </div>
      <p className={styles.title}>{title}</p>
      <p className={styles.desc}>{description}</p>
      {retryHref ? (
        <div className={styles.action}>
          <Link className={styles.retry} href={retryHref}>
            {retryLabel}
          </Link>
        </div>
      ) : null}
    </div>
  );
}
