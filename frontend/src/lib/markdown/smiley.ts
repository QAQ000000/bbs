// 表情短代码：内置 Unicode 渲染为字符，图片包渲染为受控 /smiley 地址。
import type { SmileyGroup } from '../api/types';

export type SmileyValue = { unicode: string } | { url: string; alt: string };
export type SmileyMap = Record<string, SmileyValue>;

/** 把 /api/v1/smileys 的响应整理成 code -> 渲染值。 */
export function parseSmileyGroups(groups: SmileyGroup[] | null | undefined): SmileyMap {
  const map: SmileyMap = {};
  if (!Array.isArray(groups)) return map;
  for (const group of groups) {
    for (const code of group.Codes ?? []) {
      if (!code.code) continue;
      if (code.unicode) {
        map[code.code] = { unicode: code.unicode };
      } else if (code.pkg && code.file) {
        map[code.code] = { url: `/smiley/${code.pkg}/${code.file}`, alt: code.code };
      }
    }
  }
  return map;
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

const regexCache = new WeakMap<SmileyMap, RegExp | null>();

function smileyRegex(map: SmileyMap): RegExp | null {
  const cached = regexCache.get(map);
  if (cached !== undefined) return cached;
  const codes = Object.keys(map).sort((a, b) => b.length - a.length);
  const re = codes.length ? new RegExp(codes.map(escapeRegExp).join('|'), 'g') : null;
  regexCache.set(map, re);
  return re;
}

type MdNode =
  | { type: 'text'; value: string }
  | { type: 'image'; url: string; alt: string };

/** 将纯文本按已知短代码切片为 text / image 节点；未知代码保持原样。 */
export function splitSmileyText(text: string, map: SmileyMap): MdNode[] {
  const re = smileyRegex(map);
  if (!re || !text) return [{ type: 'text', value: text }];
  const nodes: MdNode[] = [];
  let last = 0;
  re.lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = re.exec(text)) !== null) {
    const value = map[match[0]];
    if (!value) continue;
    if (match.index > last) nodes.push({ type: 'text', value: text.slice(last, match.index) });
    if ('unicode' in value) {
      nodes.push({ type: 'text', value: value.unicode });
    } else {
      nodes.push({ type: 'image', url: value.url, alt: value.alt });
    }
    last = match.index + match[0].length;
    if (match[0].length === 0) re.lastIndex += 1;
  }
  if (nodes.length === 0) return [{ type: 'text', value: text }];
  if (last < text.length) nodes.push({ type: 'text', value: text.slice(last) });
  return nodes;
}
