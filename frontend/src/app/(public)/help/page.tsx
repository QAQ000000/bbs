import type { Metadata } from 'next';
import Link from 'next/link';
import { Breadcrumb } from '@/components/ui/Breadcrumb';

export const metadata: Metadata = {
  title: '帮助中心',
  description: 'GoBBS 社区使用帮助：发帖、回复、通知与账号安全。',
  alternates: { canonical: '/help' },
};

const HELP = [
  {
    q: '如何发布主题？',
    a: '登录后点击顶部「发布主题」，选择版块、填写标题与正文。正文支持 Markdown，可在发布前预览。',
  },
  {
    q: '回复为什么显示待审核？',
    a: '当站点开启发帖审核，或新用户内容包含链接时，内容会先进入审核队列，通过后公开显示。',
  },
  {
    q: '通知和订阅有什么区别？',
    a: '订阅关注某个版块或主题的新内容；通知是订阅产生的事件投递。新订阅默认开启通知，可单独关闭或静音。',
  },
  {
    q: '私信为什么发不出去？',
    a: '私信无需互相关注。但发起方在对方回复前只能发送首条消息，这是防止骚扰的限制，对方回复后即可继续。',
  },
  {
    q: '忘记密码怎么办？',
    a: '在登录页点击「忘记密码」，通过注册邮箱获取重置链接。',
  },
];

export default function HelpPage() {
  return (
    <div className="container page">
      <Breadcrumb items={[{ label: '首页', href: '/' }, { label: '帮助中心' }]} />
      <h1 style={{ margin: '0 0 6px', fontSize: 22 }}>帮助中心</h1>
      <p style={{ margin: '0 0 20px', color: 'var(--color-text-tertiary)', fontSize: 13 }}>
        常见问题与使用说明。仍有疑问可在「意见反馈」版块发帖。
      </p>
      <div className="panel" style={{ padding: 24, display: 'flex', flexDirection: 'column', gap: 20 }}>
        {HELP.map((item) => (
          <div key={item.q}>
            <h2 style={{ margin: '0 0 6px', fontSize: 15 }}>{item.q}</h2>
            <p style={{ margin: 0, color: 'var(--color-text-secondary)', fontSize: 14, lineHeight: 1.8 }}>{item.a}</p>
          </div>
        ))}
        <p style={{ margin: 0, fontSize: 13 }}>
          <Link href="/forums" style={{ color: 'var(--color-primary)' }}>
            前往版块浏览
          </Link>
        </p>
      </div>
    </div>
  );
}
