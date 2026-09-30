'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Input, InputNumber, Switch } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { SettingField, SettingsSchema, SettingsStatus, SiteSettingsAdmin } from '@/lib/api/types';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './SettingsForm.module.css';

export interface SettingsFormProps {
  settings: SiteSettingsAdmin;
  schema: SettingsSchema;
  status: SettingsStatus;
}

// 分组只用于页面呈现；字段、类型与边界全部来自后端 schema。
const GROUPS: { title: string; hint: string; fields: string[] }[] = [
  {
    title: '站点信息',
    hint: '站点名称、标识、页脚与关站设置。',
    fields: ['siteName', 'siteLogo', 'footerText', 'siteClosed', 'siteClosedReason'],
  },
  {
    title: '注册与合规',
    hint: '注册开关、验证码、邮箱验证与条款内容。',
    fields: ['registerEnabled', 'captchaEnabled', 'emailVerifyEnabled', 'requireConsent', 'termsContent', 'privacyContent'],
  },
  {
    title: '内容与审核',
    hint: '分页数量与发帖审核。',
    fields: ['threadsPerPage', 'postsPerPage', 'moderateEnabled'],
  },
  {
    title: '上传',
    hint: '上传开关、单文件上限与磁盘占用上限。',
    fields: ['uploadEnabled', 'maxImageMB', 'maxFileMB', 'uploadMaxDiskGB'],
  },
  {
    title: '报表与保留',
    hint: '统计保留天数与报表时区。',
    fields: ['analyticsRetentionDays', 'reportTimeZone'],
  },
];

function isLongText(name: string): boolean {
  return name === 'termsContent' || name === 'privacyContent';
}

