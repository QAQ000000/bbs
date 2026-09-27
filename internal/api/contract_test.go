package api

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

type contractObject = map[string]any

func loadAPIContract(t *testing.T) contractObject {
	t.Helper()
	raw, err := os.ReadFile("../../docs/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc contractObject
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestOpenAPIRegisteredOperations(t *testing.T) {
	doc := loadAPIContract(t)
	s := &Server{mux: http.NewServeMux()}
	s.routes()
	placeholder := regexp.MustCompile(`{[^}]+}`)
	for path, methods := range doc["paths"].(contractObject) {
		for method := range methods.(contractObject) {
			req := httptest.NewRequest(strings.ToUpper(method), placeholder.ReplaceAllString(path, "1"), nil)
			_, pattern := s.mux.Handler(req)
			if want := strings.ToUpper(method) + " " + path; pattern != want {
				t.Errorf("contract %s resolves to %q", want, pattern)
			}
		}
	}
}

func resolveContract(doc, schema contractObject) contractObject {
	for {
		ref, ok := schema["$ref"].(string)
		if !ok {
			return schema
		}
		var v any = doc
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			v = v.(contractObject)[part]
		}
		schema = v.(contractObject)
	}
}

// Checks the response-shape subset used below, not the entire OpenAPI standard.
// Requests/business constraints remain covered by dedicated API regressions.
func checkContractShape(doc, schema contractObject, value any, path string) error {
	schema = resolveContract(doc, schema)
	if value == nil && schema["nullable"] == true {
		return nil
	}
	if components, ok := schema["allOf"].([]any); ok {
		for _, component := range components {
			if err := checkContractShape(doc, component.(contractObject), value, path); err != nil {
				return err
			}
		}
	}
	if alternatives, ok := schema["oneOf"].([]any); ok {
		matches := 0
		for _, alternative := range alternatives {
			if checkContractShape(doc, alternative.(contractObject), value, path) == nil {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("%s: oneOf matched %d branches", path, matches)
		}
	}
	if values, ok := schema["enum"].([]any); ok {
		matched := false
		for _, candidate := range values {
			matched = matched || reflect.DeepEqual(value, candidate)
		}
		if !matched {
			return fmt.Errorf("%s: unexpected enum value", path)
		}
	}
	wrong := func() error { return fmt.Errorf("%s: got %T, expected %v", path, value, schema["type"]) }
	switch schema["type"] {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return wrong()
		}
		if required, ok := schema["required"].([]any); ok {
			for _, key := range required {
				if _, ok := obj[key.(string)]; !ok {
					return fmt.Errorf("%s: missing %s", path, key)
				}
			}
		}
		properties, _ := schema["properties"].(contractObject)
		for key, child := range obj {
			rule, declared := properties[key].(contractObject)
			if !declared {
				rule, declared = schema["additionalProperties"].(contractObject)
				if schema["additionalProperties"] == false {
					return fmt.Errorf("%s: unexpected %s", path, key)
				}
			}
			if declared {
				if err := checkContractShape(doc, rule, child, path+"."+key); err != nil {
					return err
				}
			}
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return wrong()
		}
		for i, child := range items {
			if err := checkContractShape(doc, schema["items"].(contractObject), child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string":
		s, ok := value.(string)
		if !ok {
			return wrong()
		}
		if pattern, ok := schema["pattern"].(string); ok && !regexp.MustCompile(pattern).MatchString(s) {
			return fmt.Errorf("%s: pattern mismatch", path)
		}
		if schema["format"] == "date-time" {
			if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
				return fmt.Errorf("%s: invalid timestamp", path)
			}
		}
	case "integer", "number":
		n, ok := value.(float64)
		if !ok || schema["type"] == "integer" && math.Trunc(n) != n {
			return wrong()
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return wrong()
		}
	}
	return nil
}

func assertContractResponse(t *testing.T, doc contractObject, method, path string, rec *httptest.ResponseRecorder, wantStatus int) contractObject {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, rec.Code, wantStatus, rec.Body.String())
	}
	op := doc["paths"].(contractObject)[path].(contractObject)[strings.ToLower(method)].(contractObject)
	responses := op["responses"].(contractObject)
	response, ok := responses[strconv.Itoa(rec.Code)].(contractObject)
	if !ok {
		response = responses["default"].(contractObject)
	}
	response = resolveContract(doc, response)
	schema := response["content"].(contractObject)["application/json"].(contractObject)["schema"].(contractObject)
	var body contractObject
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if err := checkContractShape(doc, schema, body, "response"); err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return body
}

