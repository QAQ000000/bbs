import type { Metadata } from 'next';
import Link from 'next/link';
import { notFound, redirect } from 'next/navigation';
import { getPost, getThread } from '@/lib/data.server';
import { getSession } from '@/lib/auth/server';
import { getSite } from '@/lib/site.server';
import { getSmileyMap } from '@/lib/markdown/smiley.server';
import { Breadcrumb } from '@/components/ui/Breadcrumb';
import { EditPostForm } from '@/components/content/EditPostForm';
import styles from './edit.module.css';

export const metadata: Metadata = {
  title: '编辑内容',
  robots: { index: false, follow: false },
};

export default async function EditPostPage({ params }: { params: { pid: string } }) {
  const post = await getPost(params.pid);
  if (!post) notFound();

  const session = await getSession();
  if (!session.user) {
    redirect('/login?next=' + encodeURIComponent('/posts/' + params.pid + '/edit'));
  }

  if (!post.capabilities.canEdit) {
    return (
      <div className={styles.page}>
        <div className={['panel', styles.forbidden].join(' ')}>
          <h1>无权限编辑</h1>
          <p>你只能编辑自己的内容；管理员或版主还需要在管辖版块内。</p>
          <Link href={'/threads/' + post.threadId + '#p' + post.id}>返回主题</Link>
        </div>
      </div>
    );
  }

  const [thread, site, smileys] = await Promise.all([getThread(post.threadId), getSite(), getSmileyMap()]);

  return (
    <div className={styles.page}>
      <Breadcrumb
        items={[
          { label: '首页', href: '/' },
          { label: '返回主题', href: '/threads/' + post.threadId },
          { label: '编辑内容' },
        ]}
      />
      <h1 className={styles.title}>编辑内容</h1>
      <p className={styles.subtitle}>
        当前版本 v{post.version}。修改仅在保存成功后生效；发生版本冲突时本地内容会保留。
      </p>
      <EditPostForm
        post={post}
        threadTitle={thread?.title ?? post.content.slice(0, 40)}
        forumId={thread?.forumId ?? ''}
        smileys={smileys}
        uploadEnabled={Boolean(site?.uploadEnabled)}
      />
    </div>
  );
}
