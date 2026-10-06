// Package config reads the process configuration from environment variables
// (twelve-factor) and refuses unsafe settings when running in release mode.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// Environment names.
const (
	EnvRelease     = "release"
	EnvDevelopment = "development"
)

// Config is the validated configuration.
type Config struct {
	Env            string
	HTTPAddr       string
	DatabaseURL    string
	JWTSecret      string
	AccessTTL      time.Duration
	RefreshTTL     time.Duration
	AuthRatePerMin int // per client IP, on /v1/auth/*
	APIRatePerMin  int // per authenticated user, on the rest of the API
	AdminEmail     string
	AdminPassword  string
	LogLevel       slog.Level
}

// Release reports whether the strict (production) rules apply.
func (c Config) Release() bool { return c.Env == EnvRelease }

// Load builds a Config from getenv (os.Getenv in production). It reports every
// problem at once instead of stopping at the first.
//
// APP_ENV defaults to "release": forgetting to set it gives the strict behaviour,
// never the lenient one.
func Load(getenv func(string) string) (Config, error) {
	var errs []error
	str := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	dur := func(key string, def time.Duration) time.Duration {
		v := str(key, "")
		if v == "" {
			return def
		}
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("%s: %q is not a positive duration (e.g. 15m)", key, v))
			return def
		}
		return d
	}
	num := func(key string, def int) int {
		v := str(key, "")
		if v == "" {
			return def
		}
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("%s: %q is not a positive integer", key, v))
			return def
		}
		return n
	}

	c := Config{
		Env:            str("APP_ENV", EnvRelease),
		HTTPAddr:       str("HTTP_ADDR", ":8080"),
		DatabaseURL:    str("DATABASE_URL", ""),
		JWTSecret:      getenv("JWT_SECRET"),
		AccessTTL:      dur("JWT_ACCESS_TTL", 15*time.Minute),
		RefreshTTL:     dur("JWT_REFRESH_TTL", 7*24*time.Hour),
		AuthRatePerMin: num("RATE_LIMIT_AUTH_PER_MIN", 10),
		APIRatePerMin:  num("RATE_LIMIT_API_PER_MIN", 120),
		AdminEmail:     str("ADMIN_EMAIL", ""),
		AdminPassword:  getenv("ADMIN_PASSWORD"),
	}
	if err := c.LogLevel.UnmarshalText([]byte(str("LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
	}

	if c.Env != EnvRelease && c.Env != EnvDevelopment {
		errs = append(errs, fmt.Errorf("APP_ENV: %q must be %q or %q", c.Env, EnvRelease, EnvDevelopment))
	}
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if (c.AdminEmail == "") != (c.AdminPassword == "") {
		errs = append(errs, errors.New("ADMIN_EMAIL and ADMIN_PASSWORD must be set together"))
	}
	errs = append(errs, checkSecrets(c)...)
	return c, errors.Join(errs...)
}

// weakMarkers are substrings that identify the placeholder values shipped in
// .env.example / docker-compose.yml.
var weakMarkers = []string{"dev-only", "change-me", "changeme", "example", "password", "secret-key"}

func checkSecrets(c Config) []error {
	var errs []error
	if len(c.JWTSecret) < 16 {
		errs = append(errs, errors.New("JWT_SECRET is required and must have at least 16 characters"))
	}
	if !c.Release() {
		return errs
	}
	if len(c.JWTSecret) < 32 || weak(c.JWTSecret) {
		errs = append(errs, errors.New("JWT_SECRET is too weak for release mode: use 32+ random characters (e.g. `openssl rand -base64 48`) that are not a placeholder"))
	}
	if c.AdminPassword != "" && (len(c.AdminPassword) < 12 || weak(c.AdminPassword)) {
		errs = append(errs, errors.New("ADMIN_PASSWORD is too weak for release mode: use 12+ characters that are not a placeholder"))
	}
	return errs
}

func weak(s string) bool {
	l := strings.ToLower(s)
	for _, m := range weakMarkers {
		if strings.Contains(l, m) {
			return true
		}
	}
	distinct := map[rune]bool{}
	for _, r := range s {
		distinct[r] = true
	}
	return len(distinct) < 8 // "aaaaaaaa…", "abababab…"
}
