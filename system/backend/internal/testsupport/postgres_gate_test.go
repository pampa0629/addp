package testsupport

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestResetDisposablePostgresForGate(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	RequireDisposablePostgresDSN(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to disposable PostgreSQL database: %v", err)
	}
	defer connection.Close(ctx)
	if _, err := connection.Exec(ctx, `
		DROP SCHEMA IF EXISTS system CASCADE;
		DROP SCHEMA IF EXISTS common CASCADE;
	`); err != nil {
		t.Fatalf("reset disposable PostgreSQL database: %v", err)
	}
}
