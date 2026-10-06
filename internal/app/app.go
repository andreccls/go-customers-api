// Package app is the composition root: it wires config, PostgreSQL, services and
// the HTTP handler together, and runs the server with graceful shutdown.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreccls/go-customers-api/internal/auth"
	"github.com/andreccls/go-customers-api/internal/config"
	"github.com/andreccls/go-customers-api/internal/customer"
	"github.com/andreccls/go-customers-api/internal/httpapi"
	"github.com/andreccls/go-customers-api/internal/postgres"
	"github.com/andreccls/go-customers-api/internal/ratelimit"
)

const (
	requestTimeout  = 8 * time.Second // per request (handlers + queries); below WriteTimeout
	shutdownTimeout = 10 * time.Second
)

// App is a fully wired application.
type App struct {
	handler http.Handler
	pool    *pgxpool.Pool
	log     *slog.Logger
}

// New connects to PostgreSQL, applies the migrations, seeds the bootstrap admin
// (when configured) and builds the HTTP handler.
func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*App, error) {
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}

	authSvc := auth.NewService(postgres.NewUsers(pool), auth.Config{
		Secret: []byte(cfg.JWTSecret), Issuer: "go-customers-api",
		AccessTTL: cfg.AccessTTL, RefreshTTL: cfg.RefreshTTL,
	}, nil)
	if cfg.AdminEmail != "" {
		if err := authSvc.EnsureAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword); err != nil {
			pool.Close()
			return nil, fmt.Errorf("bootstrap admin: %w", err)
		}
	}
	if !cfg.Release() {
		log.Warn("running in development mode: placeholder secrets are accepted", "app_env", cfg.Env)
	}

	return &App{
		pool: pool, log: log,
		handler: httpapi.New(httpapi.Deps{
			Customers:      customer.NewService(postgres.NewCustomers(pool), nil),
			Auth:           authSvc,
			Ready:          pool.Ping,
			Logger:         log,
			AuthLimiter:    ratelimit.New(cfg.AuthRatePerMin, nil),
			APILimiter:     ratelimit.New(cfg.APIRatePerMin, nil),
			RequestTimeout: requestTimeout,
		}),
	}, nil
}

// Handler returns the HTTP handler (used by tests).
func (a *App) Handler() http.Handler { return a.handler }

// Close releases the database pool.
func (a *App) Close() { a.pool.Close() }

// Serve serves on ln until ctx is cancelled, then drains in-flight requests
// (up to 10 s) and returns nil on a clean shutdown.
func (a *App) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           a.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	a.log.Info("listening", "addr", ln.Addr().String())

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	a.log.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Healthcheck probes GET /healthz on addr (":8080" or "host:port"). The distroless
// image has no curl, so the container healthcheck runs `/api -healthcheck`.
func Healthcheck(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "" {
		host = "127.0.0.1"
	}
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
	return nil
}
