import { redirect } from 'next/navigation';

// CONTENT-02：回复在主题页内联完成，独立路由统一跳转到回复输入区，避免重复实现。
export default function ReplyPage({ params }: { params: { tid: string } }) {
  redirect('/threads/' + params.tid + '#reply');
}
