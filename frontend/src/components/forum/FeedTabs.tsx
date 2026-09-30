import Link from 'next/link';
import styles from './FeedTabs.module.css';

export interface FeedTabsProps {
  active: string;
  sortNote?: string;
}

const TABS = [
  { value: 'all', label: '综合' },
  { value: 'forums', label: '关注版块' },
  { value: 'people', label: '关注的人' },
];

export function FeedTabs({ active, sortNote }: FeedTabsProps) {
  return (
    <nav className={styles.tabs} aria-label="信息流切换">
      <div className={styles.left}>
        {TABS.map((tab) => {
          const isActive = tab.value === active;
          return (
            <Link
              key={tab.value}
              href={tab.value === 'all' ? '/' : `/?feed=${tab.value}`}
              className={isActive ? styles.active : styles.tab}
              aria-current={isActive ? 'page' : undefined}
            >
              {tab.label}
            </Link>
          );
        })}
      </div>
      {sortNote ? <span className={styles.note}>{sortNote}</span> : null}
    </nav>
  );
}
