'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Input, Modal, Select } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { TagView } from '@/lib/api/types';
import { formatCount } from '@/lib/format';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './TagAdmin.module.css';

interface Draft {
  id: string;
  name: string;
  slug: string;
  description: string;
  color: string;
  status: string;
  version: number;
}

const EMPTY: Draft = { id: '', name: '', slug: '', description: '', color: '', status: 'active', version: 1 };

export function TagAdmin({ tags, query }: { tags: TagView[]; query: string }) {
  const router = useRouter();
  const [draft, setDraft] = useState<Draft | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [conflict, setConflict] = useState(false);
  const [keyword, setKeyword] = useState(query);

  async function save() {
    if (!draft) return;
    if (!draft.name.trim() || !draft.slug.trim()) {
      setError('名称与 slug 必填');
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const body = {
        name: draft.name.trim(),
        slug: draft.slug.trim(),
        description: draft.description,
        color: draft.color.trim(),
        status: draft.status,
        version: draft.version,
      };
      if (draft.id) {
        await browserSend('/admin/tags/' + draft.id, { method: 'PUT', body });
      } else {
        await browserSend('/admin/tags', { method: 'POST', body });
      }
      toastSuccess(draft.id ? '标签已保存' : '标签已创建');
      setDraft(null);
      setConflict(false);
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      // 版本冲突保留输入，提示重新读取后再合并。
      if (err.status === 409) {
        setConflict(true);
        setError('标签已被他人修改（版本冲突）。你的输入仍然保留，请重新读取最新版本后再保存。');
      } else {
        setError(err.message);
      }
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  async function toggleStatus(tag: TagView) {
    setBusy(true);
    try {
      await browserSend('/admin/tags/' + tag.id, {
        method: 'PUT',
        body: {
          name: tag.name,
          slug: tag.slug,
          description: tag.description,
          color: tag.color,
          status: tag.status === 'active' ? 'disabled' : 'active',
          version: tag.version ?? 1,
        },
      });
      toastSuccess(tag.status === 'active' ? '标签已禁用' : '标签已启用');
      router.refresh();
    } catch (caught) {
      toastError(caught as ApiError);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <form className={styles.toolbar} action="/admin/tags" method="get" role="search">
        <input className={styles.search} type="search" name="q" defaultValue={query} placeholder="按名称或 slug 搜索" aria-label="搜索标签" />
        <Button size="small" type="secondary" htmlType="submit">
          搜索
        </Button>
        <Button size="small" type="primary" onClick={() => { setDraft({ ...EMPTY }); setError(null); setConflict(false); }}>
          新建标签
        </Button>
      </form>

      {tags.length > 0 ? (
        <div className={['panel', styles.tableWrap].join(' ')}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>名称</th>
                <th>Slug</th>
                <th>状态</th>
                <th>主题数</th>
                <th>版本</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {tags.map((tag) => (
                <tr key={tag.id}>
                  <td>
                    <span className={styles.colorDot} style={tag.color ? { background: tag.color } : undefined} aria-hidden="true" />
                    <span className={styles.name}>{tag.name}</span>
                    {tag.description ? <p className={styles.desc}>{tag.description}</p> : null}
                  </td>
                  <td className={styles.mono}>{tag.slug}</td>
                  <td>
                    <span className={tag.status === 'active' ? styles.active : styles.disabled}>
                      {tag.status === 'active' ? '启用' : '禁用'}
                    </span>
                  </td>
                  <td>{formatCount(tag.threadCount)}</td>
                  <td>v{tag.version ?? 1}</td>
                  <td className={styles.actions}>
                    <Button size="mini" type="text" onClick={() => { setDraft({ id: tag.id, name: tag.name, slug: tag.slug, description: tag.description, color: tag.color, status: tag.status, version: tag.version ?? 1 }); setError(null); setConflict(false); }}>
                      编辑
                    </Button>
                    <Button size="mini" type="text" loading={busy} onClick={() => void toggleStatus(tag)}>
                      {tag.status === 'active' ? '禁用' : '启用'}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="panel" style={{ padding: 24, color: 'var(--color-text-tertiary)' }}>
          没有匹配的标签。
        </div>
      )}

      <Modal
        title={draft?.id ? '编辑标签' : '新建标签'}
        visible={Boolean(draft)}
        confirmLoading={busy}
        onCancel={() => setDraft(null)}
        onOk={() => void save()}
        okText="保存"
        cancelText="取消"
      >
        {draft ? (
          <div className={styles.form}>
            {conflict ? (
              <div className={styles.conflict}>
                <p className={styles.conflictText}>{error}</p>
                <Button size="mini" type="secondary" onClick={() => router.refresh()}>
                  重新读取列表
                </Button>
              </div>
            ) : error ? (
              <p className={styles.error} role="alert">
                {error}
              </p>
            ) : null}
            <label className={styles.label} htmlFor="tag-name">
              名称（1-32 字符，会转小写并合并空白）
            </label>
            <Input id="tag-name" value={draft.name} onChange={(v) => setDraft({ ...draft, name: v })} maxLength={32} />
            <label className={styles.label} htmlFor="tag-slug">
              Slug（小写字母数字连字符）
            </label>
            <Input id="tag-slug" value={draft.slug} onChange={(v) => setDraft({ ...draft, slug: v })} maxLength={64} />
            <label className={styles.label} htmlFor="tag-desc">
              说明（最多 500 字，留空将清空）
            </label>
            <Input.TextArea id="tag-desc" value={draft.description} onChange={(v) => setDraft({ ...draft, description: v })} maxLength={500} rows={2} />
            <label className={styles.label} htmlFor="tag-color">
              颜色（#RRGGBB，留空清除）
            </label>
            <Input id="tag-color" value={draft.color} onChange={(v) => setDraft({ ...draft, color: v })} placeholder="#165DFF" />
            <label className={styles.label} htmlFor="tag-status">
              状态
            </label>
            <Select id="tag-status" value={draft.status} onChange={(v) => setDraft({ ...draft, status: v as string })} style={{ width: 160 }}>
              <Select.Option value="active">启用</Select.Option>
              <Select.Option value="disabled">禁用</Select.Option>
            </Select>
            {draft.id ? <p className={styles.hint}>当前版本 v{draft.version}。保存需携带版本，冲突时先重新读取。</p> : null}
          </div>
        ) : null}
      </Modal>
    </div>
  );
}
