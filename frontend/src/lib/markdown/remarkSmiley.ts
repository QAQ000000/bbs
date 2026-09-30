import { splitSmileyText, type SmileyMap } from './smiley';

interface Node {
  type: string;
  value?: string;
  children?: Node[];
}

/** remark 插件：把正文中的表情短代码替换为 Unicode 或图片节点。 */
export function remarkSmiley(map: SmileyMap) {
  return (tree: Node) => {
    if (!map || Object.keys(map).length === 0) return;
    walk(tree, map);
  };
}

function walk(node: Node, map: SmileyMap): void {
  if (!node || !Array.isArray(node.children)) return;
  const next: Node[] = [];
  for (const child of node.children) {
    if (child.type === 'text' && typeof child.value === 'string') {
      next.push(...(splitSmileyText(child.value, map) as Node[]));
    } else {
      walk(child, map);
      next.push(child);
    }
  }
  node.children = next;
}
