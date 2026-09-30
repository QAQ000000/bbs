'use client';

import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Input, InputNumber, Modal, Select } from '@arco-design/web-react';
import { browserSend } from '@/lib/api/browser';
import { ApiError } from '@/lib/api/errors';
import type { AdminUserView } from '@/lib/api/types';
import { formatCount, formatDateTime, isEmptyTime } from '@/lib/format';
import { LevelBadge } from '../ui/LevelBadge';
import { toastError, toastSuccess } from '../ui/feedback';
import styles from './UserAdmin.module.css';

const GROUP_LABELS: Record<number, string> = { 0: '会员', 1: '管理员', 2: '版主' };

type RestrictKind = 'ban' | 'block';

export function UserAdmin({ users, query }: { users: AdminUserView[]; query: string }) {
  const router = useRouter();
  const [detail, setDetail] = useState<AdminUserView | null>(null);
  const [restrict, setRestrict] = useState<{ user: AdminUserView; kind: RestrictKind } | null>(null);
  const [days, setDays] = useState(0);
  const [reason, setReason] = useState('');
  const [groupTarget, setGroupTarget] = useState<AdminUserView | null>(null);
  const [group, setGroup] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [keyword, setKeyword] = useState(query);

  async function run(path: string, body: Record<string, unknown>, okText: string) {
    setBusy(true);
    setError(null);
    try {
      await browserSend(path, { method: 'POST', body });
      toastSuccess(okText);
      setRestrict(null);
      setGroupTarget(null);
      router.refresh();
    } catch (caught) {
      const err = caught as ApiError;
      // 权限点可能已被矩阵撤销；403 原样展示，不自行推断能力。
      setError(err.message);
      toastError(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <form className={styles.toolbar} action="/admin/users" method="get" role="search">
        <input className={styles.search} type="search" name="q" defaultValue={query} placeholder="按用户名或邮箱搜索" aria-label="搜索用户" />
        <Button size="small" type="secondary" htmlType="submit">
          搜索
        </Button>
      </form>

      {users.length > 0 ? (
        <div className={['panel', styles.tableWrap].join(' ')}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>用户</th>
                <th>用户组</th>
                <th>发帖</th>
                <th>状态</th>
                <th>最后登录</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {users.map((user) => (
                <tr key={user.id}>
                  <td>
                    <span className={styles.name}>{user.username}</span>
                    <LevelBadge level={user.level} size="sm" />
                    <p className={styles.meta}>#{user.id}{user.email ? ' · ' + user.email : ''}</p>
                  </td>
                  <td>{GROUP_LABELS[user.groupId] ?? '未知（' + user.groupId + '）'}</td>
                  <td>{formatCount(user.postCount)}</td>
                  <td>
                    {user.isBlocked ? <span className={styles.danger}>已封禁登录</span> : null}
                    {user.isBanned ? <span className={styles.warn}>已禁言</span> : null}
                    {!user.isBlocked && !user.isBanned ? <span className={styles.ok}>正常</span> : null}
                    {user.banReason ? <p className={styles.meta}>理由：{user.banReason}</p> : null}
                  </td>
                  <td className={styles.meta}>{isEmptyTime(user.lastLoginAt) ? '—' : formatDateTime(user.lastLoginAt)}</td>
                  <td className={styles.actions}>
                    <Button size="mini" type="text" onClick={() => setDetail(user)}>
                      详情
                    </Button>
                    {user.isBanned ? (
                      <Button size="mini" type="text" loading={busy} onClick={() => void run('/admin/users/unban', { uid: user.id }, '已解禁')}>
                        解除禁言
                      </Button>
                    ) : (
                      <Button size="mini" type="text" onClick={() => { setRestrict({ user, kind: 'ban' }); setDays(0); setReason(''); setError(null); }}>
                        禁言
                      </Button>
                    )}
                    {user.isBlocked ? (
                      <Button size="mini" type="text" loading={busy} onClick={() => void run('/admin/users/unblock', { uid: user.id }, '已解封')}>
                        解除封禁
                      </Button>
                    ) : (
                      <Button size="mini" type="text" status="danger" onClick={() => { setRestrict({ user, kind: 'block' }); setDays(0); setReason(''); setError(null); }}>
                        封禁登录
                      </Button>
                    )}
                    <Button size="mini" type="text" onClick={() => { setGroupTarget(user); setGroup(user.groupId); setError(null); }}>
                      用户组
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="panel" style={{ padding: 24, color: 'var(--color-text-tertiary)' }}>
          没有匹配的用户。
        </div>
      )}

      <Modal title="用户详情" visible={Boolean(detail)} footer={null} onCancel={() => setDetail(null)}>
        {detail ? (
          <dl className={styles.detail}>
            <div>
              <dt>用户名</dt>
              <dd>{detail.username}</dd>
            </div>
            <div>
              <dt>用户 ID</dt>
              <dd className={styles.mono}>{detail.id}</dd>
            </div>
            <div>
              <dt>邮箱</dt>
              <dd>{detail.email || '未绑定'}</dd>
            </div>
            <div>
              <dt>用户组</dt>
              <dd>{GROUP_LABELS[detail.groupId] ?? detail.groupId}</dd>
            </div>
            <div>
              <dt>发帖数</dt>
              <dd>{formatCount(detail.postCount)}</dd>
            </div>
            <div>
              <dt>注册时间</dt>
              <dd>{formatDateTime(detail.createdAt)}</dd>
            </div>
            <div>
              <dt>最后登录</dt>
              <dd>{isEmptyTime(detail.lastLoginAt) ? '—' : formatDateTime(detail.lastLoginAt)}</dd>
            </div>
            <div>
              <dt>禁言</dt>
              <dd>{detail.isBanned ? '是' + (detail.bannedUntil ? '（至 ' + formatDateTime(detail.bannedUntil) + '）' : '（永久）') : '否'}</dd>
            </div>
            <div>
              <dt>封禁登录</dt>
              <dd>{detail.isBlocked ? '是' + (detail.blockedUntil ? '（至 ' + formatDateTime(detail.blockedUntil) + '）' : '（永久）') : '否'}</dd>
            </div>
          </dl>
        ) : null}
      </Modal>

      <Modal
        title={restrict?.kind === 'block' ? '封禁登录' : '禁言'}
        visible={Boolean(restrict)}
        confirmLoading={busy}
        onCancel={() => setRestrict(null)}
        onOk={() => {
          if (!restrict) return;
          if (restrict.kind === 'block') {
            void run('/admin/users/block', { uid: restrict.user.id, days }, '已封禁');
          } else {
            void run('/admin/users/ban', { uid: restrict.user.id, days, reason }, '已禁言');
          }
        }}
        okText="确认"
        cancelText="取消"
      >
        {restrict ? (
          <div className={styles.form}>
            <p className={styles.hint}>
              {restrict.kind === 'block'
                ? '封禁后该账号无法登录，并进行中的会话会被踢下线。'
                : '禁言后该账号仍可登录浏览，但不能发帖与回复。'}
            </p>
            {error ? <p className={styles.error}>{error}</p> : null}
            <label className={styles.label} htmlFor="restrict-days">
              期限天数（0 表示永久）
            </label>
            <InputNumber id="restrict-days" min={0} max={3650} value={days} onChange={(v) => setDays(Number(v) || 0)} />
            {restrict.kind === 'ban' ? (
              <>
                <label className={styles.label} htmlFor="restrict-reason">
                  理由（可选，会记录到审计日志）
                </label>
                <Input id="restrict-reason" value={reason} onChange={setReason} maxLength={200} />
              </>
            ) : null}
          </div>
        ) : null}
      </Modal>

      <Modal
        title="调整用户组"
        visible={Boolean(groupTarget)}
        confirmLoading={busy}
        onCancel={() => setGroupTarget(null)}
        onOk={() => {
          if (!groupTarget) return;
          void run('/admin/users/group', { uid: groupTarget.id, group }, '用户组已调整');
        }}
        okText="保存"
        cancelText="取消"
      >
        {groupTarget ? (
          <div className={styles.form}>
            {error ? <p className={styles.error}>{error}</p> : null}
            <p className={styles.hint}>
              当前用户组：{GROUP_LABELS[groupTarget.groupId] ?? groupTarget.groupId}。调整为管理员会获得后台权限，需谨慎。
            </p>
            <Select value={String(group)} onChange={(v) => setGroup(Number(v))} style={{ width: 200 }}>
              <Select.Option value="0">会员</Select.Option>
              <Select.Option value="1">管理员</Select.Option>
              <Select.Option value="2">版主</Select.Option>
            </Select>
          </div>
        ) : null}
      </Modal>
    </div>
  );
}
