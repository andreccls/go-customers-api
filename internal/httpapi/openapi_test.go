package httpapi_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/andreccls/go-customers-api/internal/httpapi"
)

// TestOpenAPIMatchesRouter keeps docs and code in lock-step. It is deterministic
// (no database, no .env): it compares the router's own route table with the
// embedded openapi.json.
func TestOpenAPIMatchesRouter(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	type operation struct {
		Security    []map[string][]string      `json:"security"`
		RequestBody json.RawMessage            `json:"requestBody"`
		Responses   map[string]json.RawMessage `json:"responses"`
	}
	if err := json.Unmarshal(httpapi.OpenAPI(), &doc); err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	for _, op := range httpapi.Operations() {
		key := op.Method + " " + op.Path
		seen[key] = true
		raw, ok := doc.Paths[op.Path][strings.ToLower(op.Method)]
		if !ok {
			t.Errorf("%s is served but missing from openapi.json", key)
			continue
		}
		var spec operation
		if err := json.Unmarshal(raw, &spec); err != nil {
			t.Fatal(err)
		}
		has := func(code string) bool { _, ok := spec.Responses[code]; return ok }

		if secured := len(spec.Security) > 0; secured != (op.Access != httpapi.Public) {
			t.Errorf("%s: security in spec = %v, but route access = %v", key, secured, op.Access)
		}
		if op.Access != httpapi.Public && !has("401") {
			t.Errorf("%s: authenticated route must document 401", key)
		}
		if has("403") != (op.Access == httpapi.AdminOnly) {
			t.Errorf("%s: 403 documented = %v, admin-only = %v", key, has("403"), op.Access == httpapi.AdminOnly)
		}
		if op.RateLimited && !has("429") {
			t.Errorf("%s: rate-limited route must document 429", key)
		}
		hasBody := len(spec.RequestBody) > 0
		if wantBody := op.Method == "POST" && strings.HasPrefix(op.Path, "/v1/") || op.Method == "PUT" || op.Method == "PATCH"; hasBody != wantBody {
			t.Errorf("%s: request body documented = %v, expected %v", key, hasBody, wantBody)
		}
		if hasBody && !(has("400") && has("413") && has("415")) {
			t.Errorf("%s: body endpoints must document 400, 413 and 415", key)
		}
	}
	for path, item := range doc.Paths {
		for method := range item {
			if method == "parameters" {
				continue
			}
			if key := strings.ToUpper(method) + " " + path; !seen[key] {
				t.Errorf("%s is in openapi.json but the router does not serve it", key)
			}
		}
	}
}

// TestOpenAPIReferencesResolve fails on a $ref that points nowhere.
func TestOpenAPIReferencesResolve(t *testing.T) {
	var root map[string]any
	if err := json.Unmarshal(httpapi.OpenAPI(), &root); err != nil {
		t.Fatal(err)
	}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if ref, ok := x["$ref"].(string); ok {
				cur := any(root)
				for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
					m, ok := cur.(map[string]any)
					if !ok || m[part] == nil {
						t.Errorf("unresolved $ref %s", ref)
						return
					}
					cur = m[part]
				}
			}
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(root)
}
