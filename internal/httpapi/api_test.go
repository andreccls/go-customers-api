package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/andreccls/go-customers-api/internal/customer"
)

func TestCustomerLifecycle(t *testing.T) {
	e := newEnv(t)

	created := e.do("POST", "/v1/customers", e.userTk, validCustomer(0))
	if created.status != http.StatusCreated {
		t.Fatalf("create: %d %s", created.status, created.body)
	}
	c := created.json(t)
	id := c["id"].(string)
	if created.header.Get("Location") != "/v1/customers/"+id || c["document"] != "52998224725" || c["status"] != "active" {
		t.Errorf("create response: %v %v", created.header, c)
	}
	if addr := c["address"].(map[string]any); addr["state"] != "MG" || addr["zip_code"] != "30130000" {
		t.Errorf("address not normalized: %v", addr)
	}

	if r := e.do("GET", "/v1/customers/"+id, e.userTk, nil); r.status != 200 || r.json(t)["email"] != "c0@example.com" {
		t.Errorf("get: %d %s", r.status, r.body)
	}

	put := validCustomer(0)
	put["name"] = "Renamed"
	delete(put, "address")
	if r := e.do("PUT", "/v1/customers/"+id, e.userTk, put); r.status != 200 || r.json(t)["name"] != "Renamed" || r.json(t)["address"] != nil {
		t.Errorf("put: %d %s", r.status, r.body)
	}

	if r := e.do("PATCH", "/v1/customers/"+id, e.userTk, map[string]any{"status": "inactive"}); r.status != 200 || r.json(t)["status"] != "inactive" || r.json(t)["name"] != "Renamed" {
		t.Errorf("patch: %d %s", r.status, r.body)
	}

	// A plain user cannot delete; an admin can; afterwards it is gone.
	e.wantProblem(e.do("DELETE", "/v1/customers/"+id, e.userTk, nil), 403, "forbidden")
	if r := e.do("DELETE", "/v1/customers/"+id, e.adminTk, nil); r.status != 204 || len(r.body) != 0 {
		t.Errorf("delete: %d %s", r.status, r.body)
	}
	e.wantProblem(e.do("GET", "/v1/customers/"+id, e.userTk, nil), 404, "customer_not_found")
	e.wantProblem(e.do("DELETE", "/v1/customers/"+id, e.adminTk, nil), 404, "customer_not_found")
}

func TestListPaginationFilterSearch(t *testing.T) {
	e := newEnv(t)
	// Distinct valid documents and e-mails for 4 customers; the last two are inactive.
	for i, doc := range []string{"52998224725", "11222333000181", "00000003700", "11144477735"} {
		in := validCustomer(i)
		in["document"] = doc
		if i >= 2 {
			in["status"] = "inactive"
		}
		if r := e.do("POST", "/v1/customers", e.userTk, in); r.status != 201 {
			t.Fatalf("seed %d: %d %s", i, r.status, r.body)
		}
	}
	list := func(q string) map[string]any {
		t.Helper()
		r := e.do("GET", "/v1/customers"+q, e.userTk, nil)
		if r.status != 200 {
			t.Fatalf("list %s: %d %s", q, r.status, r.body)
		}
		return r.json(t)
	}
	names := func(m map[string]any) []string {
		var out []string
		for _, it := range m["data"].([]any) {
			out = append(out, it.(map[string]any)["name"].(string))
		}
		return out
	}

	all := list("")
	if all["total"] != 4.0 || all["page"] != 1.0 || all["page_size"] != 20.0 {
		t.Errorf("defaults: %v", all)
	}
	if got := strings.Join(names(list("?page_size=2")), ","); got != "Customer 3,Customer 2" {
		t.Errorf("page 1 = %s (newest first)", got)
	}
	p2 := list("?page=2&page_size=2")
	if got := strings.Join(names(p2), ","); got != "Customer 1,Customer 0" || p2["page"] != 2.0 || p2["total"] != 4.0 {
		t.Errorf("page 2 = %v", p2)
	}
	if got := names(list("?status=inactive")); len(got) != 2 {
		t.Errorf("status filter: %v", got)
	}
	if got := names(list("?q=C2%40EXAMPLE")); len(got) != 1 || got[0] != "Customer 2" {
		t.Errorf("search: %v", got)
	}
	empty := list("?q=nobody")
	if d, ok := empty["data"].([]any); !ok || len(d) != 0 {
		t.Errorf("no matches must be [] not null: %v", empty["data"])
	}
}

func TestListRejectsBadParameters(t *testing.T) {
	e := newEnv(t)
	for _, q := range []string{"?page=abc", "?page_size=1000", "?page=0", "?status=banned", "?page_size=x"} {
		p := e.wantProblem(e.do("GET", "/v1/customers"+q, e.userTk, nil), 422, "validation_failed")
		if _, ok := p["errors"].([]any); !ok {
			t.Errorf("%s: missing errors list: %v", q, p)
		}
	}
}

