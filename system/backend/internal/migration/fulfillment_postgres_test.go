package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestFulfillmentOutcomeForwardMigrationAgainstPostgres(t *testing.T) {
	testCoordinationForwardMigration(t, "000167_engine_access_fulfillment_outcomes.up.sql", 167, "engine_access_fulfillment_outcomes", nil)
}

func TestApprovalRequirementForwardMigrationAgainstPostgres(t *testing.T) {
	testCoordinationForwardMigration(t, "000168_engine_access_approval_requirements.up.sql", 168, "engine_access_approval_requirements", nil)
}

func testCoordinationForwardMigration(t *testing.T, filename string, expectedVersion int64, table string, fixture func(*sql.DB) func()) {
	t.Helper()
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
	if _, err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	// Bootstrap, forward upgrade and repeated-run verification are separate
	// stages. Do not spend the latter stages' deadlines bootstrapping 160+ files.
	run := func(runner *Runner) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		started := time.Now()
		if err := runner.Run(ctx); err != nil {
			t.Fatalf("migration stage after %s: %v", time.Since(started), err)
		}
	}
	before, through := migrationFilesBeforeAndThrough(t, filename)
	run(&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot})
	var verifyFixture func()
	if fixture != nil {
		verifyFixture = fixture(db)
	}
	var permissionsBefore, versionsBefore int64
	if err := db.QueryRow("SELECT count(*) FROM system.permissions").Scan(&permissionsBefore); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COALESCE(sum(authorization_version),0) FROM system.principals").Scan(&versionsBefore); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	for i := 0; i < 2; i++ {
		run(runner)
	}
	var version, permissionsAfter, versionsAfter, triggers int64
	var dirty bool
	if err := db.QueryRow("SELECT version, dirty FROM system.schema_migrations").Scan(&version, &dirty); err != nil || version != expectedVersion || dirty {
		t.Fatalf("migration state=%d/%t err=%v", version, dirty, err)
	}
	if err := db.QueryRow("SELECT count(*) FROM system.permissions").Scan(&permissionsAfter); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COALESCE(sum(authorization_version),0) FROM system.principals").Scan(&versionsAfter); err != nil {
		t.Fatal(err)
	}
	if permissionsBefore != permissionsAfter || versionsBefore != versionsAfter {
		t.Fatal("coordination migration changed IAM grants or authorization versions")
	}
	if err := db.QueryRow("SELECT count(*) FROM pg_trigger WHERE tgrelid = $1::regclass AND NOT tgisinternal", "system."+table).Scan(&triggers); err != nil || triggers != 2 {
		t.Fatalf("immutable guards=%d err=%v", triggers, err)
	}
	if expectedVersion == 168 {
		var initialized, copiedFacts int64
		if err := db.QueryRow("SELECT count(*) FROM system.engine_access_approval_requirements").Scan(&initialized); err != nil || initialized != 0 {
			t.Fatalf("migration auto-initialized authority: rows=%d err=%v", initialized, err)
		}
		if err := db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_schema = 'system'
			AND table_name = 'engine_access_approval_requirements' AND column_name IN
			('successor_principal_id', 'approver_id', 'business_owner_id', 'department_id', 'catalog_entry_id')`).Scan(&copiedFacts); err != nil || copiedFacts != 0 {
			t.Fatalf("authority contains responsibility or approver allowlist: columns=%d err=%v", copiedFacts, err)
		}
	}
	if verifyFixture != nil {
		verifyFixture()
	}
}
