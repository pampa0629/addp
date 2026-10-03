package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestTransferTaskCreateDelegationMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable System PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reset := func() {
		t.Helper()
		if _, err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	before, after := migrationFilesBeforeAndThrough(t, "000181_transfer_task_create_delegation.up.sql")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	var grantsBefore, grantsAfter int
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions`).Scan(&grantsBefore); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{DSN: dsn, FS: after, Root: DefaultMigrationsRoot}
	for range 2 {
		if err := runner.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var delegable bool
	if err := db.QueryRow(`SELECT delegable FROM system.permissions WHERE permission_key='transfer.task.create'`).Scan(&delegable); err != nil || !delegable {
		t.Fatalf("create delegable=%v err=%v", delegable, err)
	}
	if err := db.QueryRow(`SELECT delegable FROM system.permissions WHERE permission_key='transfer.task.execute'`).Scan(&delegable); err != nil || delegable {
		t.Fatalf("execute delegable=%v err=%v", delegable, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions`).Scan(&grantsAfter); err != nil || grantsAfter != grantsBefore {
		t.Fatalf("implicit grants: before=%d after=%d err=%v", grantsBefore, grantsAfter, err)
	}
}
