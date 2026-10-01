import { tagPath } from '@/lib/tag';
import Link from 'next/link';
import type { CategoryWithForums, TagView } from '@/lib/api/types';
import styles from './ForumNav.module.css';

const DOT_COLORS = ['#165DFF', '#00B42A', '#FF7D00', '#F53F3F', '#722ED1', '#0FC6C2', '#3491FA', '#FF9A2E'];

export interface ForumNavProps {
  categories: CategoryWithForums[];
  activeForumId?: string;
  totalThreads?: number;
  tags?: TagView[];
}

/** 首页左栏版块导航：分类内列出可读版块与主题数。 */
export function ForumNav({ categories, activeForumId, totalThreads, tags = [] }: ForumNavProps) {
  let index = 0;
  return (
    <aside className={styles.navPanel} aria-label="版块导航">
      <div className={['panel', styles.nav].join(' ')}>
        <p className={styles.title}>版块导航</p>
        <Link
          href="/forums"
          className={[styles.item, !activeForumId ? styles.active : ''].join(' ')}
          aria-current={!activeForumId ? 'page' : undefined}
        >
          <span className={styles.dot} style={{ background: 'var(--color-primary)' }} />
          <span className={styles.name}>全部版块</span>
          {typeof totalThreads === 'number' ? <span className={styles.count}>{totalThreads}</span> : null}
        </Link>
        {categories.map((category) => (
          <div key={category.id} className={styles.group}>
            <p className={styles.groupTitle}>{category.name}</p>
            {category.forums.map((forum) => {
              const color = DOT_COLORS[index % DOT_COLORS.length];
              index += 1;
              const active = activeForumId === forum.id;
              return (
                <Link
                  key={forum.id}
                  href={`/forums/${forum.id}`}
                  className={[styles.item, styles.sub, active ? styles.active : ''].join(' ')}
                  aria-current={active ? 'page' : undefined}
                >
                  <span className={styles.dot} style={{ background: color }} />
                  <span className={styles.name}>{forum.name}</span>
                  <span className={styles.count}>{forum.threadCount}</span>
                </Link>
              );
            })}
          </div>
        ))}
      </div>
      <section className={['panel', styles.tagPanel].join(' ')} aria-label="社区标签">
        <header className={styles.tagHeader}><h2>社区标签</h2><Link href="/tags">全部</Link></header>
        {tags.length ? <div className={styles.tags}>{tags.map((tag) => (
          <Link key={tag.id} href={tagPath(tag)} className={styles.tag}
            style={tag.color ? { color: tag.color, borderColor: tag.color } : undefined}>{tag.name}</Link>
        ))}</div> : <p className={styles.empty}>暂无标签</p>}
      </section>
    </aside>
  );
}
