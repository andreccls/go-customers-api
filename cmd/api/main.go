// Command api runs the customers API.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/andreccls/go-customers-api/internal/app"
	"github.com/andreccls/go-customers-api/internal/config"
)

func main() { os.Exit(run()) }

func run() int {
	healthcheck := flag.Bool("healthcheck", false, "probe /healthz on HTTP_ADDR and exit 0/1 (for container healthchecks)")
	flag.Parse()

	cfg, err := config.Load(os.Getenv)
	if *healthcheck {
		if err := app.Healthcheck(cfg.HTTPAddr); err != nil {
			fmt.Fprintln(os.Stderr, "unhealthy:", err)
			return 1
		}
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid configuration:\n"+err.Error())
		return 1
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.New(ctx, cfg, log)
	if err != nil {
		log.Error("startup failed", "error", err)
		return 1
	}
	defer a.Close()

	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		log.Error("listen failed", "error", err)
		return 1
	}
	if err := a.Serve(ctx, ln); err != nil {
		log.Error("server stopped", "error", err)
		return 1
	}
	return 0
}