func TestValidationAndConflicts(t *testing.T) {
	e := newEnv(t)
	if r := e.do("POST", "/v1/customers", e.userTk, validCustomer(0)); r.status != 201 {
		t.Fatal(string(r.body))
	}

	bad := map[string]any{"name": "x", "email": "nope", "document": "123"}
	p := e.wantProblem(e.do("POST", "/v1/customers", e.userTk, bad), 422, "validation_failed")
	fields := map[string]bool{}
	for _, fe := range p["errors"].([]any) {
		fields[fe.(map[string]any)["field"].(string)] = true
	}
	if !fields["name"] || !fields["email"] || !fields["document"] {
		t.Errorf("errors = %v", p["errors"])
	}

	dupEmail := validCustomer(1)
	dupEmail["email"] = "C0@example.com"
	e.wantProblem(e.do("POST", "/v1/customers", e.userTk, dupEmail), 409, "email_taken")
	dupDoc := validCustomer(1)
	dupDoc["document"] = "529.982.247-25"
	e.wantProblem(e.do("POST", "/v1/customers", e.userTk, dupDoc), 409, "document_taken")
}

func TestBodyErrors(t *testing.T) {
	e := newEnv(t)
	post := func(body string, headers ...string) resp {
		return e.do("POST", "/v1/customers", e.userTk, body, headers...)
	}
	e.wantProblem(post(`{"name":`), 400, "invalid_json")
	e.wantProblem(post(`{"name":"Maria","unknown_field":1}`), 400, "invalid_json")
	e.wantProblem(post(`{"name":123}`), 400, "invalid_json")
	e.wantProblem(post(``), 400, "invalid_json")
	e.wantProblem(post(`{"name":"Maria"} {"again":true}`), 400, "invalid_json")
	e.wantProblem(post(`{"name":"Maria"} garbage`), 400, "invalid_json")
	e.wantProblem(post(`{}`, "Content-Type", "text/plain"), 415, "unsupported_media_type")
	e.wantProblem(post(`{"name":"`+strings.Repeat("x", 2<<20)+`"}`), 413, "body_too_large")
}

func TestEveryBodyEndpointRejectsMalformedJSON(t *testing.T) {
	e := newEnv(t)
	id := "6f1b1c3e-0b0e-4a53-9a43-3f0f0c1d2e3f"
	for _, ep := range [][2]string{
		{"POST", "/v1/auth/register"}, {"POST", "/v1/auth/login"}, {"POST", "/v1/auth/refresh"}, {"POST", "/v1/auth/logout"},
		{"POST", "/v1/customers"}, {"PUT", "/v1/customers/" + id}, {"PATCH", "/v1/customers/" + id},
	} {
		e.wantProblem(e.do(ep[0], ep[1], e.userTk, `{"broken":`), 400, "invalid_json")
	}
}

func TestAuthentication(t *testing.T) {
	e := newEnv(t)
	r := e.do("GET", "/v1/customers", "", nil)
	e.wantProblem(r, 401, "missing_token")
	if r.header.Get("WWW-Authenticate") == "" {
		t.Error("401 must carry WWW-Authenticate")
	}
	e.wantProblem(e.do("GET", "/v1/customers", "garbage.token.value", nil), 401, "invalid_token")
	e.wantProblem(e.do("GET", "/v1/customers", "", nil, "Authorization", "Basic abc"), 401, "missing_token")
	e.wantProblem(e.do("POST", "/v1/customers", "", validCustomer(0)), 401, "missing_token")
}