func TestOpenAPICoreResponses(t *testing.T) {
	requireDB(t)
	doc := loadAPIContract(t)
	_, cookie, csrf := memberTestUser(t)
	for _, tc := range []struct {
		path, route string
		cookie      *http.Cookie
		status      int
	}{
		{"/api/v1/session", "/api/v1/session", nil, 200},
		{"/api/v1/session", "/api/v1/session", cookie, 200},
		{"/api/v1/me", "/api/v1/me", cookie, 200},
		{"/api/v1/me", "/api/v1/me", nil, 401},
		{"/api/v1/threads", "/api/v1/threads", nil, 200},
		{"/api/v1/threads?pagination=cursor", "/api/v1/threads", cookie, 200},
		{"/api/v1/threads/1", "/api/v1/threads/{tid}", nil, 200},
		{"/api/v1/threads/1/posts", "/api/v1/threads/{tid}/posts", cookie, 200},
		{"/api/v1/posts/1", "/api/v1/posts/{pid}", nil, 200},
		{"/api/v1/posts/0", "/api/v1/posts/{pid}", nil, 404},
		{"/api/v1/search?q=smoke", "/api/v1/search", cookie, 200},
		{"/api/v1/me/notifications/summary", "/api/v1/me/notifications/summary", cookie, 200},
		{"/api/v1/health/live", "/api/v1/health/live", nil, 200},
	} {
		t.Run(tc.path+"-"+strconv.Itoa(tc.status), func(t *testing.T) {
			assertContractResponse(t, doc, "GET", tc.route, smokeGet(t, tc.path, tc.cookie), tc.status)
		})
	}
	rec := memberJSON(t, "POST", "/api/v1/threads", map[string]any{"forumId": "1", "subject": "contract topic", "content": "contract body"}, csrf, cookie)
	body := assertContractResponse(t, doc, "POST", "/api/v1/threads", rec, 201)["data"].(contractObject)
	tid, pid := body["threadId"].(string), body["postId"].(string)
	rec = memberJSON(t, "PATCH", "/api/v1/posts/"+pid, map[string]any{"subject": "contract edited", "content": "updated body", "version": body["version"]}, csrf, cookie)
	assertContractResponse(t, doc, "PATCH", "/api/v1/posts/{pid}", rec, 200)
	rec = memberJSON(t, "POST", "/api/v1/threads/"+tid+"/posts", map[string]any{"content": "contract reply", "replyToPostId": pid}, csrf, cookie)
	assertContractResponse(t, doc, "POST", "/api/v1/threads/{tid}/posts", rec, 201)
}

func TestContractShapeRejectsIDAndRequiredFieldDrift(t *testing.T) {
	doc := loadAPIContract(t)
	schema := contractObject{"$ref": "#/components/schemas/ContentWrite"}
	for _, bad := range []any{map[string]any{}, map[string]any{"threadId": float64(1), "postId": "2", "pending": false, "version": float64(1)}} {
		if err := checkContractShape(doc, schema, bad, "test"); err == nil {
			t.Fatal("invalid response accepted")
		}
	}
	op := doc["paths"].(contractObject)["/api/v1/threads"].(contractObject)["post"].(contractObject)
	wrapped := op["responses"].(contractObject)["201"].(contractObject)["content"].(contractObject)["application/json"].(contractObject)["schema"].(contractObject)
	if err := checkContractShape(doc, wrapped, map[string]any{"data": map[string]any{}}, "wrapped"); err == nil {
		t.Fatal("allOf envelope hid missing content fields")
	}
}
