package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"dzforum/internal/store"
)

type settingsKey struct{}

func (s *Server) loadSettings(r *http.Request) (*http.Request, error) {
	v, err := s.st.Settings(r.Context())
	if err != nil {
		return r, err
	}
	return r.WithContext(context.WithValue(r.Context(), settingsKey{}, v)), nil
}

func (s *Server) settingsError(w http.ResponseWriter, r *http.Request, err error, reading bool) bool {
	if err == nil {
		return false
	}
	var invalid *store.SettingsValidationError
	switch {
	case reading:
		s.fail(w, r, 503, "SETTINGS_UNAVAILABLE", "配置无法读取，请通过配置状态接口诊断")
	case errors.Is(err, store.ErrSettingsConflict):
		s.fail(w, r, 409, "SETTINGS_CONFLICT", "配置已变化，请重新读取后保存")
	case errors.As(err, &invalid):
		s.fail(w, r, 422, "VALIDATION_FAILED", invalid.Error())
	default:
		s.log.Error("settings operation failed", "err", err)
		s.fail(w, r, 503, "SETTINGS_UNAVAILABLE", "配置保存失败，请稍后重试")
	}
	return true
}

func (s *Server) settingInvalid(w http.ResponseWriter, r *http.Request, field, message string) {
	s.settingsError(w, r, &store.SettingsValidationError{Field: field, Message: message}, false)
}

func (s *Server) persistSettings(w http.ResponseWriter, r *http.Request, version int64, values map[string]string) {
	if values["email_verify_enabled"] == "1" && !s.mailer.Enabled() {
		s.settingInvalid(w, r, "emailVerifyEnabled", "启用邮箱验证前必须配置邮件服务")
		return
	}
	v, err := s.st.SaveSiteSettings(r.Context(), version, values, User(r).ID, maskIP(remoteIP(r)))
	if !s.settingsError(w, r, err, false) {
		s.respond(w, 200, v)
	}
}

func (s *Server) saveLegacySettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.checkCSRF(r) {
		s.fail(w, r, 403, "CSRF_INVALID", "无效 CSRF token")
		return
	}
	if r.PostFormValue("version") == "" {
		s.fail(w, r, 428, "SETTINGS_VERSION_REQUIRED", "请提供读取配置时的 version")
		return
	}
	version, err := strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	if err != nil || version < 1 {
		s.settingInvalid(w, r, "version", "需要正整数版本号")
		return
	}
	known := map[string]bool{"version": true, "_csrf": true}
	values := map[string]string{}
	for _, f := range store.SiteSettingFields() {
		known[f.LegacyName] = true
		v, ok := r.PostForm[f.LegacyName]
		if !ok || len(v) != 1 {
			s.settingInvalid(w, r, f.LegacyName, "完整保存必须提供该字段且不能重复")
			return
		}
		values[f.LegacyName] = v[0]
	}
	for key, v := range r.PostForm {
		if !known[key] || len(v) != 1 {
			s.settingInvalid(w, r, key, "未知或重复字段")
			return
		}
	}
	s.persistSettings(w, r, version, values)
}

func (s *Server) settingsJSONSave(w http.ResponseWriter, r *http.Request) {
	if !s.checkCSRF(r) {
		s.fail(w, r, 403, "CSRF_INVALID", "无效 CSRF token")
		return
	}
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || ct != "application/json" {
		s.fail(w, r, 415, "UNSUPPORTED_MEDIA_TYPE", "请使用 application/json")
		return
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		s.settingInvalid(w, r, "body", "必须为 JSON 对象")
		return
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			s.settingInvalid(w, r, "body", "无效 JSON 字段")
			return
		}
		if _, exists := fields[key]; exists {
			s.settingInvalid(w, r, key, "不能重复提交字段")
			return
		}
		var raw json.RawMessage
		if err = d.Decode(&raw); err != nil {
			s.settingInvalid(w, r, key, "无效或过大的 JSON")
			return
		}
		fields[key] = raw
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		s.settingInvalid(w, r, "body", "无效 JSON 对象")
		return
	}
	if d.Decode(new(any)) != io.EOF {
		s.settingInvalid(w, r, "body", "只能提交一个 JSON 对象")
		return
	}
	rawVersion, ok := fields["version"]
	if !ok {
		s.fail(w, r, 428, "SETTINGS_VERSION_REQUIRED", "请提供读取配置时的 version")
		return
	}
	var version int64
	if json.Unmarshal(rawVersion, &version) != nil || version < 1 {
		s.settingInvalid(w, r, "version", "需要正整数版本号")
		return
	}
	delete(fields, "version")
	if len(fields) == 0 {
		s.settingInvalid(w, r, "body", "至少提供一项设置")
		return
	}
	values := map[string]string{}
	for _, f := range store.SiteSettingFields() {
		raw, supplied := fields[f.Name]
		if !supplied {
			if r.Method == http.MethodPut {
				s.settingInvalid(w, r, f.Name, "完整保存必须提供该字段")
				return
			}
			continue
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			s.settingInvalid(w, r, f.Name, "不能为 null")
			return
		}
		var value string
		switch f.Type {
		case "boolean":
			var v bool
			err = json.Unmarshal(raw, &v)
			value = "0"
			if v {
				value = "1"
			}
		case "integer":
			var v int
			err = json.Unmarshal(raw, &v)
			value = strconv.Itoa(v)
		default:
			err = json.Unmarshal(raw, &value)
		}
		if err != nil {
			s.settingInvalid(w, r, f.Name, "字段类型不正确")
			return
		}
		values[f.LegacyName] = value
		delete(fields, f.Name)
	}
	for key := range fields {
		s.settingInvalid(w, r, key, "未知设置")
		return
	}
	s.persistSettings(w, r, version, values)
}

func (s *Server) settingsSchema(w http.ResponseWriter, r *http.Request) {
	s.respond(w, 200, map[string]any{"fields": store.SiteSettingFields(), "defaults": store.SiteSettingsDefaults(), "permission": "settings.edit", "versionRequired": true, "requiresRestart": false, "stringLengthUnit": "unicodeCodePoints", "dependencies": map[string]any{"emailVerifyEnabled": []string{"smtpEnabled"}, "requireConsent": []string{"termsContent", "privacyContent"}}})
}

func (s *Server) settingsStatus(w http.ResponseWriter, r *http.Request) {
	version, err := s.st.SettingsVersion(r.Context())
	if s.settingsError(w, r, err, true) {
		return
	}
	v, err := s.st.Settings(r.Context())
	var invalid *store.SettingsValidationError
	if err != nil && !errors.As(err, &invalid) {
		s.settingsError(w, r, err, true)
		return
	}
	issues := []*store.SettingsValidationError{}
	var effective any
	if invalid != nil {
		issues = append(issues, invalid)
	} else {
		version = v.Version
		reason := ""
		if v.EmailVerifyEnabled && !s.mailer.Enabled() {
			reason = "SMTP_DISABLED"
		}
		effective = map[string]any{"emailVerificationRequired": v.EmailVerifyEnabled && s.mailer.Enabled(), "emailVerificationInactiveReason": reason, "mailSiteName": v.SiteName, "requiresRestart": false}
	}
	s.respond(w, 200, map[string]any{"version": version, "valid": invalid == nil, "issues": issues, "effective": effective, "smtpEnabled": s.mailer.Enabled()})
}
