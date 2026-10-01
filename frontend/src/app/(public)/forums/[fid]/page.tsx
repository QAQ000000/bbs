import type { Metadata } from 'next';
import Link from 'next/link';
import { notFound } from 'next/navigation';
import { getForum, getForumThreads } from '@/lib/data.server';
import { getSession } from '@/lib/auth/server';
import { FORUM_SORTS } from '@/lib/thread';
import { formatCount } from '@/lib/format';
import { Breadcrumb } from '@/components/ui/Breadcrumb';
import { ThreadList } from '@/components/forum/ThreadList';
import { Pagination } from '@/components/ui/Pagination';
import { ErrorState } from '@/components/ui/StateView';
import { SubscribeButton } from '@/components/forum/SubscribeButton';
import styles from './forum.module.css';

function parsePage(value?: string): number {
  const n = Number.parseInt(value ?? '1', 10);
  return Number.isFinite(n) && n > 0 ? Math.min(n, 10000) : 1;
}

function normalizeSort(value?: string): string {
  return FORUM_SORTS.some((s) => s.value === value) ? (value as string) : '';
}

export async function generateMetadata({ params }: { params: { fid: string } }): Promise<Metadata> {
  const forum = await getForum(params.fid);
  if (!forum) return { title: '版块不存在' };
  return {
    title: forum.name,
    description: forum.description || forum.name + ' 版块主题列表',
    alternates: { canonical: '/forums/' + forum.id },
  };
}

export default async function ForumDetailPage({
  params,
  searchParams,
}: {
  params: { fid: string };
  searchParams: { page?: string; sort?: string };
}) {
  const fid = params.fid;
  const page = parsePage(searchParams.page);
  const sort = normalizeSort(searchParams.sort);
  const forum = await getForum(fid);
  if (!forum) notFound();

  const [session, listPage] = await Promise.all([getSession(), getForumThreads(fid, page, sort)]);
  const data = listPage?.data;
  const meta = listPage?.meta;
  const canCreateThread = Boolean(forum.capabilities?.canCreateThread);
  const moderators = (forum.moderators || '')
    .split(/[,，、]/)
    .map((name) => name.trim())
    .filter(Boolean);

  return (
    <div className={styles.page}>
      <div className="hide-mobile"><Breadcrumb
        items={[
          { label: '首页', href: '/' },
          { label: '版块目录', href: '/forums' },
          { label: forum.name },
        ]}
      /></div>

      <section className={['panel', styles.header].join(' ')}>
            <div className={styles.headerMain}>
              <h1 className={styles.name}>
                <span className={styles.dot} aria-hidden="true" />
                {forum.name}
              </h1>
              <p className={styles.desc}>{forum.description || '暂无版块说明'}</p>
              <p className={styles.stats}>
                <span>{formatCount(forum.threadCount)} 个主题</span>
                <span className={styles.dotSep}>·</span>
                <span>{formatCount(forum.postCount)} 条回复</span>
                <span className={styles.dotSep}>·</span>
                <span>今日新增 {formatCount(forum.todayCount)}</span>
              </p>
            </div>
            <div className={styles.actions}>
              {session.user ? (
                <SubscribeButton key={`${session.user.id}:${forum.id}:${forum.subscribed}`} kind="forum" id={forum.id} initialSubscribed={forum.subscribed ?? null} />
              ) : (
                <Link className={styles.loginLink} href="/login">
                  登录后关注
                </Link>
              )}
              {session.user ? (
                canCreateThread ? (
                  <Link className={styles.publish} href={'/new?forumId=' + forum.id}>
                    发帖
                  </Link>
                ) : (
                  <span className={styles.publishDisabled} title="当前账号或版块状态不允许发帖">
                    发帖
                  </span>
                )
              ) : (
                <Link className={styles.publish} href={'/login?next=' + encodeURIComponent('/new?forumId=' + forum.id)}>
                  登录后发帖
                </Link>
              )}
            </div>
          </section>

          <nav className={styles.sorts} aria-label="主题排序">
            {FORUM_SORTS.map((option) => {
              const active = option.value === sort;
              const href = option.value ? '/forums/' + fid + '?sort=' + option.value : '/forums/' + fid;
              return (
                <Link
                  key={option.value || 'default'}
                  href={href}
                  className={active ? styles.sortActive : styles.sort}
                  aria-current={active ? 'page' : undefined}
                >
                  {option.label}
                  {option.hint ? <span className={styles.sortHint}>（{option.hint}）</span> : null}
                </Link>
              );
            })}
            {meta ? <span className={styles.total}>共 {formatCount(meta.total)} 个主题</span> : null}
          </nav>

      <div className={styles.grid}>
        <div className={styles.main}>
          {data ? (
            <ThreadList
              variant="browse"
              threads={data.threads}
              stickies={data.stickies}
              forumNames={{ [forum.id]: forum.name }}
              showForum={false}
              emptyTitle="本版块暂无主题"
              emptyDescription="还没有公开主题，登录后可以发布第一个主题。"
            />
          ) : (
            <div className="panel">
              <ErrorState
                title="主题列表加载失败"
                description="无法读取本版块的主题，请稍后重试。"
                retryHref={'/forums/' + fid}
              />
            </div>
          )}

          {meta ? (
            <div className={styles.paginationWrap}>
              <Pagination page={meta.page} totalPages={meta.totalPages} basePath={'/forums/' + fid} query={{ sort }} ariaLabel="版块分页" />
            </div>
          ) : null}
        </div>

        <aside className={styles.side}>
          <section className={['panel', styles.sideCard].join(' ')} aria-label="版块信息">
            <h2 className={styles.sideTitle}>版块信息</h2>
            <dl className={styles.sideStats}>
              <div>
                <dt>主题</dt>
                <dd>{formatCount(forum.threadCount)}</dd>
              </div>
              <div>
                <dt>回复</dt>
                <dd>{formatCount(forum.postCount)}</dd>
              </div>
              <div>
                <dt>今日</dt>
                <dd>{formatCount(forum.todayCount)}</dd>
              </div>
            </dl>
          </section>

          <section className={['panel', styles.sideCard].join(' ')} aria-label="版主">
            <h2 className={styles.sideTitle}>版主</h2>
            {moderators.length > 0 ? (
              <ul className={styles.moderators}>
                {moderators.map((name) => (
                  <li key={name} className={styles.moderator}>
                    {name}
                  </li>
                ))}
              </ul>
            ) : (
              <p className={styles.sideEmpty}>暂无公开版主信息</p>
            )}
          </section>

          <section className={['panel', styles.sideCard].join(' ')} aria-label="版块规则">
            <h2 className={styles.sideTitle}>发帖提示</h2>
            <ul className={styles.rules}>
              <li>尊重原创，转载请注明出处与作者。</li>
              <li>教程类主题建议附可复现的步骤或代码。</li>
              <li>提问前先搜索，避免重复发帖。</li>
              <li>遇到违规内容请使用举报功能。</li>
            </ul>
          </section>
        </aside>
      </div>
    </div>
  );
}
