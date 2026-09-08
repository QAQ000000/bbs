package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRequestAccessSnapshot(t *testing.T) {
	ctx := context.Background()
	uid, _ := setupUsers(t)
	fid := setupForum(t)
	if _, err := testPool.Exec(ctx, `UPDATE users SET group_id=2,banned_until=now()+interval '1 hour' WHERE id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO forum_moderators(forum_id,user_id) VALUES($1,$2)`, fid, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := testStore.ReserveMemberQuota(ctx, uid, "thread.create", 2, 10); err != nil {
		t.Fatal(err)
	}
	c, err := testStore.FreshMembershipConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want, err := testStore.MembershipWithConfig(ctx, uid, c)
	if err != nil {
		t.Fatal(err)
	}
	before := testPool.Stat().AcquireCount()
	a, err := testStore.MembershipAccess(ctx, uid)
	if err != nil || a.Member == nil || !reflect.DeepEqual(*a.Member, want) || !a.Banned || a.Quota["thread.create"] != 2 || !reflect.DeepEqual(a.Moderates, []int64{fid}) {
		t.Fatalf("access: %+v %v", a, err)
	}
	if n := testPool.Stat().AcquireCount() - before; n != 1 {
		t.Fatal("membership acquired connections", n)
	}
	guest, err := testStore.MembershipAccess(ctx, 0)
	if err != nil || guest.Member != nil || len(guest.Quota) != 0 || guest.Banned {
		t.Fatal("guest inherited account state", err)
	}
	if _, err = testStore.MembershipAccess(ctx, -1); err == nil {
		// Negative IDs are not anonymous identities.
		t.Fatal("invalid identity accepted")
	}
}

func TestRequestSessionUserFreshness(t *testing.T) {
	ctx := context.Background()
	uid, _ := setupUsers(t)
	token, _, err := testStore.CreateSession(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	wantSession, err := testStore.Session(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	wantUser, err := testStore.UserByID(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	before := testPool.Stat().AcquireCount()
	sess, user, lastSeen, err := testStore.SessionUser(ctx, token)
	if err != nil || !reflect.DeepEqual(sess, wantSession) || !reflect.DeepEqual(user, wantUser) || lastSeen.IsZero() {
		t.Fatal("session snapshot differs", err)
	}
	if n := testPool.Stat().AcquireCount() - before; n != 1 {
		t.Fatal("session acquired connections", n)
	}
	if _, err = testPool.Exec(ctx, `UPDATE users SET blocked_until='infinity' WHERE id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = testStore.SessionUser(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatal("blocked user authenticated", err)
	}
	if _, err = testPool.Exec(ctx, `UPDATE users SET blocked_until=NULL,group_id=2 WHERE id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	_, user, _, err = testStore.SessionUser(ctx, token)
	if err != nil || user.GroupID != 2 {
		t.Fatal("role change not visible", err)
	}
	if err = testStore.DeleteSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = testStore.SessionUser(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked session authenticated", err)
	}
}
