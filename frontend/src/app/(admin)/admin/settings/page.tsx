import type { Metadata } from 'next';
import Link from 'next/link';
import { adminGet } from '@/lib/admin.server';
import type { SettingsSchema, SettingsStatus, SiteSettingsAdmin } from '@/lib/api/types';
import { SettingsForm } from '@/components/admin/SettingsForm';
import { ErrorState } from '@/components/ui/StateView';
import styles from './settings-admin.module.css';

export const metadata: Metadata = {
  title: '站点设置',
  robots: { index: false, follow: false },
};

export default async function AdminSettingsPage() {
  const [settings, schema, status] = await Promise.all([
    adminGet<SiteSettingsAdmin>('/api/v1/admin/settings'),
    adminGet<SettingsSchema>('/api/v1/admin/settings/schema'),
    adminGet<SettingsStatus>('/api/v1/admin/settings/status'),
  ]);

  if (!settings.ok || !schema.ok || !status.ok) {
    return (
      <div className={styles.forbidden}>
        <h1>无法读取站点设置</h1>
        <p>需要管理员权限与 settings.configure；配置读取失败时不会回退为默认值。</p>
        <Link href="/admin">返回概览</Link>
      </div>
    );
  }

  return (
    <div>
      <div className={styles.pageHead}>
        <h1 className={styles.pageTitle}>站点设置</h1>
        <span className={styles.pageMeta}>
          字段、类型与边界来自后端 schema；保存只提交已修改字段并携带版本，冲突时保留输入。
        </span>
      </div>
      {settings.data && schema.data && status.data ? (
        <SettingsForm settings={settings.data} schema={schema.data} status={status.data} />
      ) : (
        <div className="panel">
          <ErrorState title="配置不可用" description="请通过配置状态接口诊断后重试。" retryHref="/admin/settings" />
        </div>
      )}
    </div>
  );
}
