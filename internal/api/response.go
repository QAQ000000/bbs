// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type actionResult struct {
	Message string `json:"message,omitempty"`
}

func (s *Server) respond(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, map[string]any{"data": data})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		status = http.StatusInternalServerError
		body = []byte(`{"error":{"code":"INTERNAL_ERROR","message":"服务器内部错误"}}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// fail never sends implementation errors (SQL, credentials or paths) to clients.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	if status >= 500 && code != "SITE_CLOSED" {
		s.log.Error("api error", "path", r.URL.Path, "err", message)
		message = "服务器暂时不可用，请稍后重试"
	}
	if strings.HasPrefix(message, `{"error":`) {
		var old struct {
			Error string `json:"error"`
		}
		if json.Unmarshal([]byte(message), &old) == nil {
			message = old.Error
		}
	}
	if code == "" || strings.IndexFunc(code, func(c rune) bool { return !(c == '_' || c >= 'A' && c <= 'Z') }) >= 0 {
		code = map[int]string{400: "BAD_REQUEST", 401: "UNAUTHENTICATED", 403: "FORBIDDEN", 404: "NOT_FOUND", 405: "METHOD_NOT_ALLOWED", 409: "CONFLICT", 413: "PAYLOAD_TOO_LARGE", 415: "UNSUPPORTED_MEDIA_TYPE", 422: "VALIDATION_FAILED", 429: "RATE_LIMITED"}[status]
		if code == "" {
			code = "INTERNAL_ERROR"
		}
	}
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func (s *Server) requireLogin(w http.ResponseWriter, r *http.Request) bool {
	if User(r) != nil {
		return true
	}
	s.fail(w, r, 401, "UNAUTHENTICATED", "请先登录")
	return false
}
func idString(id int64) string { return strconv.FormatInt(id, 10) }
func pathID(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n
}

// action accepts JSON using the existing action field names, or form data during
// migration. Arrays represent repeated fields such as tids[]. Business handlers
// share their existing validation rather than maintaining a second write path.
func (s *Server) action(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if User(r) == nil && !strings.HasPrefix(r.URL.Path, "/api/v1/auth/") && r.URL.Path != "/api/v1/setup" {
			s.fail(w, r, 401, "UNAUTHENTICATED", "请先登录")
			return
		}
		ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if r.Header.Get("Content-Type") != "" && err != nil {
			s.fail(w, r, 415, "UNSUPPORTED_MEDIA_TYPE", "无效 Content-Type")
			return
		}
		if ct == "application/json" {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			dec := json.NewDecoder(r.Body)
			dec.UseNumber()
			var fields map[string]json.RawMessage
			if err := dec.Decode(&fields); err != nil {
				status := 400
				var max *http.MaxBytesError
				if errors.As(err, &max) {
					status = 413
				}
				s.fail(w, r, status, "", "无效或过大的 JSON 请求")
				return
			}
			if fields == nil {
				s.fail(w, r, 400, "BAD_REQUEST", "请求必须是 JSON 对象")
				return
			}
			if dec.Decode(new(any)) != io.EOF {
				s.fail(w, r, 400, "BAD_REQUEST", "请求只能包含一个 JSON 对象")
				return
			}
			form := url.Values{}
			for key, raw := range fields {
				var list []json.RawMessage
				if len(raw) > 0 && raw[0] == '[' {
					if json.Unmarshal(raw, &list) != nil {
						s.fail(w, r, 400, "BAD_REQUEST", "无效数组")
						return
					}
				} else {
					list = []json.RawMessage{raw}
				}
				for _, v := range list {
					var value string
					if len(v) > 0 && v[0] == '"' {
						if json.Unmarshal(v, &value) != nil {
							s.fail(w, r, 400, "BAD_REQUEST", "无效字段")
							return
						}
					} else if bytes.Equal(v, []byte("true")) {
						value = "1"
					} else if bytes.Equal(v, []byte("false")) {
						value = "0"
					} else {
						var n json.Number
						if json.Unmarshal(v, &n) != nil || string(v) == "null" {
							s.fail(w, r, 400, "BAD_REQUEST", "字段必须为字符串、数字或布尔值")
							return
						}
						value = n.String()
					}
					form.Add(key, value)
				}
			}
			r.PostForm = form
			r.Form = url.Values{}
			for k, v := range form {
				r.Form[k] = v
			}
			for k, v := range r.URL.Query() {
				r.Form[k] = append(r.Form[k], v...)
			}
		} else if ct == "application/x-www-form-urlencoded" || ct == "" {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			if err := r.ParseForm(); err != nil {
				s.fail(w, r, 400, "BAD_REQUEST", "无效表单")
				return
			}
		} else if ct != "multipart/form-data" {
			s.fail(w, r, 415, "UNSUPPORTED_MEDIA_TYPE", "使用 JSON、表单或 multipart 上传")
			return
		}
		// For multipart the handler parses the bounded upload before its own CSRF check.
		if ct != "multipart/form-data" && !s.checkCSRF(r) {
			s.fail(w, r, 403, "CSRF_INVALID", "CSRF token 无效或已过期")
			return
		}
		s.memberAction(w, r, h)
	}
}
