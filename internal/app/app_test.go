package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/andreccls/go-customers-api/internal/app"
	"github.com/andreccls/go-customers-api/internal/config"
	"github.com/andreccls/go-customers-api/internal/testdb"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		Env: config.EnvDevelopment, DatabaseURL: testdb.URL(t), JWTSecret: "app-test-secret-app-test-secret-0123",
		AccessTTL: time.Minute, RefreshTTL: time.Hour, AuthRatePerMin: 1000, APIRatePerMin: 1000,
		AdminEmail: "admin@example.com", AdminPassword: "admin-pass-1234",
	}
}

type client struct {
	t    *testing.T
	base string
}

func (c client) call(method, path, token string, body any) (int, map[string]any) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// TestEndToEndAgainstPostgres drives the whole stack (HTTP -> services -> pgx -> PostgreSQL).
func TestEndToEndAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	a, err := app.New(ctx, testConfig(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	c := client{t, srv.URL}

	if st, _ := c.call("GET", "/readyz", "", nil); st != 200 {
		t.Fatalf("readyz = %d", st)
	}

	// A normal user and the bootstrap admin (seeded by app.New from the config).
	if st, _ := c.call("POST", "/v1/auth/register", "", map[string]any{"email": "maria@example.com", "password": "user-pass-1234"}); st != 201 {
		t.Fatalf("register = %d", st)
	}
	_, ut := c.call("POST", "/v1/auth/login", "", map[string]any{"email": "maria@example.com", "password": "user-pass-1234"})
	_, at := c.call("POST", "/v1/auth/login", "", map[string]any{"email": "admin@example.com", "password": "admin-pass-1234"})
	user, admin := ut["access_token"].(string), at["access_token"].(string)

	in := map[string]any{"name": "Maria Silva", "email": "maria.s@example.com", "document": "529.982.247-25",
		"address": map[string]any{"street": "Rua A", "number": "1", "city": "BH", "state": "MG", "zip_code": "30130000"}}
	st, created := c.call("POST", "/v1/customers", user, in)
	if st != 201 {
		t.Fatalf("create = %d %v", st, created)
	}
	id := created["id"].(string)
	if st, p := c.call("POST", "/v1/customers", user, in); st != 409 || p["code"] != "email_taken" {
		t.Errorf("duplicate = %d %v", st, p)
	}
	if st, list := c.call("GET", "/v1/customers?q=silva&status=active", user, nil); st != 200 || list["total"] != 1.0 {
		t.Errorf("list = %d %v", st, list)
	}
	if st, p := c.call("PATCH", "/v1/customers/"+id, user, map[string]any{"status": "inactive"}); st != 200 || p["status"] != "inactive" {
		t.Errorf("patch = %d %v", st, p)
	}
	if st, _ := c.call("DELETE", "/v1/customers/"+id, user, nil); st != 403 {
		t.Errorf("user delete = %d", st)
	}
	if st, _ := c.call("DELETE", "/v1/customers/"+id, admin, nil); st != 204 {
		t.Errorf("admin delete = %d", st)
	}
	if st, _ := c.call("GET", "/v1/customers/"+id, user, nil); st != 404 {
		t.Errorf("after delete = %d", st)
	}
}

func TestNewRestartKeepsAdminAndData(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for i := 0; i < 2; i++ { // second start: migrations and admin seeding are idempotent
		a, err := app.New(ctx, cfg, log)
		if err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		a.Close()
	}
}

func TestNewFailures(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	bad := testConfig(t)
	bad.DatabaseURL = "postgres://nobody:x@127.0.0.1:1/none?connect_timeout=1"
	if _, err := app.New(ctx, bad, log); err == nil {
		t.Error("unreachable database must fail startup")
	}
	invalidAdmin := testConfig(t)
	invalidAdmin.AdminEmail = "not-an-email"
	if _, err := app.New(ctx, invalidAdmin, log); err == nil {
		t.Error("invalid bootstrap admin must fail startup")
	}
}

func TestServeShutsDownGracefully(t *testing.T) {
	cfg := testConfig(t)
	cfg.Env = config.EnvRelease // exercises the non-warning branch
	a, err := app.New(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Serve(ctx, ln) }()

	addr := ln.Addr().String()
	var last error
	for i := 0; i < 50; i++ { // wait until it answers
		if last = app.Healthcheck(addr); last == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if last != nil {
		t.Fatalf("never became healthy: %v", last)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve returned %v on graceful shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	if err := app.Healthcheck(addr); err == nil {
		t.Error("server still answering after shutdown")
	}
}

func TestServeReturnsListenerError(t *testing.T) {
	a, err := app.New(context.Background(), testConfig(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	ln.Close() // serving on a closed listener fails immediately
	if err := a.Serve(context.Background(), ln); err == nil {
		t.Error("expected an error")
	}
}

func TestHealthcheck(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer ok.Close()
	if err := app.Healthcheck(ok.Listener.Addr().String()); err != nil {
		t.Errorf("healthy server: %v", err)
	}
	sick := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer sick.Close()
	if err := app.Healthcheck(sick.Listener.Addr().String()); err == nil {
		t.Error("503 must be unhealthy")
	}
	if err := app.Healthcheck("no-port"); err == nil {
		t.Error("malformed address must fail")
	}
	if err := app.Healthcheck(":1"); err == nil {
		t.Error("nothing listens on :1")
	}
}
