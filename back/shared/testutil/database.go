package testutil

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
)

func Database(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal("invalid integration database configuration")
	}
	t.Cleanup(pool.Close)
	var database string
	if err := pool.QueryRow(context.Background(), "SELECT current_database()").Scan(&database); err != nil {
		t.Fatal("integration database unavailable")
	}
	if !strings.HasPrefix(database, "confhub_test_") {
		t.Fatal("refusing to run integration tests against a non-test database")
	}
	return pool
}
