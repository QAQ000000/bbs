import Link from 'next/link';
import { getSite } from '@/lib/site.server';
import { Logo } from '@/components/layout/Logo';
import styles from './auth.module.css';

const POINTS = [
  '深度技术交流，拒绝水文与灌水',
  '完善的积分、等级与称号成长体系',
  '开源自建，数据完全自持可控',
];

export default async function AuthLayout({ children }: { children: React.ReactNode }) {
  const site = await getSite();
  const name = site?.name ?? 'GoBBS 社区';
  return (
    <div className={styles.shell}>
      <aside className={styles.brand}>
        <Link href="/" className={styles.brandHead}>
          <Logo size={32} />
          <span>{name}</span>
        </Link>
        <div className={styles.brandBody}>
          <h1 className={styles.slogan}>与开发者一起交流成长</h1>
          <p className={styles.brandDesc}>从一次踩坑复盘到完整的最佳实践，每一个经验都值得被认真对待。</p>
          <ul className={styles.points}>
            {POINTS.map((point) => (
              <li key={point}>
                <span className={styles.check} aria-hidden="true">
                  ✓
                </span>
                {point}
              </li>
            ))}
          </ul>
        </div>
      </aside>
      <main className={styles.formSide}>
        <div className={styles.formInner}>
          <Link href="/" className={styles.mobileBrand}>
            <Logo size={44} />
            <span>{name}</span>
          </Link>
          {site?.siteClosed ? (
            <p className={styles.closed}>
              站点当前已关闭：{site.siteClosedReason || '维护中'}。仅保留必要的管理入口。
            </p>
          ) : null}
          {children}
        </div>
      </main>
    </div>
  );
}
