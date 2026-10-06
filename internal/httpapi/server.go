// Package httpapi is the HTTP adapter: routing, middleware, JSON and problem
// details, and the embedded OpenAPI document + Swagger UI. Handlers are thin: they
// decode, call a service, and encode.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/andreccls/go-customers-api/internal/auth"
	"github.com/andreccls/go-customers-api/internal/customer"
	"github.com/andreccls/go-customers-api/internal/ratelimit"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// CustomerService is what the handlers need from the customer package.
type CustomerService interface {
	Create(ctx context.Context, in customer.Input) (customer.Customer, error)
	Get(ctx context.Context, id string) (customer.Customer, error)
	List(ctx context.Context, f customer.Filter) ([]customer.Customer, int, error)
	Replace(ctx context.Context, id string, in customer.Input) (customer.Customer, error)
	Update(ctx context.Context, id string, p customer.Patch) (customer.Customer, error)
	Delete(ctx context.Context, id string) error
}

// AuthService is what the handlers and middleware need from the auth package.
type AuthService interface {
	Register(ctx context.Context, email, password string) (auth.User, error)
	Login(ctx context.Context, email, password string) (auth.Tokens, error)
	Refresh(ctx context.Context, refreshToken string) (auth.Tokens, error)
	Logout(ctx context.Context, refreshToken string) error
	Authenticate(token string) (auth.Principal, error)
}

// Deps are the collaborators of the API.
type Deps struct {
	Customers      CustomerService
	Auth           AuthService
	Ready          func(context.Context) error // readiness probe (database ping)
	Logger         *slog.Logger
	AuthLimiter    *ratelimit.Limiter // per client IP, /v1/auth/*
	APILimiter     *ratelimit.Limiter // per user, /v1/customers*
	RequestTimeout time.Duration
}

type server struct {
	customers      CustomerService
	auth           AuthService
	ready          func(context.Context) error
	log            *slog.Logger
	authLimiter    *ratelimit.Limiter
	apiLimiter     *ratelimit.Limiter
	requestTimeout time.Duration
}

// Access says who may call a route.
type Access int

const (
	Public  Access = iota // no token
	AnyUser               // any valid token (admin or user)
	AdminOnly
)

// route is one API operation. This table is the single source of truth for the
// router; the OpenAPI test compares it with openapi.json, so the two cannot drift.
type route struct {
	method, path string
	access       Access
	limited      bool // /v1/auth/* : rate-limited per IP
	handler      http.HandlerFunc
}

func (s *server) routes() []route {
	return []route{
		{"GET", "/healthz", Public, false, s.healthz},
		{"GET", "/readyz", Public, false, s.readyz},

		{"POST", "/v1/auth/register", Public, true, s.register},
		{"POST", "/v1/auth/login", Public, true, s.login},
		{"POST", "/v1/auth/refresh", Public, true, s.refresh},
		{"POST", "/v1/auth/logout", Public, true, s.logout},

		{"POST", "/v1/customers", AnyUser, false, s.createCustomer},
		{"GET", "/v1/customers", AnyUser, false, s.listCustomers},
		{"GET", "/v1/customers/{id}", AnyUser, false, s.getCustomer},
		{"PUT", "/v1/customers/{id}", AnyUser, false, s.replaceCustomer},
		{"PATCH", "/v1/customers/{id}", AnyUser, false, s.patchCustomer},
		{"DELETE", "/v1/customers/{id}", AdminOnly, false, s.deleteCustomer},
	}
}

// Operation describes one route for documentation tests.
type Operation struct {
	Method, Path string
	Access       Access
	RateLimited  bool
}

// Operations lists the routes the API serves (excluding /docs and /openapi.json).
func Operations() []Operation {
	var ops []Operation
	for _, rt := range (&server{}).routes() {
		ops = append(ops, Operation{rt.method, rt.path, rt.access, rt.limited || rt.access != Public})
	}
	return ops
}

// New builds the full handler: routes, docs and the middleware chain.
func New(d Deps) http.Handler {
	s := &server{
		customers: d.Customers, auth: d.Auth, ready: d.Ready, log: d.Logger,
		authLimiter: d.AuthLimiter, apiLimiter: d.APILimiter, requestTimeout: d.RequestTimeout,
	}
	mux := http.NewServeMux()
	for _, rt := range s.routes() {
		pattern := rt.method + " " + rt.path
		mux.Handle(pattern, named(pattern, s.protect(rt)))
	}
	mountDocs(mux)
	mux.Handle("/", named("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusNotFound, "route_not_found", "No such route.", nil)
	})))

	// Outermost first: observe (ID, log) -> recover -> limits (body, timeout) -> mux.
	return s.observe(s.recoverer(s.limits(mux)))
}

// protect wraps a handler with the rate limit and authentication its route needs.
func (s *server) protect(rt route) http.Handler {
	var h http.Handler = rt.handler
	switch rt.access {
	case AdminOnly:
		h = requireRole(auth.RoleAdmin, h)
		fallthrough
	case AnyUser:
		h = s.authenticate(rateLimit(s.apiLimiter, byUser, h))
	default:
		if rt.limited {
			h = rateLimit(s.authLimiter, byIP, h)
		}
	}
	return h
}
