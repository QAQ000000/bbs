import Link from 'next/link';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeSanitize, { defaultSchema } from 'rehype-sanitize';
import { remarkSmiley } from './remarkSmiley';
import type { SmileyMap } from './smiley';
import styles from './markdown.module.css';

// 初版禁用原始 HTML；仅允许 GFM 表格 / 任务列表 / 代码块，危险协议由 sanitize 拦截。
const schema = {
  ...defaultSchema,
  attributes: {
    ...defaultSchema.attributes,
    code: [...(defaultSchema.attributes?.code ?? []), 'className'],
    span: [...(defaultSchema.attributes?.span ?? []), 'className'],
    a: [...(defaultSchema.attributes?.a ?? []), 'rel'],
  },
};

export interface MarkdownProps {
  content: string;
  smileys?: SmileyMap;
  className?: string;
}

export default function Markdown({ content, smileys = {}, className }: MarkdownProps) {
  const isInternal = (href?: string) => Boolean(href && (href.startsWith('/') || href.startsWith('#')));
  return (
    <div className={[styles.markdown, className].filter(Boolean).join(' ')}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm, [remarkSmiley, smileys]]}
        rehypePlugins={[[rehypeSanitize, schema]]}
        components={{
          a({ href, children, node, ...props }) {
            void node;
            if (isInternal(href)) {
              return (
                <Link href={href as string} {...props}>
                  {children}
                </Link>
              );
            }
            return (
              <a href={href} target="_blank" rel="noopener noreferrer nofollow" {...props}>
                {children}
              </a>
            );
          },
          img({ node, alt, ...props }) {
            void node;
            // 受控媒体地址直接使用后端返回结果，不做 URL 拼接。
            // eslint-disable-next-line @next/next/no-img-element
            return <img {...props} alt={alt ?? ''} loading="lazy" />;
          },
          table({ children }) {
            return (
              <div className={styles.tableWrap}>
                <table>{children}</table>
              </div>
            );
          },
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}
