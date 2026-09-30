import { redirect } from 'next/navigation';

// 邮件中的历史路径 /reset?token=... 指向重置页，保留兼容并带上令牌。
export default function ResetAliasPage({ searchParams }: { searchParams: { token?: string } }) {
  const token = searchParams.token ?? '';
  redirect(token ? '/password/reset?token=' + encodeURIComponent(token) : '/password/forgot');
}
