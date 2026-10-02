'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import Link from 'next/link';
import { Button, Checkbox } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import { ADMIN_ROLES } from '@/lib/api/types';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './PermissionsAdmin.module.css';

const HARD_PROTECTED = ['admin.panel', 'permissions.edit'];

const POINT_LABELS: Record<string, string> = {
  'admin.panel': '进入管理后台',
  'permissions.edit': '编辑权限矩阵',
  'users.view': '查看用户',
  'user.ban': '禁言 / 解禁',
  'user.group': '调整用户组',
  'user.delete': '删除账号',
  'content.moderate': '内容治理',
  'content.edit.own': '编辑本人内容',
  'content.edit.any': '编辑他人内容',
  'content.delete.own': '删除本人内容',
  'content.delete.any': '删除他人内容',
  'moderate.queue': '处理审核队列',
  'recycle.bin': '回收站',
  'prune.run': '批量删帖',
  'forum.manage': '版块与分类',
  'tags.configure': '标签配置',
  'censor.manage': '敏感词',
  'announce.manage': '公告',
  'settings.edit': '站点设置',
  'logs.view': '管理日志',
  'email.manage': '邮件队列',
  'sessions.manage': '用户设备会话',
  'membership.view': '会员资料查看',
  'membership.configure': '会员配置',
  'membership.adjust': '会员等级调整',
  'experience.adjust': '经验调整',
  'membership.logs': '会员审计',
  'titles.view': '称号查看',
  'titles.configure': '称号配置',
  'titles.grant': '称号授予',
  'titles.revoke': '称号撤销',
  'titles.logs': '称号日志',
  'points.view': '积分查看',
  'points.configure': '积分规则',
  'points.adjust': '积分调整',
  'engagement.view': '互动配置查看',
  'engagement.configure': '互动配置保存',
  'polls.manage': '投票管理',
  'bounties.manage': '悬赏管理',
  'reply.accept': '采纳回复',
  'upload.use': '使用上传',
};

export interface PermissionsAdminProps {
  points: string[];
  matrix: Record<string, Record<string, boolean>>;
}

export function PermissionsAdmin({ points, matrix }: PermissionsAdminProps) {
  const router = useRouter();
  const [state, setState] = useState<Record<string, Record<string, boolean>>>(() => {
    const next: Record<string, Record<string, boolean>> = {};
    for (const role of ADMIN_ROLES) {
      next[role.id] = {};
      for (const point of points) next[role.id][point] = Boolean(matrix[role.id]?.[point]);
    }
    return next;
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  function toggle(roleId: string, point: string, value: boolean) {
    setState((prev) => ({ ...prev, [roleId]: { ...prev[roleId], [point]: value } }));
    setNotice(null);
  }

  async function save() {
    setBusy(true);
    setError(null);
    setNotice(null);
    // 该接口没有版本号，是整表覆盖；失败时保留当前输入不重置。
    const form = new FormData();
    for (const role of ADMIN_ROLES) {
      for (const point of points) {
        if (role.id === '1' && HARD_PROTECTED.includes(point)) continue; // 硬保护由后端强制
        if (state[role.id][point]) form.append('allow.' + role.id + '.' + point, '1');
      }
    }
    try {
      await browserSend('/admin/perms/save', { method: 'POST', form });
      toastSuccess('权限矩阵已保存');
      setNotice('权限矩阵已保存并即时生效；当前账号的能力会在刷新后按新矩阵重新计算。');
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      setError('保存失败：' + err.message + '。你的改动仍然保留在页面上。');
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className={['panel', styles.card].join(' ')} aria-label="管理员权限矩阵">
      <div className={styles.head}>
        <h2 className={styles.title}>管理员权限矩阵</h2>
        <Button size="small" type="primary" loading={busy} onClick={() => void save()}>
          保存矩阵
        </Button>
      </div>
      <p className={styles.hint}>
        这里配置的是<strong>后台角色权限点</strong>（会员 / 版主 / 管理员），不是会员等级权限。
        会员等级的权限与额度在{' '}
        <Link href="/admin/membership">会员等级</Link> 配置；用户组（会员 / 版主 / 管理员）在{' '}
        <Link href="/admin/users">用户与封禁</Link> 调整。三者互相独立。
      </p>
      <p className={styles.hint}>
        管理员不可自锁：进入后台与编辑权限矩阵两个权限点对管理员强制开启，后端会忽略这里的修改。
        该接口没有版本号，是整表覆盖；失败时页面保留当前输入。
      </p>
      {error ? <p className={styles.error} role="alert">{error}</p> : null}
      {notice ? <p className={styles.notice}>{notice}</p> : null}

      <div className={styles.tableWrap}>
        <table className={styles.table}>
          <thead>
            <tr>
              <th>权限点</th>
              {ADMIN_ROLES.map((role) => (
                <th key={role.id}>{role.label}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {points.map((point) => (
              <tr key={point}>
                <td>
                  <span className={styles.pointName}>{POINT_LABELS[point] ?? point}</span>
                  <span className={styles.pointCode}>{point}</span>
                </td>
                {ADMIN_ROLES.map((role) => {
                  const locked = role.id === '1' && HARD_PROTECTED.includes(point);
                  return (
                    <td key={role.id} data-label={role.label}>
                      <Checkbox
                        checked={locked ? true : Boolean(state[role.id][point])}
                        disabled={locked}
                        onChange={(value) => toggle(role.id, point, value)}
                      />
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className={styles.actions}>
        <Button type="primary" loading={busy} onClick={() => void save()}>
          保存矩阵
        </Button>
        <Button type="text" onClick={() => router.refresh()}>
          重新读取
        </Button>
      </div>
    </section>
  );
}
