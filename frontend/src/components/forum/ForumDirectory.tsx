'use client';

import { useState } from 'react';
import Link from 'next/link';
import { IconSearch } from '@arco-design/web-react/icon';
import type { CategoryWithForums } from '@/lib/api/types';
import { formatCount, formatRelative, isEmptyTime } from '@/lib/format';
import styles from '../../app/(public)/forums/forums.module.css';

export function ForumDirectory({ categories }: { categories: CategoryWithForums[] }) {
  const [query, setQuery] = useState('');
  const term = query.trim().toLocaleLowerCase();
  const groups = categories
    .map(category => ({
      ...category,
      forums: category.forums.filter(forum =>
        `${category.name} ${forum.name} ${forum.description}`.toLocaleLowerCase().includes(term),
      ),
    }))
    .filter(category => category.forums.length > 0);

  return (
    <>
      <div className={styles.heading}>
        <div>
          <h1 className={styles.title}>版块目录</h1>
          <p className={styles.subtitle}>按分类浏览全部公开版块</p>
        </div>
        <label className={styles.search}>
          <IconSearch aria-hidden="true" />
          <input
            type="search"
            aria-label="搜索版块"
            placeholder="搜索版块..."
            value={query}
            onChange={event => setQuery(event.target.value)}
          />
        </label>
      </div>
      {groups.length ? (
        <div className={styles.groups}>
          {groups.map(category => (
            <section key={category.id} className={styles.group} aria-label={category.name}>
              <h2 className={styles.groupTitle}>
                {category.name}
                <small>{category.forums.length} 个版块</small>
              </h2>
              <ul className={styles.forums}>
                {category.forums.map(forum => (
                  <li key={forum.id} className={styles.forum}>
                    <div className={styles.forumMain}>
                      <Link href={'/forums/' + forum.id} className={styles.forumName}>
                        {forum.name}
                      </Link>
                      <p className={styles.forumDesc}>{forum.description || '暂无版块说明'}</p>
                    </div>
                    <div className={styles.forumStats}>
                      <span>{formatCount(forum.threadCount)} 主题</span>
                      <span>{formatCount(forum.postCount)} 回复</span>
                      {forum.todayCount > 0 ? (
                        <span className={styles.today}>今日 {formatCount(forum.todayCount)}</span>
                      ) : null}
                    </div>
                    <div className={styles.forumLast}>
                      {!isEmptyTime(forum.lastPostAt) && forum.lastThreadId !== '0' ? (
                        <>
                          <Link href={'/threads/' + forum.lastThreadId} className={styles.lastThread}>
                            {forum.lastThreadTitle || '查看最新主题'}
                          </Link>
                          <span className={styles.lastMeta}>
                            {forum.lastPostAuthor || '未知'} · {formatRelative(forum.lastPostAt)}
                          </span>
                        </>
                      ) : (
                        <span className={styles.lastMeta}>暂无主题</span>
                      )}
                    </div>
                    <Link href={'/forums/' + forum.id} className={styles.enter}>
                      进入版块
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>
      ) : (
        <p className={styles.empty}>{term ? '没有匹配的版块。' : '当前没有可见版块。'}</p>
      )}
    </>
  );
}
