import { PublicShell } from '@/components/layout/PublicShell';
import { MemberNav } from '@/components/layout/MemberNav';
import styles from './member.module.css';

export default function MemberLayout({ children }: { children: React.ReactNode }) {
  return (
    <PublicShell>
      <div className={styles.page}>
        <div className={styles.grid}>
          <MemberNav />
          <div className={styles.content}>{children}</div>
        </div>
      </div>
    </PublicShell>
  );
}
