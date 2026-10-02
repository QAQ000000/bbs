import type { Metadata } from 'next';
import Link from 'next/link';
import { redirect } from 'next/navigation';
import { serverGet } from '@/lib/api/server';
import { SetupForm } from '@/components/auth/SetupForm';
import type { SetupState } from '@/lib/api/types';
import styles from '../auth.module.css';

export const metadata: Metadata = {
  title: '安装向导',
  description: 'GoBBS 首次安装：设置站点信息与初始管理员。',
  robots: { index: false, follow: false },
};

export default async function SetupPage() {
  let state: SetupState | null = null;
  try {
    state = await serverGet<SetupState>('/api/v1/setup', { forwardCookies: false });
  } catch {
    state = null;
  }

  // 已初始化：不能再次进入安装流程。
  if (state?.required === false) redirect('/login');

  if (!state) {
    return (
      <div>
        <h1 className={styles.title}>无法读取安装状态</h1>
        <p className={styles.subtitle}>后端服务暂时不可用，无法确认站点是否已初始化。请稍后重试。</p>
        <p className={styles.subtitle}>
          <Link href="/setup">重新检查</Link>
        </p>
      </div>
    );
  }

  return <SetupForm initialState={state} />;
}
