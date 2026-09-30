'use client';

import { useState } from 'react';
import { Button, Input, Modal } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './ReportButton.module.css';

export interface ReportButtonProps {
  postId: string;
}

// 举报理由允许为空；提交成功不代表已经处理。
export function ReportButton({ postId }: ReportButtonProps) {
  const [visible, setVisible] = useState(false);
  const [reason, setReason] = useState('');
  const [submitting, setSubmitting] = useState(false);

  async function submit() {
    setSubmitting(true);
    try {
      await browserSend('/posts/' + postId + '/reports', { method: 'POST', body: { reason } });
      toastSuccess('举报已提交，我们会尽快处理');
      setVisible(false);
      setReason('');
    } catch (error) {
      toastError(error);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <>
      <button type="button" className={styles.trigger} onClick={() => setVisible(true)}>
        举报
      </button>
      <Modal
        title="举报该内容"
        visible={visible}
        confirmLoading={submitting}
        onOk={() => void submit()}
        onCancel={() => setVisible(false)}
        okText="提交举报"
        cancelText="取消"
      >
        <p style={{ margin: '0 0 8px', color: 'var(--color-text-tertiary)', fontSize: 13 }}>
          请简述问题（可选，最多 200 字）。提交后由管理员或版主处理。
        </p>
        <Input.TextArea
          value={reason}
          onChange={setReason}
          maxLength={200}
          rows={3}
          placeholder="例如：广告引流、无关内容、人身攻击…"
        />
      </Modal>
    </>
  );
}
