'use client';

import { useRef, useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Input, InputNumber, Switch } from '@arco-design/web-react';
import { browserGet, browserSend } from '@/lib/api/browser';
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

const FIELD_META: Record<string, { label: string; description: string; unit?: string; risk?: string }> = {
  siteName: { label: '站点名称', description: '显示在页头、邮件和公开页面。' },
  siteLogo: { label: '站点 Logo', description: '可填 Logo 地址；为空时使用部署默认 Logo。' },
  footerText: { label: '页脚文本', description: '显示在公共页面页脚。' },
  siteClosed: { label: '关闭站点', description: '关闭后普通用户只能访问必要的登录和管理入口。', risk: '高风险：将影响普通用户访问。' },
  siteClosedReason: { label: '关闭原因', description: '站点关闭时展示给访问者。' },
  registerEnabled: { label: '开放注册', description: '允许新用户创建账号。' },
  captchaEnabled: { label: '启用验证码', description: '在支持的认证流程中启用验证码。' },
  emailVerifyEnabled: { label: '要求邮箱验证', description: '需要 SMTP 已配置；保存后新注册用户需验证邮箱。', risk: '请确认邮件服务已配置并可投递。' },
  requireConsent: { label: '要求同意条款', description: '注册时要求用户同意条款和隐私政策。' },
  termsContent: { label: '服务条款', description: '注册同意框使用的正文。' },
  privacyContent: { label: '隐私政策', description: '注册同意框使用的隐私政策正文。' },
  threadsPerPage: { label: '主题每页数量', description: '列表页每页显示的主题数量。', unit: '条' },
  postsPerPage: { label: '回复每页数量', description: '主题页每页显示的回复数量。', unit: '条' },
  moderateEnabled: { label: '启用内容审核', description: '新主题和回复进入审核队列。' },
  uploadEnabled: { label: '允许上传', description: '允许用户上传图片和文件。', risk: '关闭后用户将无法上传新文件。' },
  maxImageMB: { label: '单张图片上限', description: '单个图片文件允许的最大大小。', unit: 'MB' },
  maxFileMB: { label: '单个文件上限', description: '单个非图片文件允许的最大大小。', unit: 'MB' },
  uploadMaxDiskGB: { label: '上传磁盘上限', description: '上传目录允许使用的磁盘空间上限。', unit: 'GB' },
  analyticsRetentionDays: { label: '报表保留天数', description: '快照清理任务保留的天数；0 表示关闭清理。', unit: '天' },
  reportTimeZone: { label: '报表时区', description: '使用有效 IANA 时区，例如 Asia/Shanghai。' },
};

function isLongText(name: string): boolean {
  return name === 'termsContent' || name === 'privacyContent';
}

