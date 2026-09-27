package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func contractFixture(t *testing.T, source string) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"internal/api", "docs"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "internal/api/routes.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRegisteredRoutesIgnoreUnregisteredData(t *testing.T) {
	root := contractFixture(t, `package api
import "net/http"
var ignored = map[string]http.HandlerFunc{"GET /api/unregistered": nil}
func routes(m *http.ServeMux) {
 m.HandleFunc("GET /api/direct", nil)
 m.HandleFunc("GET /avatar/{id}", nil)
 for pattern, h := range map[string]http.HandlerFunc{"POST /api/mapped": nil} { m.HandleFunc(pattern, h) }
 for pattern := range map[string]http.HandlerFunc{"GET /api/unused": nil} { _ = pattern }
}`)
	routes, err := registeredRoutes(root)
	if err != nil || len(routes) != 2 {
		t.Fatalf("routes=%v err=%v", routes, err)
	}
	if routes[0].Path != "/api/direct" || routes[1].Path != "/api/mapped" {
		t.Fatalf("unexpected routes: %v", routes)
	}
}

func TestContractRejectsRemovedRouteOverride(t *testing.T) {
	root := contractFixture(t, `package api; func routes() { m.HandleFunc("GET /api/present", h) }`)
	if err := os.WriteFile(filepath.Join(root, "docs/openapi.overrides.json"), []byte(`{"paths":{"/api/removed":{"get":{}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := generate(root); err == nil || !strings.Contains(err.Error(), "unregistered route") {
		t.Fatalf("stale override accepted: %v", err)
	}
}

func TestCommittedContractMatchesSourceAndReferences(t *testing.T) {
	root := filepath.Join("..", "..")
	generated, count, err := generate(root)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(filepath.Join(root, "docs/openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, committed) {
		t.Fatal("OpenAPI is stale; run go run ./scripts/api-contract")
	}
	var doc object
	if err := json.Unmarshal(generated, &doc); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok {
				if !strings.HasPrefix(ref, "#/") {
					t.Fatalf("non-local reference %s", ref)
				}
				var target any = doc
				for _, key := range strings.Split(ref[2:], "/") {
					key = strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
					m, ok := target.(map[string]any)
					if !ok || m[key] == nil {
						t.Fatalf("unresolved reference %s", ref)
					}
					target = m[key]
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(doc)
	ids := map[string]bool{}
	for path, item := range doc["paths"].(map[string]any) {
		for method, value := range item.(map[string]any) {
			op := value.(map[string]any)
			id, _ := op["operationId"].(string)
			if id == "" || ids[id] {
				t.Fatalf("missing or duplicate operation ID: %s %s", method, path)
			}
			ids[id] = true
			parameters := map[string]bool{}
			for _, parameter := range op["parameters"].([]any) {
				p := parameter.(map[string]any)
				if p["in"] == "path" && p["required"] == true {
					parameters[p["name"].(string)] = true
				}
			}
			for _, p := range parameterRE.FindAllStringSubmatch(path, -1) {
				if !parameters[p[1]] {
					t.Fatalf("missing path parameter %s in %s", p[1], path)
				}
			}
		}
	}
	if len(ids) != count || count == 0 {
		t.Fatalf("operations=%d routes=%d", len(ids), count)
	}
}