export function SettingsForm({ settings, schema, status }: SettingsFormProps) {
  const router = useRouter();
  const [values, setValues] = useState<SiteSettingsAdmin>(settings);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [conflict, setConflict] = useState(false);
  const [busy, setBusy] = useState(false);

  const fieldMap: Record<string, SettingField> = {};
  for (const field of schema.fields) fieldMap[field.name] = field;

  function setValue(name: string, value: unknown) {
    setValues((prev) => ({ ...prev, [name]: value }));
  }

  function validate(): boolean {
    const next: Record<string, string> = {};
    for (const field of schema.fields) {
      const value = values[field.name];
      if (field.type === 'boolean') continue;
      if (field.type === 'integer') {
        const n = Number(value);
        if (!Number.isFinite(n) || n < field.min || n > field.max) {
          next[field.name] = '必须为 ' + field.min + ' 到 ' + field.max + ' 的整数';
        }
        continue;
      }
      const text = String(value ?? '');
      if (text.length < field.min || text.length > field.max) {
        next[field.name] = '长度必须为 ' + field.min + ' 到 ' + field.max + ' 个字符';
      }
    }
    if (values.requireConsent && (!String(values.termsContent).trim() || !String(values.privacyContent).trim())) {
      next.requireConsent = '要求同意条款时，条款和隐私政策均不能为空';
    }
    if (values.emailVerifyEnabled && !status.smtpEnabled) {
      next.emailVerifyEnabled = '启用邮箱验证前必须配置邮件服务（SMTP）';
    }
    setErrors(next);
    return Object.keys(next).length === 0;
  }

  async function save(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    setNotice(null);
    if (conflict) {
      setError('配置版本已变化，请先重新读取后再保存。');
      return;
    }
    if (!validate()) {
      setError('请先修正标记的字段。');
      return;
    }
    const body: Record<string, unknown> = { version: values.version };
    for (const field of schema.fields) body[field.name] = values[field.name];
    setBusy(true);
    try {
      await browserSend('/admin/settings', { method: 'PUT', body });
      toastSuccess('配置已保存');
      setNotice('配置已保存为新版本，站点设置即时生效。');
      setErrors({});
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      if (err.status === 409 || err.code === 'SETTINGS_CONFLICT') {
        setConflict(true);
        setError('配置已被他人修改（版本冲突）。你输入的内容仍然保留，请重新读取后再保存。');
      } else if (err.status === 422) {
        // 后端返回 "字段: 说明"，按字段展示。
        const message = err.message || '';
        const idx = message.indexOf(': ');
        if (idx > 0) {
          const field = message.slice(0, idx);
          const text = message.slice(idx + 2);
          setErrors({ [field]: text });
          setError('部分字段未通过校验，请修正后重试。');
        } else {
          setError(message);
        }
      } else {
        setError(err.message);
      }
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  function renderField(field: SettingField) {
    const id = 'setting-' + field.name;
    const value = values[field.name];
    if (field.type === 'boolean') {
      return (
        <div key={field.name} className={styles.switchRow}>
          <div>
            <label className={styles.switchLabel} htmlFor={id}>
              {field.name}
            </label>
            <p className={styles.fieldHint}>{field.legacyName}</p>
          </div>
          <Switch id={id} checked={Boolean(value)} onChange={(checked) => setValue(field.name, checked)} />
        </div>
      );
    }
    return (
      <div key={field.name} className={styles.field}>
        <label className={styles.label} htmlFor={id}>
          {field.name}
          <span className={styles.range}>
            {field.type === 'integer' ? field.min + ' - ' + field.max : field.min + '-' + field.max + ' 字符'}
          </span>
        </label>
        {isLongText(field.name) ? (
          <Input.TextArea
            id={id}
            value={String(value ?? '')}
            onChange={(v) => setValue(field.name, v)}
            rows={6}
            maxLength={field.max}
            showWordLimit
          />
        ) : field.name === 'siteClosedReason' || field.name === 'footerText' ? (
          <Input.TextArea id={id} value={String(value ?? '')} onChange={(v) => setValue(field.name, v)} rows={2} maxLength={field.max} />
        ) : field.type === 'integer' ? (
          <InputNumber id={id} min={field.min} max={field.max} value={Number(value)} onChange={(v) => setValue(field.name, Number(v))} />
        ) : (
          <Input id={id} value={String(value ?? '')} onChange={(v) => setValue(field.name, v)} maxLength={field.max} />
        )}
        <p className={styles.fieldHint}>{field.legacyName}</p>
        {errors[field.name] ? <p className={styles.fieldError}>{errors[field.name]}</p> : null}
      </div>
    );
  }

  return (
    <form className={styles.form} onSubmit={save}>
      <section className={['panel', styles.statusCard].join(' ')}>
        <div className={styles.statusRow}>
          <span className={styles.statusLabel}>当前版本</span>
          <span className={styles.statusValue}>v{values.version}</span>
        </div>
        <div className={styles.statusRow}>
          <span className={styles.statusLabel}>配置有效性</span>
          <span className={status.valid ? styles.ok : styles.bad}>{status.valid ? '有效' : '存在问题'}</span>
        </div>
        <div className={styles.statusRow}>
          <span className={styles.statusLabel}>邮件服务</span>
          <span className={status.smtpEnabled ? styles.ok : styles.warn}>{status.smtpEnabled ? '已配置' : '未配置（SMTP 由部署环境变量提供）'}</span>
        </div>
        <div className={styles.statusRow}>
          <span className={styles.statusLabel}>重启要求</span>
          <span className={styles.statusValue}>{schema.requiresRestart ? '需要重启' : '无需重启，保存即时生效'}</span>
        </div>
        <p className={styles.statusHint}>
          密钥类配置（SMTP 账号密码、邮件密钥等）只来自部署环境变量，不由本接口返回或保存；本页字段不含敏感密钥。
        </p>
        {status.issues.length > 0 ? (
          <ul className={styles.issues}>
            {status.issues.map((issue) => (
              <li key={issue.field}>
                {issue.field}：{issue.message}
              </li>
            ))}
          </ul>
        ) : null}
      </section>

      {GROUPS.map((group) => {
        const fields = group.fields.map((name) => fieldMap[name]).filter(Boolean);
        if (fields.length === 0) return null;
        return (
          <section key={group.title} className={['panel', styles.groupCard].join(' ')}>
            <header className={styles.groupHead}>
              <h2 className={styles.groupTitle}>{group.title}</h2>
              <span className={styles.groupHint}>{group.hint}</span>
            </header>
            <div className={styles.fields}>{fields.map((field) => renderField(field))}</div>
          </section>
        );
      })}

      {notice ? <p className={styles.notice}>{notice}</p> : null}
      {error ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : null}
      <div className={styles.actions}>
        <Button type="primary" htmlType="submit" loading={busy} disabled={conflict} title={conflict ? '请先重新读取配置' : undefined}>
          保存配置
        </Button>
        <Button type="secondary" onClick={() => router.refresh()}>
          重新读取
        </Button>
        <span className={styles.actionHint}>
          保存会整体提交全部字段并携带当前版本；冲突时保留输入。
        </span>
      </div>
    </form>
  );
}
