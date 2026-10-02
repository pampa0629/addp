package migration

import (
	"context"
	"database/sql"
	"github.com/addp/system/internal/testsupport"
	"os"
	"testing"
	"time"
)

func TestModuleLogSourceDiagnosticsForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard System PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reset := func() {
		if _, err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	before, after := migrationFilesBeforeAndThrough(t, "000177_module_log_source_diagnostics.up.sql")
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.Exec(`INSERT INTO system.module_log_source_nodes(node,boot_id,sequence,sampled_at,received_at,complete,payload_hash) VALUES ('host','boot',4,$1,$1,true,'hash')`, stamp); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{DSN: dsn, FS: after, Root: DefaultMigrationsRoot}
	for i := 0; i < 2; i++ {
		if err := runner.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var received time.Time
	var complete, missing bool
	var sequence int
	if err := db.QueryRow(`SELECT received_at,complete,sequence,scan_issues IS NULL FROM system.module_log_source_nodes WHERE node='host'`).Scan(&received, &complete, &sequence, &missing); err != nil {
		t.Fatal(err)
	}
	if !received.Equal(stamp) || !complete || sequence != 4 || !missing {
		t.Fatal("migration fabricated diagnostic or rewrote historical evidence")
	}
	if _, err := db.Exec(`UPDATE system.module_log_source_nodes SET scan_issues='{}'::jsonb WHERE node='host'`); err == nil {
		t.Fatal("non-array diagnostic accepted")
	}
}
