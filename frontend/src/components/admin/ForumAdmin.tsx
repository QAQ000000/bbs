'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Input, Modal, Select } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { CategoryWithForums } from '@/lib/api/types';
import { formatCount } from '@/lib/format';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './ForumAdmin.module.css';

interface ForumDraft {
  id: string;
  categoryId: string;
  name: string;
  description: string;
  moderators: string;
}

const EMPTY_DRAFT: ForumDraft = { id: '', categoryId: '', name: '', description: '', moderators: '' };

export function ForumAdmin({ categories }: { categories: CategoryWithForums[] }) {
  const router = useRouter();
  const [draft, setDraft] = useState<ForumDraft | null>(null);
  const [busy, setBusy] = useState(false);
  const [catOpen, setCatOpen] = useState(false);
  const [catName, setCatName] = useState('');
  const [catId, setCatId] = useState('');

  async function submitForum() {
    if (!draft) return;
    setBusy(true);
    try {
      await browserSend('/admin/forums/save', {
        method: 'POST',
        body: {
          id: draft.id || '0',
          category_id: draft.categoryId,
          name: draft.name,
          description: draft.description,
          moderators: draft.moderators,
        },
      });
      toastSuccess(draft.id ? '版块已保存' : '版块已创建');
      setDraft(null);
      router.refresh();
    } catch (caught) {
      toastError(caught as ApiError);
    } finally {
      setBusy(false);
    }
  }

  async function removeForum(id: string, name: string) {
    Modal.confirm({
      title: '确认删除版块「' + name + '」？',
      content: '删除前请确认版块内主题已迁移或清理，操作会记录审计日志。',
      okText: '确认删除',
      cancelText: '取消',
      onOk: async () => {
        try {
          await browserSend('/admin/forums/delete', { method: 'POST', body: { id } });
          toastSuccess('版块已删除');
          router.refresh();
        } catch (caught) {
          toastError(caught as ApiError);
          throw caught;
        }
      },
    });
  }

  async function moveForum(id: string, dir: 'up' | 'down') {
    try {
      await browserSend('/admin/forums/move', { method: 'POST', body: { id, dir } });
      toastSuccess('排序已调整');
      router.refresh();
    } catch (caught) {
      toastError(caught as ApiError);
    }
  }

  async function submitCategory() {
    setBusy(true);
    try {
      await browserSend('/admin/cats/save', { method: 'POST', body: { id: catId || '0', name: catName } });
      toastSuccess('分类已保存');
      setCatOpen(false);
      setCatName('');
      setCatId('');
      router.refresh();
    } catch (caught) {
      toastError(caught as ApiError);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <div className={styles.toolbar}>
        <Button
          type="primary"
          onClick={() =>
            setDraft({ ...EMPTY_DRAFT, categoryId: categories[0]?.id ?? '' })
          }
        >
          新建版块
        </Button>
        <Button type="secondary" onClick={() => setCatOpen(true)}>
          新建分类
        </Button>
      </div>

      {categories.map((category) => (
        <section key={category.id} className={['panel', styles.section].join(' ')}>
          <header className={styles.sectionHead}>
            <h2 className={styles.sectionTitle}>{category.name}</h2>
            <button
              type="button"
              className={styles.linkBtn}
              onClick={() => {
                setCatId(category.id);
                setCatName(category.name);
                setCatOpen(true);
              }}
            >
              重命名
            </button>
          </header>
          <ul className={styles.list}>
            {category.forums.map((forum) => (
              <li key={forum.id} className={styles.item}>
                <div className={styles.itemBody}>
                  <p className={styles.itemName}>{forum.name}</p>
                  <p className={styles.itemDesc}>{forum.description || '暂无说明'}</p>
                  <span className={styles.itemMeta}>
                    {formatCount(forum.threadCount)} 主题 · {formatCount(forum.postCount)} 回复
                    {forum.moderators ? ' · 版主 ' + forum.moderators : ''}
                  </span>
                </div>
                <div className={styles.itemActions}>
                  <Button
                    size="mini"
                    type="text"
                    onClick={() =>
                      setDraft({
                        id: forum.id,
                        categoryId: forum.categoryId,
                        name: forum.name,
                        description: forum.description,
                        moderators: forum.moderators,
                      })
                    }
                  >
                    编辑
                  </Button>
                  <Button size="mini" type="text" onClick={() => void moveForum(forum.id, 'up')}>
                    上移
                  </Button>
                  <Button size="mini" type="text" onClick={() => void moveForum(forum.id, 'down')}>
                    下移
                  </Button>
                  <Button size="mini" type="text" status="danger" onClick={() => void removeForum(forum.id, forum.name)}>
                    删除
                  </Button>
                </div>
              </li>
            ))}
            {category.forums.length === 0 ? <li className={styles.empty}>该分类下暂无版块。</li> : null}
          </ul>
        </section>
      ))}

      <Modal
        title={draft?.id ? '编辑版块' : '新建版块'}
        visible={Boolean(draft)}
        confirmLoading={busy}
        onCancel={() => setDraft(null)}
        onOk={() => void submitForum()}
        okText="保存"
        cancelText="取消"
      >
        {draft ? (
          <div className={styles.form}>
            <label className={styles.label} htmlFor="forum-name">
              名称
            </label>
            <Input id="forum-name" value={draft.name} onChange={(value) => setDraft({ ...draft, name: value })} />
            <label className={styles.label} htmlFor="forum-desc">
              说明
            </label>
            <Input.TextArea
              id="forum-desc"
              value={draft.description}
              onChange={(value) => setDraft({ ...draft, description: value })}
              rows={2}
            />
            <label className={styles.label} htmlFor="forum-cat">
              分类
            </label>
            <Select
              id="forum-cat"
              value={draft.categoryId}
              onChange={(value) => setDraft({ ...draft, categoryId: value as string })}
              style={{ width: '100%' }}
            >
              {categories.map((category) => (
                <Select.Option key={category.id} value={category.id}>
                  {category.name}
                </Select.Option>
              ))}
            </Select>
            <label className={styles.label} htmlFor="forum-mods">
              版主（用户名，逗号或空格分隔）
            </label>
            <Input
              id="forum-mods"
              value={draft.moderators}
              onChange={(value) => setDraft({ ...draft, moderators: value })}
            />
          </div>
        ) : null}
      </Modal>

      <Modal
        title={catId ? '重命名分类' : '新建分类'}
        visible={catOpen}
        confirmLoading={busy}
        onCancel={() => {
          setCatOpen(false);
          setCatId('');
          setCatName('');
        }}
        onOk={() => void submitCategory()}
        okText="保存"
        cancelText="取消"
      >
        <label className={styles.label} htmlFor="cat-name">
          分类名称
        </label>
        <Input id="cat-name" value={catName} onChange={setCatName} />
      </Modal>
    </div>
  );
}
