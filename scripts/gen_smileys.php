<?php
/**
 * 表情代码映射导出工具（可选辅助）。
 *
 * 在你自己的 经典论坛程序 站点环境运行，把该站点数据库中的表情代码映射
 * （common_smiley / forum_imagetype 表）导出为本站导入工具可识别的 JSON，
 * 配合：forumd -import-smileys <你的Discuz表情目录> -codes codes.json
 *
 * 用法：php gen_smileys.php <你的Discuz安装根目录> > codes.json
 * 注意：导出的映射 JSON 与表情图片均来自你自己的站点数据，请自行确认
 * 你有权在其许可范围内使用并迁移这些素材；本程序仓库不包含任何此类资源。
 */

$root = rtrim($argv[1] ?? '', '/');
if ($root === '' || !is_dir($root)) {
    fwrite(STDERR, "用法: php gen_smileys.php <Discuz安装根目录>\n");
    exit(1);
}
$base = $root . '/source/i18n/EN/install/lang_sql_install/';

include $base . 'table_forum_imagetype.php';
$types = [];
foreach ($data as $r) {
    $types[$r['typeid']] = ['name' => $r['name'], 'dir' => $r['directory']];
}
unset($data);
include $base . 'table_common_smiley.php';

$out = ['codes' => []];
foreach ($data as $r) {
    if (($r['type'] ?? '') !== 'smiley') continue;
    $dir = $types[$r['typeid']]['dir'] ?? '';
    if (!$dir) continue;
    $f = $root . '/static/image/smiley/' . $dir . '/' . $r['url'];
    if (!file_exists($f)) continue;
    $out['codes'][] = ['code' => $r['code'], 'pkg' => $dir, 'file' => $r['url']];
}
echo json_encode($out, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_PRETTY_PRINT);