func TestAuthFlowOverHTTP(t *testing.T) {
	e := newEnv(t, func(o *options) { o.skipSeeding = true })

	reg := e.do("POST", "/v1/auth/register", "", map[string]any{"email": "New@Example.com", "password": "long-enough-pass"})
	if reg.status != 201 || reg.json(t)["role"] != "user" || reg.json(t)["email"] != "new@example.com" {
		t.Fatalf("register: %d %s", reg.status, reg.body)
	}
	if strings.Contains(string(reg.body), "password") {
		t.Error("register response must not echo password material")
	}
	e.wantProblem(e.do("POST", "/v1/auth/register", "", map[string]any{"email": "new@example.com", "password": "long-enough-pass"}), 409, "email_taken")
	e.wantProblem(e.do("POST", "/v1/auth/register", "", map[string]any{"email": "bad", "password": "short"}), 422, "validation_failed")
	e.wantProblem(e.do("POST", "/v1/auth/login", "", map[string]any{"email": "new@example.com", "password": "wrong-password"}), 401, "invalid_credentials")

	login := e.do("POST", "/v1/auth/login", "", map[string]any{"email": "new@example.com", "password": "long-enough-pass"})
	if login.status != 200 {
		t.Fatalf("login: %d %s", login.status, login.body)
	}
	tok := login.json(t)
	if tok["token_type"] != "Bearer" || tok["expires_in"] != 900.0 {
		t.Errorf("token response: %v", tok)
	}
	access, refresh := tok["access_token"].(string), tok["refresh_token"].(string)
	if r := e.do("GET", "/v1/customers", access, nil); r.status != 200 {
		t.Errorf("access token rejected: %d", r.status)
	}

	rotated := e.do("POST", "/v1/auth/refresh", "", map[string]any{"refresh_token": refresh})
	if rotated.status != 200 || rotated.json(t)["refresh_token"] == refresh {
		t.Fatalf("refresh: %d %s", rotated.status, rotated.body)
	}
	e.wantProblem(e.do("POST", "/v1/auth/refresh", "", map[string]any{"refresh_token": refresh}), 401, "invalid_token")
	// The replay above revoked the family: the rotated token is dead too.
	e.wantProblem(e.do("POST", "/v1/auth/refresh", "", map[string]any{"refresh_token": rotated.json(t)["refresh_token"]}), 401, "invalid_token")

	fresh := e.do("POST", "/v1/auth/login", "", map[string]any{"email": "new@example.com", "password": "long-enough-pass"}).json(t)
	if r := e.do("POST", "/v1/auth/logout", "", map[string]any{"refresh_token": fresh["refresh_token"]}); r.status != 204 {
		t.Errorf("logout: %d", r.status)
	}
	e.wantProblem(e.do("POST", "/v1/auth/refresh", "", map[string]any{"refresh_token": fresh["refresh_token"]}), 401, "invalid_token")
}

func TestRateLimiting(t *testing.T) {
	t.Run("auth endpoints per IP", func(t *testing.T) {
		e := newEnv(t, func(o *options) { o.authLimit = 3 })
		for i := 0; i < 3; i++ {
			e.do("POST", "/v1/auth/login", "", map[string]any{"email": "x@example.com", "password": "whatever-pass"})
		}
		r := e.do("POST", "/v1/auth/login", "", map[string]any{"email": "x@example.com", "password": "whatever-pass"})
		e.wantProblem(r, 429, "rate_limited")
		if r.header.Get("Retry-After") == "" {
			t.Error("429 must carry Retry-After")
		}
	})
	t.Run("API per user, independent between users", func(t *testing.T) {
		e := newEnv(t, func(o *options) { o.apiLimit = 2 })
		for i := 0; i < 2; i++ {
			if r := e.do("GET", "/v1/customers", e.userTk, nil); r.status != 200 {
				t.Fatalf("request %d: %d", i, r.status)
			}
		}
		e.wantProblem(e.do("GET", "/v1/customers", e.userTk, nil), 429, "rate_limited")
		if r := e.do("GET", "/v1/customers", e.adminTk, nil); r.status != 200 {
			t.Errorf("another user must have its own budget: %d", r.status)
		}
	})
}

// brokenCustomers fails every call with a non-domain error.
type brokenCustomers struct {
	httpapiCustomers
	err error
}

type httpapiCustomers interface {
	Create(context.Context, customer.Input) (customer.Customer, error)
	Get(context.Context, string) (customer.Customer, error)
	List(context.Context, customer.Filter) ([]customer.Customer, int, error)
	Replace(context.Context, string, customer.Input) (customer.Customer, error)
	Update(context.Context, string, customer.Patch) (customer.Customer, error)
	Delete(context.Context, string) error
}

func (b brokenCustomers) List(context.Context, customer.Filter) ([]customer.Customer, int, error) {
	return nil, 0, b.err
}

func TestUnexpectedErrorsAreHidden(t *testing.T) {
	e := newEnv(t, func(o *options) {
		o.customers = brokenCustomers{err: errors.New("pq: password authentication failed for user secret")}
	})
	r := e.do("GET", "/v1/customers", e.userTk, nil)
	p := e.wantProblem(r, 500, "internal_error")
	if strings.Contains(string(r.body), "password authentication") {
		t.Errorf("internal error leaked to the client: %v", p)
	}
	if !strings.Contains(e.logs.String(), "password authentication failed") {
		t.Error("the real cause must be logged")
	}
}

func TestTimeoutMapsTo503(t *testing.T) {
	e := newEnv(t, func(o *options) { o.customers = brokenCustomers{err: context.DeadlineExceeded} })
	e.wantProblem(e.do("GET", "/v1/customers", e.userTk, nil), 503, "timeout")
}