export function SettingsForm({ settings, schema, status }: SettingsFormProps) {
  const router = useRouter();
  const [values, setValues] = useState<SiteSettingsAdmin>(settings);
  const initialValues = useRef<SiteSettingsAdmin>(settings);
  const [dirty, setDirty] = useState<Set<string>>(new Set());
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [conflict, setConflict] = useState(false);
  const [busy, setBusy] = useState(false);

  const fieldMap: Record<string, SettingField> = {};
  for (const field of schema.fields) fieldMap[field.name] = field;

  function setValue(name: string, value: unknown) {
    const previous = values[name];
    const risky = ['siteClosed', 'uploadEnabled', 'emailVerifyEnabled', 'requireConsent'].includes(name);
    if (risky && previous !== value && value === (name === 'siteClosed' || name === 'emailVerifyEnabled' || name === 'requireConsent')) {
      const meta = FIELD_META[name];
      if (!window.confirm(`${meta?.label ?? name}：${meta?.risk ?? '此操作会改变站点行为'}\n\n确定继续吗？`)) return;
    }
    setValues((prev) => ({ ...prev, [name]: value }));
    setDirty((prev) => {
      const next = new Set(prev);
      if (Object.is(initialValues.current[name], value)) next.delete(name);
      else next.add(name);
      return next;
    });
  }

  async function reload() {
    setError(null);
    setNotice(null);
    try {
      const [nextSettings, nextSchema, nextStatus] = await Promise.all([
        browserGet<SiteSettingsAdmin>('/admin/settings'),
        browserGet<SettingsSchema>('/admin/settings/schema'),
        browserGet<SettingsStatus>('/admin/settings/status'),
      ]);
      initialValues.current = nextSettings;
      setValues(nextSettings);
      setDirty(new Set());
      setConflict(false);
      // schema/status are server props; refresh updates them after the local reset.
      void nextSchema;
      void nextStatus;
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      setError(err.message);
    }
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
    if (dirty.size === 0) {
      setNotice('没有需要保存的修改。');
      return;
    }
    const body: Record<string, unknown> = { version: values.version };
    for (const name of dirty) body[name] = values[name];
    setBusy(true);
    try {
      const saved = await browserSend<SiteSettingsAdmin>('/admin/settings', { method: 'PATCH', body });
      initialValues.current = saved;
      setValues(saved);
      setDirty(new Set());
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
        if (err.fields) setErrors(err.fields);
        setError(err.message);
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
    const meta = FIELD_META[field.name] ?? { label: field.name, description: '' };
    const defaultValue = schema.defaults[field.name];
    if (field.type === 'boolean') {
      return (
        <div key={field.name} className={styles.switchRow}>
          <div>
            <label className={styles.switchLabel} htmlFor={id}>
              {meta.label}
            </label>
            <p className={styles.fieldDescription}>{meta.description}{meta.risk ? ` ${meta.risk}` : ''}</p>
            {field.name === 'emailVerifyEnabled' && Boolean(value) && !status.smtpEnabled ? <p className={styles.dependencyWarning}>当前 SMTP 未配置，后端不会启用邮箱验证。</p> : null}
            {field.name === 'requireConsent' && Boolean(value) && (!String(values.termsContent ?? '').trim() || !String(values.privacyContent ?? '').trim()) ? <p className={styles.dependencyWarning}>启用前请填写服务条款和隐私政策。</p> : null}
            <p className={styles.fieldHint}>默认：{String(defaultValue)} · 字段：{field.legacyName}</p>
          </div>
          <Switch id={id} checked={Boolean(value)} onChange={(checked) => setValue(field.name, checked)} />
        </div>
      );
    }
    return (
      <div key={field.name} className={styles.field}>
        <label className={styles.label} htmlFor={id}>
          {meta.label}
          <span className={styles.range}>
            {field.type === 'integer' ? `${field.min} - ${field.max}${meta.unit ? ` ${meta.unit}` : ''}` : `${field.min}-${field.max} 字符`}
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
        <p className={styles.fieldDescription}>{meta.description}{meta.risk ? ` ${meta.risk}` : ''}</p>
        <p className={styles.fieldHint}>默认：{String(defaultValue)} · 字段：{field.legacyName}</p>
        {errors[field.name] ? <p className={styles.fieldError}>{errors[field.name]}</p> : null}
      </div>
    );
  }

  return (
    <div className={styles.layout}>
      <nav className={styles.groupNav} aria-label="设置分组">
        {GROUPS.map((group, index) => group.fields.some((name) => fieldMap[name]) ? (
          <a key={group.title} href={`#settings-group-${index}`}>{group.title}</a>
        ) : null)}
      </nav>
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

      {GROUPS.map((group, index) => {
        const fields = group.fields.map((name) => fieldMap[name]).filter(Boolean);
        if (fields.length === 0) return null;
        return (
          <section key={group.title} id={`settings-group-${index}`} className={['panel', styles.groupCard].join(' ')}>
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
        <Button type="secondary" onClick={() => void reload()}>
          重新读取
        </Button>
        {dirty.size > 0 ? <span className={styles.dirtyHint}>有 {dirty.size} 项未保存</span> : null}
        <span className={styles.actionHint}>
          仅提交已修改字段并携带当前版本；冲突时保留输入。
        </span>
      </div>
    </form>
    </div>
  );
}
