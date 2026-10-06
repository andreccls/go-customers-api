// Package testdb gives integration tests an isolated PostgreSQL schema.
package testdb

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// URL creates a throw-away schema in the database named by TEST_DATABASE_URL and
// returns a connection URL whose search_path points at it; the schema is dropped
// when the test ends. Without TEST_DATABASE_URL the test is skipped, unless
// REQUIRE_DB is set (Makefile and CI), in which case it fails: a silently skipped
// integration suite is not a green build.
func URL(t *testing.T) string {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		if os.Getenv("REQUIRE_DB") != "" {
			t.Fatal("REQUIRE_DB is set but TEST_DATABASE_URL is empty")
		}
		t.Skip("TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	schema := "t_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), "DROP SCHEMA "+schema+" CASCADE")
		_ = conn.Close(ctx)
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return fmt.Sprint(u)
}
