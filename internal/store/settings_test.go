package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"dzforum/internal/db"
)

func preserveSettings(t *testing.T) {
	t.Helper()
	values, err := readSettingsValues(context.Background(), testPool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := testPool.Exec(ctx, `DELETE FROM settings`); err != nil {
			t.Error(err)
			return
		}
		for k, v := range values {
			if _, err := testPool.Exec(ctx, `INSERT INTO settings(key,value) VALUES($1,$2)`, k, v); err != nil {
				t.Error(err)
			}
		}
	})
}

func TestSettingsAtomicWritesAndAudit(t *testing.T) {
	preserveSettings(t)
	ctx := context.Background()
	actor, _ := setupUsers(t)
	before, err := testStore.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var original string
	if err = testPool.QueryRow(ctx, `SELECT coalesce(jsonb_object_agg(key,value),'{}')::text FROM settings`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"settings", "admin_logs"} {
		t.Run(target, func(t *testing.T) {
			condition := "NEW.key='footer_text'"
			if target == "admin_logs" {
				condition = "NEW.action='settings.save'"
			}
			if _, err := testPool.Exec(ctx, `CREATE FUNCTION fail_settings_save() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF `+condition+` THEN RAISE EXCEPTION 'injected failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_settings_save BEFORE INSERT ON `+target+` FOR EACH ROW EXECUTE FUNCTION fail_settings_save()`); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := testPool.Exec(ctx, `DROP TRIGGER fail_settings_save ON `+target+`; DROP FUNCTION fail_settings_save()`); err != nil {
					t.Error(err)
				}
			})
			if _, err := testStore.SaveSiteSettings(ctx, before.Version, map[string]string{"site_name": "changed", "footer_text": "changed"}, actor, "masked"); err == nil {
				t.Fatal("failure ignored")
			}
			var after string
			if err := testPool.QueryRow(ctx, `SELECT coalesce(jsonb_object_agg(key,value),'{}')::text FROM settings`).Scan(&after); err != nil || after != original {
				t.Fatal("partial settings/version commit", after, err)
			}
			var audits int
			if err := testPool.QueryRow(ctx, `SELECT count(*) FROM admin_logs WHERE uid=$1 AND action='settings.save'`, actor).Scan(&audits); err != nil || audits != 0 {
				t.Fatal("partial audit", audits, err)
			}
		})
	}
	after, err := testStore.SaveSiteSettings(ctx, before.Version, map[string]string{"site_name": "new name", "footer_text": "new footer"}, actor, "masked")
	if err != nil || after.Version != before.Version+1 || after.SiteName != "new name" {
		t.Fatal(after, err)
	}
	var detail []byte
	if err = testPool.QueryRow(ctx, `SELECT detail FROM admin_logs WHERE uid=$1 AND action='settings.save'`, actor).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	var audit struct {
		Version int64
		Changes map[string]struct{ Before, After any }
	}
	if err = json.Unmarshal(detail, &audit); err != nil || audit.Version != after.Version || audit.Changes["siteName"].Before != before.SiteName || audit.Changes["siteName"].After != "new name" {
		t.Fatal(string(detail), err)
	}
}

func TestSettingsConcurrentSavesAndReload(t *testing.T) {
	preserveSettings(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	actor, _ := setupUsers(t)
	before, err := testStore.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start, results := make(chan struct{}), make(chan error, 2)
	for _, name := range []string{"first", "second"} {
		go func(name string) {
			<-start
			_, err := New(testPool).SaveSiteSettings(ctx, before.Version, map[string]string{"site_name": name}, actor, "")
			results <- err
		}(name)
	}
	close(start)
	success, conflict := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, ErrSettingsConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	after, err := New(testPool).Settings(ctx)
	if err != nil || after.Version != before.Version+1 {
		t.Fatal(after, err)
	}
	if err = db.Migrate(ctx, testPool); err != nil {
		t.Fatal(err)
	}
	reloaded, err := New(testPool).Settings(ctx)
	if err != nil || reloaded != after {
		t.Fatal("restart changed configuration", reloaded, err)
	}
}

func TestSettingsValidationAndRecovery(t *testing.T) {
	preserveSettings(t)
	ctx := context.Background()
	actor, _ := setupUsers(t)
	before, err := testStore.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, changes := range []map[string]string{
		{"threads_per_page": "999999"}, {"upload_enabled": "true"}, {"terms_content": ""}, {"max_image_mb": "0"}, {"site_name": "x\ny"}, {"site_name": strings.Repeat("x", 101)}, {"unknown": "x"}, {settingsVersionKey: "99"},
	} {
		if _, err = testStore.SaveSiteSettings(ctx, before.Version, changes, actor, ""); err == nil {
			t.Fatal("invalid settings accepted", changes)
		}
	}
	if _, err = testPool.Exec(ctx, `INSERT INTO settings(key,value) VALUES('threads_per_page','999999') ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`); err != nil {
		t.Fatal(err)
	}
	if _, err = testStore.Settings(ctx); err == nil {
		t.Fatal("invalid stored value silently defaulted")
	}
	after, err := testStore.SaveSiteSettings(ctx, before.Version, map[string]string{"threads_per_page": "25"}, actor, "")
	if err != nil || after.ThreadsPerPage != 25 {
		t.Fatal(after, err)
	}
	ctxCancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = testStore.Settings(ctxCancelled); err == nil {
		t.Fatal("failed read served cached/default settings")
	}
}

func TestSettingsDefaultsAreAtomicAndVersioned(t *testing.T) {
	preserveSettings(t)
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `DELETE FROM settings`); err != nil {
		t.Fatal(err)
	}
	if err := testStore.EnsureSettingsDefaults(ctx, map[string]string{"site_name": "initial", "threads_per_page": "bad"}); err == nil {
		t.Fatal("invalid default accepted")
	}
	var n int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM settings`).Scan(&n); err != nil || n != 0 {
		t.Fatal("partial defaults", n, err)
	}
	if err := testStore.EnsureSettingsDefaults(ctx, map[string]string{"site_name": "initial"}); err != nil {
		t.Fatal(err)
	}
	v, err := testStore.Settings(ctx)
	if err != nil || v.Version != 2 {
		t.Fatal(v, err)
	}
	if err = testStore.EnsureSettingsDefaults(ctx, map[string]string{"site_name": "replacement"}); err != nil {
		t.Fatal(err)
	}
	after, err := testStore.Settings(ctx)
	if err != nil || after != v {
		t.Fatal("startup overwrote configured values", after, err)
	}
}
