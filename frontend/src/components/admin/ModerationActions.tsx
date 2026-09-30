'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Input, Modal } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './ModerationActions.module.css';

export type ModerationKind = 'thread' | 'post' | 'report';

export function ModerationActions({ kind, id }: { kind: ModerationKind; id: string }) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [noteOpen, setNoteOpen] = useState(false);
  const [note, setNote] = useState('');
  const [confirmDelete, setConfirmDelete] = useState(false);

  function endpoint(): string {
    if (kind === 'thread') return '/admin/moderate/thread';
    if (kind === 'post') return '/admin/moderate/post';
    return '/admin/report/handle';
  }

  function payload(op: string, withNote: boolean): Record<string, unknown> {
    if (kind === 'report') return { id, op };
    const key = kind === 'thread' ? 'tid' : 'pid';
    const body: Record<string, unknown> = { [key]: id, op };
    if (withNote) body.note = note;
    return body;
  }

  async function run(op: string, withNote: boolean) {
    setBusy(true);
    try {
      await browserSend(endpoint(), { method: 'POST', body: payload(op, withNote) });
      toastSuccess('操作已完成');
      setNoteOpen(false);
      setConfirmDelete(false);
      setNote('');
      router.refresh();
    } catch (caught) {
      toastError(caught as ApiError);
    } finally {
      setBusy(false);
    }
  }

  if (kind === 'report') {
    return (
      <div className={styles.actions}>
        <Button size="mini" type="primary" loading={busy} onClick={() => void run('delete', false)}>
          删除内容
        </Button>
        <Button size="mini" type="secondary" loading={busy} onClick={() => void run('dismiss', false)}>
          驳回
        </Button>
      </div>
    );
  }

  return (
    <div className={styles.actions}>
      <Button size="mini" type="primary" loading={busy} onClick={() => void run('approve', false)}>
        通过
      </Button>
      <Button size="mini" type="secondary" onClick={() => setNoteOpen(true)}>
        删除
      </Button>
      <Modal
        title="确认删除该内容"
        visible={noteOpen && !confirmDelete}
        onCancel={() => setNoteOpen(false)}
        onOk={() => setConfirmDelete(true)}
        okText="继续"
        cancelText="取消"
      >
        <p className={styles.modalHint}>可填写审核备注（最多 500 字，将记录到审计日志）。</p>
        <Input.TextArea value={note} onChange={setNote} maxLength={500} rows={3} placeholder="审核备注（可选）" />
      </Modal>
      <Modal
        title="再次确认"
        visible={confirmDelete}
        confirmLoading={busy}
        onCancel={() => setConfirmDelete(false)}
        onOk={() => void run('delete', true)}
        okText="确认删除"
        cancelText="取消"
      >
        <p className={styles.modalHint}>删除后内容将公开不可见，操作会记录到审计日志，且不可自动重试撤销。</p>
      </Modal>
    </div>
  );
}
