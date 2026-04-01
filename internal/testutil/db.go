// Package testutil provides shared helpers for integration tests.
package testutil

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

// NewTestDB connects to the test database using TEST_DATABASE_URL.
// It skips the test if the env var is not set (e.g. unit-test only runs).
// The pool is automatically closed when the test ends.
func NewTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	// Load .env from repo root if TEST_DATABASE_URL isn't already set.
	// Uses the source file location so it works regardless of which package calls it.
	if os.Getenv("TEST_DATABASE_URL") == "" {
		_, filename, _, _ := runtime.Caller(0)
		root := filepath.Join(filepath.Dir(filename), "..", "..")
		_ = godotenv.Load(filepath.Join(root, ".env"))
	}
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("testutil.NewTestDB: connect: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("testutil.NewTestDB: ping: %v", err)
	}

	t.Cleanup(pool.Close)
	return pool
}

// CleanTables truncates the given tables in order (respects FK constraints
// if you pass them leaf-first). Useful in t.Cleanup to reset state between tests.
func CleanTables(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	for _, table := range tables {
		_, err := pool.Exec(context.Background(), "DELETE FROM "+table)
		if err != nil {
			t.Fatalf("testutil.CleanTables: delete %s: %v", table, err)
		}
	}
}
