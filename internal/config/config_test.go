package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func env(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }

const strong = "k3Jx9Qm2VbN7ZpR4tYw8LcD5hFsA1uGe6oXi0TqB"

func TestLoadDefaultsAndOverrides(t *testing.T) {
	c, err := Load(env(map[string]string{
		"DATABASE_URL": "postgres://x", "JWT_SECRET": strong, "ADMIN_EMAIL": "a@b.co", "ADMIN_PASSWORD": "Tr0ub4dor&3-long",
		"JWT_ACCESS_TTL": "5m", "RATE_LIMIT_API_PER_MIN": "7", "LOG_LEVEL": "debug", "HTTP_ADDR": ":9000",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Release() || c.AccessTTL != 5*time.Minute || c.RefreshTTL != 7*24*time.Hour ||
		c.APIRatePerMin != 7 || c.AuthRatePerMin != 10 || c.LogLevel != slog.LevelDebug || c.HTTPAddr != ":9000" {
		t.Errorf("unexpected config: %+v", c)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"missing database", map[string]string{"JWT_SECRET": strong}, "DATABASE_URL is required"},
		{"missing secret", map[string]string{"DATABASE_URL": "x"}, "JWT_SECRET is required"},
		{"bad env", map[string]string{"DATABASE_URL": "x", "JWT_SECRET": strong, "APP_ENV": "prod"}, "APP_ENV"},
		{"bad duration", map[string]string{"DATABASE_URL": "x", "JWT_SECRET": strong, "JWT_ACCESS_TTL": "soon"}, "JWT_ACCESS_TTL"},
		{"negative duration", map[string]string{"DATABASE_URL": "x", "JWT_SECRET": strong, "JWT_REFRESH_TTL": "-1h"}, "JWT_REFRESH_TTL"},
		{"bad number", map[string]string{"DATABASE_URL": "x", "JWT_SECRET": strong, "RATE_LIMIT_AUTH_PER_MIN": "0"}, "RATE_LIMIT_AUTH_PER_MIN"},
		{"bad log level", map[string]string{"DATABASE_URL": "x", "JWT_SECRET": strong, "LOG_LEVEL": "loud"}, "LOG_LEVEL"},
		{"admin email without password", map[string]string{"DATABASE_URL": "x", "JWT_SECRET": strong, "ADMIN_EMAIL": "a@b.co"}, "set together"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(env(tt.env)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestReleaseRefusesWeakSecrets(t *testing.T) {
	weakSecrets := map[string]string{
		"the .env.example value": "dev-only-insecure-secret-change-me-0123456789abcdef",
		"too short":              "short-but-random-Xq9",
		"low entropy":            strings.Repeat("ab", 20),
		"placeholder word":       "my-super-secret-key-1234567890-abcdefghij",
	}
	for name, s := range weakSecrets {
		_, err := Load(env(map[string]string{"DATABASE_URL": "x", "JWT_SECRET": s}))
		if err == nil || !strings.Contains(err.Error(), "too weak for release") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	_, err := Load(env(map[string]string{"DATABASE_URL": "x", "JWT_SECRET": strong, "ADMIN_EMAIL": "a@b.co", "ADMIN_PASSWORD": "dev-only-admin-password"}))
	if err == nil || !strings.Contains(err.Error(), "ADMIN_PASSWORD is too weak") {
		t.Errorf("weak admin password accepted in release: %v", err)
	}
	// An unset APP_ENV is release, not development.
	if _, err := Load(env(map[string]string{"DATABASE_URL": "x", "JWT_SECRET": "dev-only-insecure-secret-change-me-0123456789abcdef"})); err == nil {
		t.Error("default environment must be strict")
	}
}

func TestDevelopmentAcceptsPlaceholderSecrets(t *testing.T) {
	c, err := Load(env(map[string]string{
		"APP_ENV": "development", "DATABASE_URL": "x",
		"JWT_SECRET": "dev-only-insecure-secret-change-me-0123456789abcdef", "ADMIN_EMAIL": "a@b.co", "ADMIN_PASSWORD": "dev-only-admin-password",
	}))
	if err != nil || c.Release() {
		t.Errorf("c=%+v err=%v", c, err)
	}
	// Even in development, an obviously too short secret is refused.
	if _, err := Load(env(map[string]string{"APP_ENV": "development", "DATABASE_URL": "x", "JWT_SECRET": "short"})); err == nil {
		t.Error("short secret accepted in development")
	}
}
