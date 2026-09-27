package api

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"dzforum/internal/store"
)

// Keep the frontend catalog aligned when settings are added or changed.
func TestSettingsContractCatalog(t *testing.T) {
	doc := loadAPIContract(t)
	schemas := doc["components"].(contractObject)["schemas"].(contractObject)
	fields := store.SiteSettingFields()
	names, legacyNames := []string{"version"}, []string{"version"}
	for _, f := range fields {
		names = append(names, f.Name)
		legacyNames = append(legacyNames, f.LegacyName)
	}
	checkRequired := func(t *testing.T, schema contractObject, want []string) {
		t.Helper()
		var got []string
		for _, value := range schema["required"].([]any) {
			got = append(got, value.(string))
		}
		want = append([]string(nil), want...)
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("required fields = %v, want %v", got, want)
		}
	}
	for _, name := range []string{"SiteSettings", "SiteSettingsReplace", "SiteSettingsPatch"} {
		t.Run(name, func(t *testing.T) {
			schema := schemas[name].(contractObject)
			props := schema["properties"].(contractObject)
			if len(props) != len(names) {
				t.Fatalf("properties = %d, want %d", len(props), len(names))
			}
			version := props["version"].(contractObject)
			if version["type"] != "integer" || version["format"] != "int64" || version["minimum"] != float64(1) {
				t.Fatal("version contract drift", version)
			}
			for _, f := range fields {
				p, ok := props[f.Name].(contractObject)
				if !ok || p["type"] != f.Type || p["nullable"] == true {
					t.Fatalf("%s type drift: %v", f.Name, p)
				}
				min, max := "minimum", "maximum"
				if f.Type == "string" {
					min, max = "minLength", "maxLength"
				}
				if f.Type != "boolean" && (p[min] != float64(f.Min) || p[max] != float64(f.Max)) {
					t.Fatalf("%s bounds drift: %v", f.Name, p)
				}
			}
			if name == "SiteSettingsPatch" {
				checkRequired(t, schema, []string{"version"})
				if schema["minProperties"] != float64(2) {
					t.Fatal("PATCH must require at least one setting")
				}
			} else {
				checkRequired(t, schema, names)
			}
			if name != "SiteSettings" && schema["additionalProperties"] != false {
				t.Fatal("write contract must reject unknown keys")
			}
		})
	}
	for _, name := range []string{"SiteSettingsLegacyJSON", "SiteSettingsLegacyForm"} {
		t.Run(name, func(t *testing.T) {
			schema := schemas[name].(contractObject)
			props := schema["properties"].(contractObject)
			checkRequired(t, schema, legacyNames)
			if len(props) != len(legacyNames)+1 || props["_csrf"] == nil || schema["additionalProperties"] != false {
				t.Fatal("legacy keys or unknown-field policy drift")
			}
			for _, field := range legacyNames {
				p, ok := props[field].(contractObject)
				if !ok || name == "SiteSettingsLegacyForm" && p["type"] != "string" {
					t.Fatalf("legacy property %s drift", field)
				}
			}
		})
	}
}

// Reuse the business regressions to verify the published response shapes/codes.
func checkSettingsContract(t *testing.T, method, path string, rec *httptest.ResponseRecorder, status int) map[string]json.RawMessage {
	t.Helper()
	body := assertContractResponse(t, loadAPIContract(t), method, path, rec, status)
	codes := map[int]string{401: "UNAUTHENTICATED", 409: "SETTINGS_CONFLICT", 422: "VALIDATION_FAILED", 428: "SETTINGS_VERSION_REQUIRED", 503: "SETTINGS_UNAVAILABLE"}
	if want := codes[status]; want != "" && body["error"].(contractObject)["code"] != want {
		t.Fatalf("unexpected settings error: %v, want %s", body, want)
	}
	return checkJSON(t, rec, status)
}
