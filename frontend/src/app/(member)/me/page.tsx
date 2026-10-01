import type { Metadata } from 'next';
import Link from 'next/link';
import { safeGet } from '@/lib/api/server';
import type { CurrentUser, OwnContentItem } from '@/lib/api/types';
import { formatCount, formatDateTime, formatRelative } from '@/lib/format';
import { Avatar } from '@/components/ui/Avatar';
import { LevelBadge, TitleBadge } from '@/components/ui/LevelBadge';
import { EmptyState } from '@/components/ui/StateView';
import { requireMember } from '../guard';
import styles from './me.module.css';

export const metadata: Metadata = {
  title: '我的主页',
  robots: { index: false, follow: false },
};

const STATUS_LABELS: Record<string, string> = {
  published: '已发布',
  pending: '待审核',
  deleted: '已删除',
  rejected: '未通过',
};

export default async function MyHomePage() {
  await requireMember('/me');
  const [me, content] = await Promise.all([
    safeGet<CurrentUser>('/api/v1/me'),
    safeGet<OwnContentItem[]>('/api/v1/me/content'),
  ]);
  const items = content ?? [];

  return (
    <div>
      <section className={styles.profile}>
        <Avatar userId={me?.id} name={me?.username} size={72} className={styles.avatar} />
        <div className={styles.profileBody}>
          <h1 className={styles.name}>
            {me?.username ?? '我'}
            <LevelBadge level={me?.level} />
            <TitleBadge title={me?.equippedTitle} />
          </h1>
          <p className={styles.signature}>{me?.signature || '还没有填写签名'}</p>
          <p className={styles.meta}>
            <span>注册于 {formatDateTime(me?.createdAt)}</span>
            {me?.email ? (
              <>
                <span className={styles.dotSep}>·</span>
                <span>{me.email}{me.emailVerified ? '（已验证）' : '（未验证）'}</span>
              </>
            ) : null}
          </p>
        </div>
        <div className={styles.profileActions}>
          <Link className={styles.action} href="/me/settings">
            编辑资料
          </Link>
          <Link className={styles.actionSecondary} href={'/users/' + (me?.id ?? '')}>
            公开主页
          </Link>
        </div>
      </section>

      {me ? (
        <dl className={styles.stats}>
          <div><dd>{formatCount(me.postCount)}</dd><dt>发帖</dt></div>
          {me.points !== undefined ? <div><dd>{formatCount(me.points)}</dd><dt>积分</dt></div> : null}
          {me.experience !== undefined ? <div><dd>{formatCount(me.experience)}</dd><dt>经验</dt></div> : null}
        </dl>
      ) : null}

      <section className={styles.content}>
        <header className={styles.contentHeader}>
          <h2 className={styles.contentTitle}>我的内容</h2>
          <span className={styles.contentHint}>包含待审核与已删除内容的本人视图</span>
        </header>
        {items.length > 0 ? (
          <ul className={styles.list}>
            {items.map((item) => (
              <li key={item.id} className={styles.item}>
                <Link className={styles.itemTitle} href={'/threads/' + item.threadId + '#p' + item.id}>
                  {item.subject || '（无标题）'}
                </Link>
                <p className={styles.itemExcerpt}>{item.content.slice(0, 120)}</p>
                <p className={styles.itemMeta}>
                  <span className={styles.status}>{STATUS_LABELS[item.status] ?? item.status}</span>
                  <span>{item.floor} 楼</span>
                  <span className={styles.dotSep}>·</span>
                  <span>{formatRelative(item.createdAt)}</span>
                  {item.moderationNote ? <span className={styles.note}>审核说明：{item.moderationNote}</span> : null}
                </p>
              </li>
            ))}
          </ul>
        ) : (
          <EmptyState title="还没有发布内容" description="去版块发起你的第一个主题吧。" action={<Link className={styles.action} href="/new">发布主题</Link>} />
        )}
      </section>
    </div>
  );
}
