// api-contract generates a route-complete OpenAPI document from actual Go route
// registrations and reviewed operation/schema overrides. No runtime dependency.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type object = map[string]any
type route struct {
	Method, Path, Handler, Source string
}

func registeredRoutes(root string) ([]route, error) {
	files, err := filepath.Glob(filepath.Join(root, "internal/api/*.go"))
	if err != nil {
		return nil, err
	}
	byPattern := map[string]route{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			return nil, err
		}
		add := func(key ast.Expr, handler ast.Expr) {
			value, ok := key.(*ast.BasicLit)
			if !ok || value.Kind != token.STRING {
				return
			}
			pattern, _ := strconv.Unquote(value.Value)
			method, path, ok := strings.Cut(pattern, " ")
			if !ok || !strings.HasPrefix(path, "/api/") {
				return
			}
			h := "inline"
			if s, ok := handler.(*ast.SelectorExpr); ok {
				h = s.Sel.Name
			}
			rel, _ := filepath.Rel(root, name)
			byPattern[pattern] = route{method, path, h, filepath.ToSlash(rel)}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				if s, ok := n.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "HandleFunc" && len(n.Args) == 2 {
					add(n.Args[0], n.Args[1])
				}
			case *ast.RangeStmt:
				literal, ok := n.X.(*ast.CompositeLit)
				if !ok {
					break
				}
				m, ok := literal.Type.(*ast.MapType)
				if !ok {
					break
				}
				v, ok := m.Value.(*ast.SelectorExpr)
				if !ok || v.Sel.Name != "HandlerFunc" {
					break
				}
				key, ok := n.Key.(*ast.Ident)
				if !ok {
					break
				}
				registered := false
				ast.Inspect(n.Body, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok || len(call.Args) != 2 {
						return true
					}
					selector, method := call.Fun.(*ast.SelectorExpr)
					argument, variable := call.Args[0].(*ast.Ident)
					if method && variable && selector.Sel.Name == "HandleFunc" && argument.Name == key.Name {
						registered = true
					}
					return true
				})
				if !registered {
					break
				}
				for _, element := range literal.Elts {
					if kv, ok := element.(*ast.KeyValueExpr); ok {
						add(kv.Key, kv.Value)
					}
				}
			}
			return true
		})
	}
	routes := make([]route, 0, len(byPattern))
	for _, r := range byPattern {
		routes = append(routes, r)
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Path+routes[i].Method < routes[j].Path+routes[j].Method })
	if len(routes) == 0 {
		return nil, fmt.Errorf("no API routes found")
	}
	return routes, nil
}

var parameterRE = regexp.MustCompile(`\{([^}]+)\}`)
var operationIDRE = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func generate(root string) ([]byte, int, error) {
	routes, err := registeredRoutes(root)
	if err != nil {
		return nil, 0, err
	}
	raw, err := os.ReadFile(filepath.Join(root, "docs/openapi.overrides.json"))
	if err != nil {
		return nil, 0, err
	}
	var doc object
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, 0, err
	}
	overrides, _ := doc["paths"].(map[string]any)
	paths := object{}
	used := map[string]bool{}
	for _, r := range routes {
		method := strings.ToLower(r.Method)
		parameters := []any{}
		for _, match := range parameterRE.FindAllStringSubmatch(r.Path, -1) {
			schema := object{"type": "string"}
			if strings.HasSuffix(strings.ToLower(match[1]), "id") {
				schema["pattern"] = "^[1-9][0-9]*$"
			}
			parameters = append(parameters, object{"name": match[1], "in": "path", "required": true, "schema": schema})
		}
		write := r.Method != "GET" && r.Method != "HEAD"
		private := strings.HasPrefix(r.Path, "/api/v1/admin") || r.Path == "/api/v1/me" || strings.HasPrefix(r.Path, "/api/v1/me/") || strings.HasPrefix(r.Path, "/api/v1/conversations/") || write && !strings.HasPrefix(r.Path, "/api/v1/auth/") && r.Path != "/api/v1/setup"
		security := []any{object{}, object{"sessionCookie": []any{}}}
		if private {
			security = []any{object{"sessionCookie": []any{}}}
		}
		tag := "community"
		if strings.Contains(r.Path, "/admin") {
			tag = "admin"
		} else if strings.Contains(r.Path, "/auth/") || strings.Contains(r.Path, "/session") || strings.Contains(r.Path, "/2fa") {
			tag = "account"
		}
		op := object{
			"operationId": strings.Trim(operationIDRE.ReplaceAllString(method+"_"+r.Path, "_"), "_"),
			"summary":     r.Method + " " + r.Path, "tags": []string{tag},
			"description": "已注册接口。路由级契约；详细业务字段及权限条件见 docs/API.md 及对应专题文档。未声明的字段不代表接口接受任意输入。",
			"security":    security, "parameters": parameters, "x-contract-level": "route",
			"x-source": r.Source, "x-handler": r.Handler, "x-csrf-required": write,
			"responses": object{"2XX": object{"description": "成功响应；具体状态码由业务决定。", "content": object{"application/json": object{"schema": object{"$ref": "#/components/schemas/Envelope"}}}}, "default": object{"$ref": "#/components/responses/APIError"}},
		}
		if write {
			op["parameters"] = append(parameters, object{"$ref": "#/components/parameters/CSRF"})
			op["requestBody"] = object{"required": false, "description": "业务字段见接口专题文档；路由级声明不替代字段契约。", "content": object{"application/json": object{"schema": object{"type": "object", "additionalProperties": true}}}}
		}
		if p, ok := overrides[r.Path].(map[string]any); ok {
			if override, ok := p[method].(map[string]any); ok {
				for k, v := range override {
					if k == "parameters" {
						op[k] = append(op[k].([]any), v.([]any)...)
					} else {
						op[k] = v
					}
				}
				used[method+" "+r.Path] = true
			}
		}
		if paths[r.Path] == nil {
			paths[r.Path] = object{}
		}
		paths[r.Path].(object)[method] = op
	}
	for path, p := range overrides {
		for method := range p.(map[string]any) {
			if !used[method+" "+path] {
				return nil, 0, fmt.Errorf("contract describes an unregistered route: %s %s", method, path)
			}
		}
	}
	doc["paths"] = paths
	doc["x-route-count"] = len(routes)
	output, err := json.MarshalIndent(doc, "", "  ")
	return append(output, '\n'), len(routes), err
}

func main() {
	check := flag.Bool("check", false, "check that docs/openapi.json matches code and reviewed overrides")
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	output, count, err := generate(*root)
	if err == nil {
		path := filepath.Join(*root, "docs/openapi.json")
		if *check {
			var current []byte
			current, err = os.ReadFile(path)
			if err == nil && !bytes.Equal(current, output) {
				err = fmt.Errorf("OpenAPI is stale; run go run ./scripts/api-contract")
			}
		} else {
			err = os.WriteFile(path, output, 0o644)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("PASS: OpenAPI covers %d registered API operations\n", count)
}
