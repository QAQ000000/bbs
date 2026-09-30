import type { EquippedTitle, MemberLevel } from '@/lib/api/types';
import styles from './LevelBadge.module.css';

export function LevelBadge({ level, size = 'md' }: { level?: MemberLevel | null; size?: 'sm' | 'md' }) {
  if (!level) return null;
  const badge = level.badge;
  const style = badge
    ? { color: badge.color || undefined, background: badge.background || undefined }
    : undefined;
  return (
    <span className={[styles.badge, styles[size]].join(' ')} style={style} title={level.name}>
      {badge?.label || level.name}
    </span>
  );
}

export function TitleBadge({ title }: { title?: EquippedTitle | null }) {
  if (!title) return null;
  return (
    <span
      className={[styles.badge, styles.md, styles.title].join(' ')}
      style={title.color ? { color: title.color, background: 'transparent', borderColor: title.color } : undefined}
    >
      {title.name}
    </span>
  );
}

export function LevelName({ level }: { level?: MemberLevel | null }) {
  if (!level) return null;
  return <span className={styles.levelName}>{level.name}</span>;
}
