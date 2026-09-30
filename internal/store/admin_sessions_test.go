package store

import (
	"context"
	"errors"
	"testing"
)

// 管理员会话撤销：按用户隔离、单个会话 404 语义，以及“会话撤销 + MFA 清理”同事务回滚。
func TestAdminRevokeSessionsScopedAndAtomic(t *testing.T) {
	ctx := context.Background()
	uid, other := setupUsers(t)
	rawA, _, err := testStore.CreateDeviceSession(ctx, uid, "ua-a", "192.0.2.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = testStore.CreateSession(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if _, _, err = testStore.CreateSession(ctx, other); err != nil {
		t.Fatal(err)
	}
	a, err := testStore.Session(ctx, rawA)
	if err != nil {
		t.Fatal(err)
	}
	active := func(u int64) int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>now()`, u).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := active(uid); got != 2 {
		t.Fatalf("setup active sessions = %d, want 2", got)
	}

	// target < 1 不是合法的单个撤销输入。
	if _, err := testStore.AdminRevokeSessions(ctx, uid, 0, "revoke"); err == nil {
		t.Fatal("target=0 revoke must be rejected")
	}
	// 单个会话不存在 → ErrNotFound，且不影响现有会话。
	if _, err := testStore.AdminRevokeSessions(ctx, uid, 999999, "revoke"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing session error = %v, want ErrNotFound", err)
	}
	// 他人会话 ID 不能被目标用户撤销。
	var strangerID int64
	if err := testPool.QueryRow(ctx, `SELECT id FROM sessions WHERE user_id=$1 ORDER BY id LIMIT 1`, other).Scan(&strangerID); err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.AdminRevokeSessions(ctx, uid, strangerID, "revoke"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user revoke error = %v, want ErrNotFound", err)
	}
	if got := active(uid); got != 2 {
		t.Fatalf("rejected revokes changed target sessions: %d", got)
	}
	if got := active(other); got != 1 {
		t.Fatalf("rejected revokes changed other sessions: %d", got)
	}

	// 原子性：清理 MFA 挑战失败时，会话撤销必须一起回滚。
	if _, err := testPool.Exec(ctx, `INSERT INTO mfa_challenges(user_id,password_hash,expires_at) VALUES($1,'x',now()+interval '5 min')`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `CREATE FUNCTION fail_mfa_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'mfa delete failure'; END $$;
	 CREATE TRIGGER fail_mfa_delete BEFORE DELETE ON mfa_challenges FOR EACH ROW EXECUTE FUNCTION fail_mfa_delete()`); err != nil {
		t.Fatal(err)
	}
	dropTrigger := func() {
		_, _ = testPool.Exec(ctx, `DROP TRIGGER IF EXISTS fail_mfa_delete ON mfa_challenges; DROP FUNCTION IF EXISTS fail_mfa_delete()`)
	}
	t.Cleanup(dropTrigger)
	if _, err := testStore.AdminRevokeSessions(ctx, uid, 0, "all"); err == nil {
		t.Fatal("revoke-all must surface the MFA cleanup failure")
	}
	if got := active(uid); got != 2 {
		t.Fatalf("revoke-all was not rolled back: active=%d", got)
	}
	dropTrigger()

	// 正常全部撤销仍然成功，且只影响目标用户。
	n, err := testStore.AdminRevokeSessions(ctx, uid, 0, "all")
	if err != nil || n != 2 {
		t.Fatalf("revoke-all n=%d err=%v", n, err)
	}
	if got := active(uid); got != 0 {
		t.Fatalf("target sessions remain: %d", got)
	}
	if got := active(other); got != 1 {
		t.Fatalf("other user's sessions changed: %d", got)
	}

	// 单个撤销成功一次，重复撤销按文档返回 ErrNotFound。
	rawB, _, err := testStore.CreateSession(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	b, err := testStore.Session(ctx, rawB)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := testStore.AdminRevokeSessions(ctx, uid, b.ID, "revoke"); err != nil || n != 1 {
		t.Fatalf("single revoke n=%d err=%v", n, err)
	}
	if _, err := testStore.AdminRevokeSessions(ctx, uid, b.ID, "revoke"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("repeated revoke error = %v, want ErrNotFound", err)
	}
	_ = a
}
