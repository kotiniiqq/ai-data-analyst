// Package testdb gives integration tests a connection to the compose
// database. Tests skip when TEST_DATABASE_URL is not set.
package testdb

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool connects with TEST_DATABASE_URL (the app role).
func Pool(t *testing.T) *pgxpool.Pool { return connect(t, "TEST_DATABASE_URL") }

// WarehousePool connects with TEST_WAREHOUSE_URL (the read-only analyst_ro role).
func WarehousePool(t *testing.T) *pgxpool.Pool { return connect(t, "TEST_WAREHOUSE_URL") }

func connect(t *testing.T, env string) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv(env)
	if dsn == "" {
		t.Skip(env + " not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
