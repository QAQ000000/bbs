#!/bin/bash
# 发布前自检：确认仓库中没有任何与第三方论坛（如 Discuz）逐字节一致的文件。
# 用法: scripts/check-clean.sh <参考的第三方论坛程序目录>
# 退出码 0 = 干净；1 = 发现一致文件（禁止发布）。
set -u
REF="${1:?用法: $0 <第三方论坛程序目录>}"
cd "$(dirname "$0")/.."
found=0
while IFS= read -r f; do
    rel="${f#./}"
    # 与参考目录任意相对路径文件做一致性比对
    if [ -f "$REF/$rel" ] && cmp -s "$f" "$REF/$rel"; then
        echo "发现与参考项目一致的文件: $rel"
        found=1
    fi
done < <(find assets internal cmd templates -type f 2>/dev/null)
[ "$found" = 0 ] && echo "✓ 自检通过：仓库中未发现与参考项目一致的文件"
exit $found
