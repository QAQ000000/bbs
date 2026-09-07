package api

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"dzforum/internal/perm"
	"dzforum/internal/store"
)

func TestPointsAPIPrivacyPermissionsAndAdjustment(t *testing.T) {
	u, cookie, _ := memberTestUser(t)
	path := fmt.Sprintf("/api/v1/admin/points/users/%d", u.ID)
	checkJSON(t, smokeGet(t, "/api/v1/me/points", nil), 401)
	checkJSON(t, smokeGet(t, path, cookie), 403)
	checkJSON(t, smokeGet(t, "/api/v1/me/points", cookie), 200)
	v := store.PointsAdjustment{Version: 1, Delta: 7, Reason: "test reward", Key: "api-reward-001"}
	checkJSON(t, memberJSON(t, "POST", path+"/adjust", v, "", adminCookie), 403)
	checkJSON(t, memberJSON(t, "POST", path+"/adjust", v, adminCSRF, adminCookie), 200)
	checkJSON(t, memberJSON(t, "POST", path+"/adjust", v, adminCSRF, adminCookie), 200)
	v.Delta = 8
	checkJSON(t, memberJSON(t, "POST", path+"/adjust", v, adminCSRF, adminCookie), 409)
	res := checkJSON(t, smokeGet(t, "/api/v1/me/points/ledger", cookie), 200)
	var entries struct {
		Items []store.PointsEntry `json:"items"`
	}
	if err := json.Unmarshal(res["data"], &entries); err != nil || len(entries.Items) != 1 || entries.Items[0].Delta != 7 {
		t.Fatal(entries, err)
	}
	checkJSON(t, smokeGet(t, path+"/reconcile", adminCookie), 200)
	checkJSON(t, smokeGet(t, "/api/v1/me/points/ledger?before=-1", cookie), 422)
	original := perm.Matrix()
	t.Cleanup(func() { perm.Load(original) })
	changed := perm.Matrix()
	changed[perm.RoleAdmin][perm.PointsAdjust] = false
	perm.Load(changed)
	checkJSON(t, memberJSON(t, "POST", path+"/adjust", v, adminCSRF, adminCookie), 403)
	checkJSON(t, smokeGet(t, path, adminCookie), 200)
}

func TestPointsAPIConfigurationVersionAndValidation(t *testing.T) {
	ctx := context.Background()
	requireDB(t)
	c, err := smokeSrv.st.PointsConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(c)
	t.Cleanup(func() {
		_, _ = smokePool.Exec(ctx, `UPDATE points_config SET version=$1,body=$2 WHERE id`, c.Version, raw)
	})
	path := "/api/v1/admin/points/config"
	checkJSON(t, memberJSON(t, "PUT", path, c, "", adminCookie), 403)
	checkJSON(t, memberJSON(t, "PUT", path, c, userCSRF, userCookie), 403)
	checkJSON(t, memberJSON(t, "PUT", path, c, adminCSRF, adminCookie), 200)
	checkJSON(t, memberJSON(t, "PUT", path, c, adminCSRF, adminCookie), 409)
	c.Version++
	c.Rules["thread"] = store.GrowthRule{Enabled: true, Points: -1, DailyCap: 10}
	checkJSON(t, memberJSON(t, "PUT", path, c, adminCSRF, adminCookie), 422)
}
