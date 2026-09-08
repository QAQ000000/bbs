package api

import (
	"context"
	"net/http"
	"testing"
)

func TestRequestAccessQueryBudget(t *testing.T) {
	requireDB(t)
	u, cookie, _ := memberTestUser(t)
	// Warm installation state without caching user authorization.
	checkJSON(t, smokeGet(t, "/api/v1/site", cookie), 200)
	for _, test := range []struct {
		cookie  *http.Cookie
		queries int64
	}{{nil, 2}, {cookie, 3}} {
		before := smokePool.Stat().AcquireCount()
		checkJSON(t, smokeGet(t, "/api/v1/site", test.cookie), 200)
		if n := smokePool.Stat().AcquireCount() - before; n != test.queries {
			t.Fatalf("request queries=%d want=%d", n, test.queries)
		}
	}
	if _, err := smokePool.Exec(context.Background(), `UPDATE users SET banned_until=now()+interval '1 hour' WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	checkJSON(t, smokeGet(t, "/api/v1/me/membership", cookie), 200)
	if err := smokeSrv.st.DeleteSession(context.Background(), cookie.Value); err != nil {
		t.Fatal(err)
	}
	checkJSON(t, smokeGet(t, "/api/v1/me/membership", cookie), 401)
}
