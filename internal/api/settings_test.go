package api

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	stdmail "net/mail"
	"strings"
	"testing"
	"time"

	"dzforum/internal/perm"
	"dzforum/internal/store"
)

func settingsAPIFixture(t *testing.T) store.SiteSettings {
	t.Helper()
	requireDB(t)
	ctx := context.Background()
	rows, err := smokePool.Query(ctx, `SELECT key,value FROM settings`)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		values[k] = v
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := smokePool.Exec(ctx, `DELETE FROM settings`); err != nil {
			t.Error(err)
			return
		}
		for k, v := range values {
			if _, err := smokePool.Exec(ctx, `INSERT INTO settings(key,value) VALUES($1,$2)`, k, v); err != nil {
				t.Error(err)
			}
		}
	})
	v, err := smokeSrv.st.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func settingsAPIGet(t *testing.T) store.SiteSettings {
	t.Helper()
	data := checkJSON(t, smokeGet(t, "/api/v1/admin/settings", adminCookie), 200)
	var v store.SiteSettings
	if err := json.Unmarshal(data["data"], &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSettingsAPIValidationAndPartialUpdates(t *testing.T) {
	original := settingsAPIFixture(t)
	path := "/api/v1/admin/settings"
	before := settingsAPIGet(t)
	checkJSON(t, memberJSON(t, "POST", path, map[string]any{"site_name": "only name"}, adminCSRF, adminCookie), 428)
	checkJSON(t, memberJSON(t, "POST", path, map[string]any{"version": before.Version, "site_name": "only name"}, adminCSRF, adminCookie), 422)
	checkJSON(t, memberJSON(t, "PUT", path, map[string]any{"version": before.Version, "siteName": "only name"}, adminCSRF, adminCookie), 422)
	for _, values := range []map[string]any{
		{"version": before.Version, "threadsPerPage": 999999}, {"version": before.Version, "uploadEnabled": "false"},
		{"version": before.Version, "siteName": nil}, {"version": before.Version, "typo": true},
		{"version": before.Version, "termsContent": ""}, {"version": before.Version, "emailVerifyEnabled": true},
	} {
		checkJSON(t, memberJSON(t, "PATCH", path, values, adminCSRF, adminCookie), 422)
	}
	checkJSON(t, apiRequest(t, "PATCH", path, adminCSRF, fmt.Sprintf(`{"version":%d,"siteName":"first","siteName":"last"}`, before.Version), adminCookie), 422)
	if after := settingsAPIGet(t); after != original {
		t.Fatal("invalid request changed settings", after)
	}
	checkJSON(t, memberJSON(t, "PATCH", path, map[string]any{"version": before.Version, "siteName": "new name"}, adminCSRF, adminCookie), 200)
	after := settingsAPIGet(t)
	before.SiteName, before.Version = "new name", before.Version+1
	if after != before {
		t.Fatal("patch changed omitted fields", before, after)
	}
	checkJSON(t, memberJSON(t, "PATCH", path, map[string]any{"version": original.Version, "siteName": "stale"}, adminCSRF, adminCookie), 409)
	after.PostsPerPage = 25
	checkJSON(t, memberJSON(t, "PUT", path, after, adminCSRF, adminCookie), 200)
	after = settingsAPIGet(t)
	if after.PostsPerPage != 25 {
		t.Fatal(after)
	}
	raw, _ := json.Marshal(after)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	legacy := map[string]any{"version": after.Version}
	for _, f := range store.SiteSettingFields() {
		legacy[f.LegacyName] = fields[f.Name]
	}
	legacy["footer_text"] = "legacy footer"
	checkJSON(t, memberJSON(t, "POST", path, legacy, adminCSRF, adminCookie), 200)
	if v := settingsAPIGet(t); v.FooterText != "legacy footer" || v.Version != after.Version+1 {
		t.Fatal(v)
	}
}

func TestSettingsAPIPermissionsAndMetadata(t *testing.T) {
	v := settingsAPIFixture(t)
	path := "/api/v1/admin/settings"
	for _, suffix := range []string{"", "/schema", "/status"} {
		checkJSON(t, smokeGet(t, path+suffix, userCookie), 403)
		checkJSON(t, smokeGet(t, path+suffix, adminCookie), 200)
	}
	for _, method := range []string{"PUT", "PATCH", "POST"} {
		checkJSON(t, memberJSON(t, method, path, v, "", adminCookie), 403)
		checkJSON(t, memberJSON(t, method, path, v, userCSRF, userCookie), 403)
	}
	previous := perm.Matrix()
	t.Cleanup(func() { perm.Load(previous) })
	m := perm.Matrix()
	m[perm.RoleAdmin][perm.SettingsEdit] = false
	perm.Load(m)
	checkJSON(t, smokeGet(t, path+"/schema", adminCookie), 403)
	checkJSON(t, memberJSON(t, "PATCH", path, map[string]any{"version": v.Version, "siteName": "forbidden"}, adminCSRF, adminCookie), 403)
	perm.Load(previous)
	data := checkJSON(t, smokeGet(t, path+"/schema", adminCookie), 200)
	var schema struct {
		Fields          []store.SettingField
		Defaults        store.SiteSettings
		VersionRequired bool
	}
	if err := json.Unmarshal(data["data"], &schema); err != nil || len(schema.Fields) != 18 || !schema.VersionRequired || schema.Defaults.ThreadsPerPage != 20 {
		t.Fatal(string(data["data"]), err)
	}
	if after := settingsAPIGet(t); after != v {
		t.Fatal("unauthorized write changed settings")
	}
}

func TestSettingsAPIUnavailableAndRecovery(t *testing.T) {
	v := settingsAPIFixture(t)
	ctx := context.Background()
	if _, err := smokePool.Exec(ctx, `INSERT INTO settings(key,value) VALUES('threads_per_page','999999') ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`); err != nil {
		t.Fatal(err)
	}
	checkJSON(t, smokeGet(t, "/api/v1/site", nil), 503)
	checkJSON(t, smokeGet(t, "/api/v1/admin/settings", adminCookie), 503)
	status := checkJSON(t, smokeGet(t, "/api/v1/admin/settings/status", adminCookie), 200)
	if !strings.Contains(string(status["data"]), `"valid":false`) || !strings.Contains(string(status["data"]), `threadsPerPage`) {
		t.Fatal(string(status["data"]))
	}
	checkJSON(t, smokeGet(t, "/api/v1/admin/settings/schema", adminCookie), 200)
	checkJSON(t, memberJSON(t, "PATCH", "/api/v1/admin/settings", map[string]any{"version": v.Version, "threadsPerPage": 30}, adminCSRF, adminCookie), 200)
	checkJSON(t, smokeGet(t, "/api/v1/site", nil), 200)
	if after := settingsAPIGet(t); after.ThreadsPerPage != 30 {
		t.Fatal(after)
	}
	if _, err := smokePool.Exec(ctx, `ALTER TABLE settings RENAME TO settings_unavailable`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := smokePool.Exec(ctx, `ALTER TABLE settings_unavailable RENAME TO settings`); err != nil {
			t.Error(err)
		}
	})
	checkJSON(t, smokeGet(t, "/api/v1/site", nil), 503)
	checkJSON(t, smokeGet(t, "/api/v1/admin/settings/status", adminCookie), 503)
	checkJSON(t, smokeGet(t, "/api/v1/health/live", nil), 200)
}

func TestSettingsAPIWriteFailureRollback(t *testing.T) {
	v := settingsAPIFixture(t)
	ctx := context.Background()
	if _, err := smokePool.Exec(ctx, `CREATE FUNCTION settings_api_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.key='footer_text' THEN RAISE EXCEPTION 'injected failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER settings_api_failure BEFORE INSERT ON settings FOR EACH ROW EXECUTE FUNCTION settings_api_failure()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := smokePool.Exec(ctx, `DROP TRIGGER settings_api_failure ON settings; DROP FUNCTION settings_api_failure()`); err != nil {
			t.Error(err)
		}
	})
	values := map[string]any{"version": v.Version, "siteName": "changed", "footerText": "changed"}
	checkJSON(t, memberJSON(t, "PATCH", "/api/v1/admin/settings", values, adminCSRF, adminCookie), 503)
	if after := settingsAPIGet(t); after != v {
		t.Fatal("partial configuration committed", after)
	}
	if _, err := smokePool.Exec(ctx, `ALTER TABLE settings DISABLE TRIGGER settings_api_failure`); err != nil {
		t.Fatal(err)
	}
	checkJSON(t, memberJSON(t, "PATCH", "/api/v1/admin/settings", values, adminCSRF, adminCookie), 200)
}

func TestSettingsEmailBrandAndDependency(t *testing.T) {
	v := settingsAPIFixture(t)
	ctx := context.Background()
	srv, smtp := emailAPIServer(t)
	u, _, _ := memberTestUser(t)
	if _, err := smokePool.Exec(ctx, `DELETE FROM email_jobs;`); err != nil {
		t.Fatal(err)
	}
	if _, err := smokePool.Exec(ctx, `UPDATE users SET email='config-brand@example.test' WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	previous := smokeSrv
	smokeSrv = srv
	t.Cleanup(func() { smokeSrv = previous })
	checkJSON(t, memberJSON(t, "PATCH", "/api/v1/admin/settings", map[string]any{"version": v.Version, "siteName": "Current Community", "emailVerifyEnabled": true}, adminCSRF, adminCookie), 200)
	status := checkJSON(t, smokeGet(t, "/api/v1/admin/settings/status", adminCookie), 200)
	if !strings.Contains(string(status["data"]), `"emailVerificationRequired":true`) {
		t.Fatal(string(status["data"]))
	}
	if err := srv.st.QueueAuthEmail(ctx, u.ID, "config-brand@example.test", "password_reset", srv.mailTokens.Seal); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.ProcessEmail(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-smtp.Messages:
		parsed, err := stdmail.ReadMessage(strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
		if err != nil || !strings.Contains(subject, "[Current Community]") || !strings.Contains(body, "--\nCurrent Community") {
			t.Fatal(subject, body, err)
		}
	case <-time.After(time.Second):
		t.Fatal("SMTP received no message")
	}
	// The deployment dependency can disappear without changing the saved switch.
	smokeSrv = previous
	status = checkJSON(t, smokeGet(t, "/api/v1/admin/settings/status", adminCookie), 200)
	if !strings.Contains(string(status["data"]), "SMTP_DISABLED") {
		t.Fatal(string(status["data"]))
	}
}
