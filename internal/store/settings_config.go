package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var ErrSettingsConflict = errors.New("settings version conflict")

func SiteSettingsDefaults() SiteSettings { return defaultSettings() }

type SettingsValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *SettingsValidationError) Error() string { return e.Field + ": " + e.Message }

type SettingField struct {
	Name       string `json:"name"`
	LegacyName string `json:"legacyName"`
	Type       string `json:"type"`
	Min        int    `json:"min"`
	Max        int    `json:"max"`
}

// One catalog defines accepted keys, scalar types and bounds for all write paths.
func SiteSettingFields() []SettingField {
	return []SettingField{
		{"siteName", "site_name", "string", 1, 100},
		{"threadsPerPage", "threads_per_page", "integer", 5, 100},
		{"postsPerPage", "posts_per_page", "integer", 5, 100},
		{"registerEnabled", "register_enabled", "boolean", 0, 0},
		{"siteClosed", "site_closed", "boolean", 0, 0},
		{"siteClosedReason", "site_closed_reason", "string", 1, 1000},
		{"moderateEnabled", "moderate_enabled", "boolean", 0, 0},
		{"uploadEnabled", "upload_enabled", "boolean", 0, 0},
		{"maxImageMB", "max_image_mb", "integer", 1, 1024},
		{"maxFileMB", "max_file_mb", "integer", 1, 1024},
		{"uploadMaxDiskGB", "upload_max_disk_gb", "integer", 1, 1024},
		{"captchaEnabled", "captcha_enabled", "boolean", 0, 0},
		{"emailVerifyEnabled", "email_verify_enabled", "boolean", 0, 0},
		{"requireConsent", "require_consent", "boolean", 0, 0},
		{"termsContent", "terms_content", "string", 0, 30000},
		{"privacyContent", "privacy_content", "string", 0, 30000},
		{"siteLogo", "site_logo", "string", 0, 200},
		{"footerText", "footer_text", "string", 0, 2000},
		{"analyticsRetentionDays", "analytics_retention_days", "integer", 0, 3650},
		{"reportTimeZone", "report_time_zone", "string", 1, 100},
	}
}

func normalizeSetting(f SettingField, value string) (string, any, error) {
	bad := func(message string) (string, any, error) {
		return "", nil, &SettingsValidationError{f.Name, message}
	}
	switch f.Type {
	case "boolean":
		if value != "0" && value != "1" {
			return bad("必须为布尔值")
		}
		return value, value == "1", nil
	case "integer":
		n, err := strconv.Atoi(value)
		if err != nil || n < f.Min || n > f.Max {
			return bad(fmt.Sprintf("必须为 %d 到 %d 的整数", f.Min, f.Max))
		}
		return strconv.Itoa(n), n, nil
	default:
		if f.Name != "termsContent" && f.Name != "privacyContent" {
			value = strings.TrimSpace(value)
		}
		n := utf8.RuneCountInString(value)
		if !utf8.ValidString(value) || n < f.Min || n > f.Max || strings.ContainsRune(value, 0) {
			return bad(fmt.Sprintf("长度必须为 %d 到 %d 个字符", f.Min, f.Max))
		}
		if (f.Name == "siteName" || f.Name == "siteLogo") && strings.ContainsFunc(value, unicode.IsControl) {
			return bad("不能包含控制字符")
		}
		if f.Name == "reportTimeZone" && !validReportTimeZone(value) {
			return bad("必须为有效的 IANA 时区，如 UTC 或 Asia/Shanghai；不支持 Local")
		}
		return value, value, nil
	}
}

const settingsVersionKey = "site_settings_version"

type settingsReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func readSettingsValues(ctx context.Context, q settingsReader) (map[string]string, error) {
	rows, err := q.Query(ctx, `SELECT key,value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		values[key] = value
	}
	return values, rows.Err()
}

func settingsVersion(values map[string]string) (int64, error) {
	if raw, ok := values[settingsVersionKey]; ok {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 {
			return 0, &SettingsValidationError{"version", "已保存的版本号无效"}
		}
		return n, nil
	}
	return 1, nil
}

func settingsFromValues(values map[string]string) (SiteSettings, error) {
	v := defaultSettings()
	data, _ := json.Marshal(v)
	var fields map[string]any
	_ = json.Unmarshal(data, &fields)
	for _, f := range SiteSettingFields() {
		if raw, ok := values[f.LegacyName]; ok {
			_, value, err := normalizeSetting(f, raw)
			if err != nil {
				return SiteSettings{}, err
			}
			fields[f.Name] = value
		}
	}
	version, err := settingsVersion(values)
	if err != nil {
		return SiteSettings{}, err
	}
	fields["version"] = version
	data, err = json.Marshal(fields)
	if err != nil {
		return SiteSettings{}, err
	}
	if err = json.Unmarshal(data, &v); err != nil {
		return SiteSettings{}, err
	}
	if v.RequireConsent && (strings.TrimSpace(v.TermsContent) == "" || strings.TrimSpace(v.PrivacyContent) == "") {
		return SiteSettings{}, &SettingsValidationError{"requireConsent", "要求同意条款时，条款和隐私政策均不能为空"}
	}
	return v, nil
}

func readSettings(ctx context.Context, q settingsReader) (SiteSettings, error) {
	values, err := readSettingsValues(ctx, q)
	if err != nil {
		return SiteSettings{}, err
	}
	return settingsFromValues(values)
}

// Settings returns a database snapshot. Errors never become permissive defaults.
func (s *Store) Settings(ctx context.Context) (SiteSettings, error) { return readSettings(ctx, s.pool) }

func (s *Store) SettingsVersion(ctx context.Context) (int64, error) {
	values, err := readSettingsValues(ctx, s.pool)
	if err != nil {
		return 0, err
	}
	return settingsVersion(values)
}

func lockSettings(ctx context.Context, tx pgx.Tx) (int64, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO settings(key,value) VALUES($1,'1') ON CONFLICT DO NOTHING`, settingsVersionKey); err != nil {
		return 0, err
	}
	var raw string
	if err := tx.QueryRow(ctx, `SELECT value FROM settings WHERE key=$1 FOR UPDATE`, settingsVersionKey).Scan(&raw); err != nil {
		return 0, err
	}
	return settingsVersion(map[string]string{settingsVersionKey: raw})
}

// SaveSiteSettings serializes writers and commits values, version and audit together.
func (s *Store) SaveSiteSettings(ctx context.Context, expected int64, changes map[string]string, actor int64, ip string) (SiteSettings, error) {
	if expected < 1 || len(changes) == 0 {
		return SiteSettings{}, &SettingsValidationError{"version", "需要当前版本及至少一项设置"}
	}
	catalog := map[string]SettingField{}
	for _, f := range SiteSettingFields() {
		catalog[f.LegacyName] = f
	}
	normalized := map[string]string{}
	for key, value := range changes {
		f, ok := catalog[key]
		if !ok {
			return SiteSettings{}, &SettingsValidationError{key, "未知设置"}
		}
		value, _, err := normalizeSetting(f, value)
		if err != nil {
			return SiteSettings{}, err
		}
		normalized[key] = value
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SiteSettings{}, err
	}
	defer tx.Rollback(ctx)
	version, err := lockSettings(ctx, tx)
	if err != nil {
		return SiteSettings{}, err
	}
	if version != expected {
		return SiteSettings{}, ErrSettingsConflict
	}
	values, err := readSettingsValues(ctx, tx)
	if err != nil {
		return SiteSettings{}, err
	}
	defaults, _ := json.Marshal(defaultSettings())
	var initial map[string]any
	_ = json.Unmarshal(defaults, &initial)
	diff := map[string]any{}
	for key, value := range normalized {
		f := catalog[key]
		before := initial[f.Name]
		if old, ok := values[key]; ok {
			_, before, err = normalizeSetting(f, old)
			if err != nil {
				before = map[string]string{"invalidStoredValue": old}
			}
		}
		_, after, _ := normalizeSetting(f, value)
		beforeJSON, _ := json.Marshal(before)
		afterJSON, _ := json.Marshal(after)
		if !bytes.Equal(beforeJSON, afterJSON) {
			diff[f.Name] = map[string]any{"before": before, "after": after}
		}
		values[key] = value
	}
	values[settingsVersionKey] = strconv.FormatInt(version+1, 10)
	v, err := settingsFromValues(values)
	if err != nil {
		return SiteSettings{}, err
	}
	for _, f := range SiteSettingFields() {
		if value, ok := normalized[f.LegacyName]; ok {
			if _, err = tx.Exec(ctx, `INSERT INTO settings(key,value) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`, f.LegacyName, value); err != nil {
				return SiteSettings{}, err
			}
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE settings SET value=$2 WHERE key=$1`, settingsVersionKey, values[settingsVersionKey]); err != nil {
		return SiteSettings{}, err
	}
	var username string
	if err = tx.QueryRow(ctx, `SELECT username FROM users WHERE id=$1`, actor).Scan(&username); err != nil {
		return SiteSettings{}, err
	}
	detail, err := json.Marshal(map[string]any{"previousVersion": version, "version": v.Version, "changes": diff})
	if err != nil {
		return SiteSettings{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO admin_logs(uid,username,action,detail,ip) VALUES($1,$2,'settings.save',$3,$4)`, actor, username, string(detail), ip); err != nil {
		return SiteSettings{}, err
	}
	return v, tx.Commit(ctx)
}
