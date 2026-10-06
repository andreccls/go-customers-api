package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/andreccls/go-customers-api/internal/auth"
	"github.com/andreccls/go-customers-api/internal/customer"
	"github.com/andreccls/go-customers-api/internal/httpapi"
	"github.com/andreccls/go-customers-api/internal/memstore"
	"github.com/andreccls/go-customers-api/internal/ratelimit"
)

const (
	adminEmail = "admin@example.com"
	adminPass  = "admin-pass-1234"
	userEmail  = "maria@example.com"
	userPass   = "user-pass-1234"
)

// specIndex lets every request made by the tests be checked against openapi.json:
// a status code the spec does not declare for that operation fails the test.
type specIndex struct {
	ops map[string]map[string]bool // "METHOD /v1/customers/{id}" -> declared statuses
	re  map[string]*regexp.Regexp  // same key -> matcher for concrete paths
}

func loadSpec(t testing.TB) specIndex {
	t.Helper()
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(httpapi.OpenAPI(), &doc); err != nil {
		t.Fatalf("openapi.json is not valid JSON: %v", err)
	}
	idx := specIndex{ops: map[string]map[string]bool{}, re: map[string]*regexp.Regexp{}}
	for path, item := range doc.Paths {
		for method, raw := range item {
			if method == "parameters" {
				continue
			}
			var op struct {
				Responses map[string]json.RawMessage `json:"responses"`
			}
			if err := json.Unmarshal(raw, &op); err != nil {
				t.Fatal(err)
			}
			key := strings.ToUpper(method) + " " + path
			idx.ops[key] = map[string]bool{}
			for code := range op.Responses {
				idx.ops[key][code] = true
			}
			idx.re[key] = regexp.MustCompile("^" + regexp.MustCompile(`\{[^/]+\}`).ReplaceAllString(regexp.QuoteMeta(path), `[^/]+`) + "$")
		}
	}
	return idx
}

// check reports a status the spec does not document for the request.
func (s specIndex) check(t testing.TB, method, path string, status int) {
	t.Helper()
	path, _, _ = strings.Cut(path, "?")
	for key, re := range s.re {
		if strings.HasPrefix(key, method+" ") && re.MatchString(path) {
			if !s.ops[key][strconv.Itoa(status)] {
				t.Errorf("%s %s answered %d, which openapi.json does not declare for %s", method, path, status, key)
			}
			return
		}
	}
}

type env struct {
	t       *testing.T
	h       http.Handler
	spec    specIndex
	logs    *bytes.Buffer
	auth    *auth.Service
	adminTk string
	userTk  string
}

type options struct {
	customers    httpapi.CustomerService
	authLimit    int
	apiLimit     int
	ready        func(context.Context) error
	timeout      time.Duration
	skipSeeding  bool
	authOverride httpapi.AuthService
}

func newEnv(t *testing.T, mods ...func(*options)) *env {
	t.Helper()
	o := options{authLimit: 1000, apiLimit: 1000, ready: func(context.Context) error { return nil }, timeout: 5 * time.Second}
	for _, m := range mods {
		m(&o)
	}
	authSvc := auth.NewService(memstore.NewUsers(), auth.Config{
		Secret: []byte("integration-test-secret-0123456789abcdef"), Issuer: "test",
		AccessTTL: 15 * time.Minute, RefreshTTL: time.Hour, BcryptCost: bcrypt.MinCost,
	}, nil)
	var customers httpapi.CustomerService = customer.NewService(memstore.NewCustomers(), nil)
	if o.customers != nil {
		customers = o.customers
	}
	var authAPI httpapi.AuthService = authSvc
	if o.authOverride != nil {
		authAPI = o.authOverride
	}
	logs := &bytes.Buffer{}
	e := &env{t: t, spec: loadSpec(t), logs: logs, auth: authSvc}
	e.h = httpapi.New(httpapi.Deps{
		Customers: customers, Auth: authAPI, Ready: o.ready,
		Logger:         slog.New(slog.NewJSONHandler(logs, nil)),
		AuthLimiter:    ratelimit.New(o.authLimit, nil),
		APILimiter:     ratelimit.New(o.apiLimit, nil),
		RequestTimeout: o.timeout,
	})
	if !o.skipSeeding {
		ctx := context.Background()
		if err := authSvc.EnsureAdmin(ctx, adminEmail, adminPass); err != nil {
			t.Fatal(err)
		}
		if _, err := authSvc.Register(ctx, userEmail, userPass); err != nil {
			t.Fatal(err)
		}
		a, _ := authSvc.Login(ctx, adminEmail, adminPass)
		u, _ := authSvc.Login(ctx, userEmail, userPass)
		e.adminTk, e.userTk = a.AccessToken, u.AccessToken
	}
	return e
}

type resp struct {
	status int
	header http.Header
	body   []byte
}

// json decodes the body into a generic map.
func (r resp) json(t testing.TB) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		t.Fatalf("body is not a JSON object: %q", r.body)
	}
	return m
}

// do sends a request; body may be a string (raw), nil, or any JSON-encodable value.
func (e *env) do(method, path, token string, body any, headers ...string) resp {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	e.spec.check(e.t, method, path, rec.Code)
	return resp{status: rec.Code, header: rec.Header(), body: rec.Body.Bytes()}
}

// wantProblem asserts an RFC 9457 response with the given status and code.
func (e *env) wantProblem(r resp, status int, code string) map[string]any {
	e.t.Helper()
	if r.status != status {
		e.t.Fatalf("status = %d, want %d (body %s)", r.status, status, r.body)
	}
	if ct := r.header.Get("Content-Type"); ct != "application/problem+json" {
		e.t.Errorf("Content-Type = %q", ct)
	}
	p := r.json(e.t)
	if p["code"] != code || p["type"] != "about:blank" || int(p["status"].(float64)) != status || p["title"] != http.StatusText(status) {
		e.t.Errorf("problem = %v, want code %q", p, code)
	}
	return p
}

func validCustomer(n int) map[string]any {
	docs := []string{"52998224725", "11222333000181", "00000003700", "11144477735"}
	return map[string]any{
		"name": "Customer " + strconv.Itoa(n), "email": "c" + strconv.Itoa(n) + "@example.com", "document": docs[n%len(docs)],
		"phone":   "(31) 99999-0000",
		"address": map[string]any{"street": "Rua A", "number": "10", "city": "Belo Horizonte", "state": "mg", "zip_code": "30130-000"},
	}
}
