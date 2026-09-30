'use client';

import { useRef, useState } from 'react';
import { Message } from '@arco-design/web-react';
import {
  IconBold,
  IconCode,
  IconImage,
  IconItalic,
  IconLink,
  IconQuote,
} from '@arco-design/web-react/icon';
import Markdown from '@/lib/markdown/Markdown';
import type { SmileyMap } from '@/lib/markdown/smiley';
import { browserSend } from '@/lib/api/browser';
import { toastError } from '../ui/feedback';
import styles from './MarkdownEditor.module.css';

export interface MarkdownEditorProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  minRows?: number;
  smileys?: SmileyMap;
  uploadEnabled?: boolean;
  ariaLabel?: string;
  footer?: React.ReactNode;
}

interface UploadResult {
  url: string;
  name: string;
  kind: string;
  mime: string;
}

export function MarkdownEditor({
  value,
  onChange,
  placeholder = '支持 Markdown 语法…',
  minRows = 6,
  smileys = {},
  uploadEnabled = false,
  ariaLabel = '正文编辑器',
  footer,
}: MarkdownEditorProps) {
  const ref = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const [mode, setMode] = useState<'edit' | 'preview'>('edit');
  const [uploading, setUploading] = useState(false);

  function surround(prefix: string, suffix: string, placeholderText: string) {
    const ta = ref.current;
    if (!ta) {
      onChange(value + prefix + placeholderText + suffix);
      return;
    }
    const start = ta.selectionStart;
    const end = ta.selectionEnd;
    const selected = value.slice(start, end) || placeholderText;
    const next = value.slice(0, start) + prefix + selected + suffix + value.slice(end);
    onChange(next);
    requestAnimationFrame(() => {
      ta.focus();
      const pos = start + prefix.length + selected.length + suffix.length;
      ta.setSelectionRange(pos, pos);
    });
  }

  function prefixLines(prefix: string) {
    const ta = ref.current;
    if (!ta) {
      onChange(value + '\n' + prefix);
      return;
    }
    const start = ta.selectionStart;
    const end = ta.selectionEnd;
    const segment = value.slice(start, end) || '';
    const transformed = (segment || prefix.trim())
      .split('\n')
      .map((line) => prefix + line)
      .join('\n');
    const next = value.slice(0, start) + transformed + value.slice(end);
    onChange(next);
    requestAnimationFrame(() => {
      ta.focus();
      ta.setSelectionRange(start + transformed.length, start + transformed.length);
    });
  }

  async function handleFile(file: File) {
    setUploading(true);
    try {
      const form = new FormData();
      form.append('kind', 'image');
      form.append('file', file);
      const result = await browserSend<UploadResult>('/uploads', { method: 'POST', form });
      const snippet = '![' + (result.name || '图片') + '](' + result.url + ')';
      const ta = ref.current;
      const start = ta ? ta.selectionStart : value.length;
      const next = value.slice(0, start) + '\n' + snippet + '\n' + value.slice(start);
      onChange(next);
      Message.success('图片已上传');
    } catch (error) {
      toastError(error);
    } finally {
      setUploading(false);
      if (fileRef.current) fileRef.current.value = '';
    }
  }

  return (
    <div className={styles.editor}>
      <div className={styles.toolbar}>
        <div className={styles.tools}>
          <button type="button" className={styles.tool} title="加粗" aria-label="加粗" onClick={() => surround('**', '**', '粗体')}>
            <IconBold />
          </button>
          <button type="button" className={styles.tool} title="斜体" aria-label="斜体" onClick={() => surround('*', '*', '斜体')}>
            <IconItalic />
          </button>
          <button type="button" className={styles.tool} title="引用" aria-label="引用" onClick={() => prefixLines('> ')}>
            <IconQuote />
          </button>
          <button type="button" className={styles.tool} title="代码" aria-label="代码" onClick={() => surround('`', '`', 'code')}>
            <IconCode />
          </button>
          <button type="button" className={styles.tool} title="链接" aria-label="链接" onClick={() => surround('[', '](https://)', '链接文字')}>
            <IconLink />
          </button>
          {uploadEnabled ? (
            <button
              type="button"
              className={styles.tool}
              title="上传图片"
              aria-label="上传图片"
              onClick={() => fileRef.current?.click()}
              disabled={uploading}
            >
              <IconImage />
            </button>
          ) : null}
          <input
            ref={fileRef}
            type="file"
            accept="image/png,image/jpeg,image/gif,image/webp"
            className="sr-only"
            onChange={(event) => {
              const file = event.target.files?.[0];
              if (file) void handleFile(file);
            }}
          />
        </div>
        <div className={styles.modes}>
          <button
            type="button"
            className={mode === 'edit' ? styles.modeActive : styles.mode}
            onClick={() => setMode('edit')}
          >
            编写
          </button>
          <button
            type="button"
            className={mode === 'preview' ? styles.modeActive : styles.mode}
            onClick={() => setMode('preview')}
          >
            预览
          </button>
        </div>
      </div>

      {mode === 'edit' ? (
        <textarea
          ref={ref}
          className={styles.textarea}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          placeholder={placeholder}
          rows={minRows}
          aria-label={ariaLabel}
        />
      ) : (
        <div className={styles.preview}>
          {value.trim() ? (
            <Markdown content={value} smileys={smileys} />
          ) : (
            <p className={styles.previewEmpty}>没有可预览的内容。</p>
          )}
        </div>
      )}

      <div className={styles.footer}>
        <span className={styles.counter}>{value.length} / 30000</span>
        {footer}
      </div>
    </div>
  );
}