func TestPanicIsRecovered(t *testing.T) {
	// A nil CustomerService makes the handler panic (nil dereference).
	e := newEnv(t, func(o *options) { o.customers = brokenCustomers{} })
	e.wantProblem(e.do("GET", "/v1/customers/"+"6f1b1c3e-0b0e-4a53-9a43-3f0f0c1d2e3f", e.userTk, nil), 500, "internal_error")
	if !strings.Contains(e.logs.String(), "panic recovered") {
		t.Error("panic must be logged")
	}
	// The server keeps serving afterwards.
	if r := e.do("GET", "/healthz", "", nil); r.status != 200 {
		t.Errorf("healthz after panic: %d", r.status)
	}
}

func TestRequestID(t *testing.T) {
	e := newEnv(t)
	generated := e.do("GET", "/healthz", "", nil).header.Get("X-Request-ID")
	if len(generated) != 36 {
		t.Errorf("generated id = %q", generated)
	}
	if got := e.do("GET", "/healthz", "", nil, "X-Request-ID", "client-id.123").header.Get("X-Request-ID"); got != "client-id.123" {
		t.Errorf("client id not kept: %q", got)
	}
	for _, evil := range []string{"has space", "new\nline", strings.Repeat("a", 65), "<script>"} {
		if got := e.do("GET", "/healthz", "", nil, "X-Request-ID", evil).header.Get("X-Request-ID"); got == evil {
			t.Errorf("unsafe id %q was echoed", evil)
		}
	}
	// The same id is in the problem body and in the access log.
	p := e.wantProblem(e.do("GET", "/v1/customers", "", nil, "X-Request-ID", "trace-me"), 401, "missing_token")
	if p["request_id"] != "trace-me" {
		t.Errorf("problem request_id = %v", p["request_id"])
	}
	if !strings.Contains(e.logs.String(), `"request_id":"trace-me"`) || !strings.Contains(e.logs.String(), `"route":"GET /v1/customers"`) {
		t.Errorf("access log lacks request_id/route:\n%s", e.logs.String())
	}
}

func TestAccessLogHasUser(t *testing.T) {
	e := newEnv(t)
	e.do("GET", "/v1/customers", e.userTk, nil)
	if !strings.Contains(e.logs.String(), `"status":200`) || !strings.Contains(e.logs.String(), `"user_id":"`) {
		t.Errorf("log = %s", e.logs.String())
	}
}

func TestHealthAndReadiness(t *testing.T) {
	e := newEnv(t)
	if r := e.do("GET", "/healthz", "", nil); r.status != 200 || r.json(t)["status"] != "ok" {
		t.Errorf("healthz: %d %s", r.status, r.body)
	}
	if r := e.do("GET", "/readyz", "", nil); r.status != 200 || r.json(t)["status"] != "ready" {
		t.Errorf("readyz: %d %s", r.status, r.body)
	}
	down := newEnv(t, func(o *options) { o.ready = func(context.Context) error { return errors.New("db down") } })
	r := down.do("GET", "/readyz", "", nil)
	down.wantProblem(r, 503, "not_ready")
	if strings.Contains(string(r.body), "db down") {
		t.Error("readiness must not leak the cause")
	}
}

func TestUnknownRouteIsProblemJSON(t *testing.T) {
	e := newEnv(t)
	e.wantProblem(e.do("GET", "/nope", "", nil), 404, "route_not_found")
}

func TestDocsAreServedFromTheBinary(t *testing.T) {
	e := newEnv(t)
	spec := e.do("GET", "/openapi.json", "", nil)
	if spec.status != 200 || spec.header.Get("Content-Type") != "application/json" || spec.json(t)["openapi"] != "3.0.3" {
		t.Errorf("openapi.json: %d %s", spec.status, spec.header.Get("Content-Type"))
	}
	page := e.do("GET", "/docs/", "", nil)
	if page.status != 200 || !strings.Contains(string(page.body), "swagger-ui-bundle.js") {
		t.Errorf("docs page: %d", page.status)
	}
	if csp := page.header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP = %q", csp)
	}
	for _, f := range []string{"swagger-ui-bundle.js", "swagger-ui.css", "init.js"} {
		if r := e.do("GET", "/docs/"+f, "", nil); r.status != 200 || len(r.body) == 0 {
			t.Errorf("%s: %d", f, r.status)
		}
	}
	if r := e.do("GET", "/docs", "", nil); r.status != http.StatusMovedPermanently {
		t.Errorf("/docs should redirect to /docs/, got %d", r.status)
	}
	if strings.Contains(string(page.body), "http://") || strings.Contains(string(page.body), "https://") {
		t.Error("docs page must not reference external hosts")
	}
}
