package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreccls/go-customers-api/internal/auth"
	"github.com/andreccls/go-customers-api/internal/customer"
	"github.com/andreccls/go-customers-api/internal/postgres"
	"github.com/andreccls/go-customers-api/internal/repotest"
	"github.com/andreccls/go-customers-api/internal/testdb"
)

// freshPool returns a pool on an empty, migrated, isolated schema.
func freshPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Open(ctx, testdb.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestCustomersContract(t *testing.T) {
	repotest.Customers(t, func(t *testing.T) customer.Repository { return postgres.NewCustomers(freshPool(t)) })
}

func TestUsersContract(t *testing.T) {
	repotest.AuthStore(t, func(t *testing.T) auth.Store { return postgres.NewUsers(freshPool(t)) })
}

func TestMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	pool := freshPool(t)
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("second run: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&n); err != nil || n != 2 {
		t.Errorf("schema_migrations rows = %d, err = %v", n, err)
	}
}

func TestMigrateConcurrentStart(t *testing.T) {
	ctx := context.Background()
	pool, err := postgres.Open(ctx, testdb.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { errs <- postgres.Migrate(ctx, pool) }()
	}
	for i := 0; i < 4; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent migrate: %v", err)
		}
	}
}

func TestOpenFailsFastOnBadURL(t *testing.T) {
	ctx := context.Background()
	if _, err := postgres.Open(ctx, "not a url"); err == nil {
		t.Error("expected parse error")
	}
	if _, err := postgres.Open(ctx, "postgres://u:p@127.0.0.1:1/db?connect_timeout=1"); err == nil {
		t.Error("expected ping error")
	}
}

func TestMigrateReportsBrokenConnection(t *testing.T) {
	ctx := context.Background()
	pool, err := postgres.Open(ctx, testdb.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	if err := postgres.Migrate(ctx, pool); err == nil {
		t.Error("expected error on a closed pool")
	}
}
