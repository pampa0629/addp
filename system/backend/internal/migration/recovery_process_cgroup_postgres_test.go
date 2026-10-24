package migration

import (
	"context"
	"database/sql"
	"github.com/addp/system/internal/testsupport"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestProcessCgroupDirtyMigrationRepairAgainstPostgres(t *testing.T) {
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
		if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	before, through := migrationFilesBeforeAndThrough(t, "000240_process_cgroup_identity.up.sql")
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	state := func(want uint, dirty bool) {
		var version uint
		var actual bool
		if err := db.QueryRow(`SELECT version,dirty FROM system.schema_migrations`).Scan(&version, &actual); err != nil || version != want || actual != dirty {
			t.Fatalf("state %d/%t want %d/%t: %v", version, actual, want, dirty, err)
		}
	}
	state(239, false)
	if err := RepairDirtyProcessCgroupMigration240(ctx, db); err == nil {
		t.Fatal("clean migration accepted for repair")
	}
	broken := fstest.MapFS{}
	for name, file := range through {
		copy := *file
		broken[name] = &copy
	}
	key := "sql/000240_process_cgroup_identity.up.sql"
	bad := strings.Replace(string(broken[key].Data), "((process_metrics->'cgroup') -", "(process_metrics->'cgroup' -", 1)
	if bad == string(broken[key].Data) {
		t.Fatal("could not reproduce exact failed expression")
	}
	broken[key].Data = []byte(bad)
	if err := (&Runner{DSN: dsn, FS: broken, Root: DefaultMigrationsRoot}).Run(ctx); err == nil || !strings.Contains(err.Error(), "operator is not unique") {
		t.Fatal("exact SQL failure not reproduced", err)
	}
	state(240, true)
	catalog, err := ReadCatalog(EmbeddedSQL, DefaultMigrationsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE system.schema_migration_checksums SET sha256=$1 WHERE version=239`, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	if err = RepairDirtyProcessCgroupMigration240(ctx, db); err == nil {
		t.Fatal("changed historical checksum accepted")
	}
	state(240, true)
	if _, err = db.Exec(`UPDATE system.schema_migration_checksums SET sha256=$1 WHERE version=239`, catalog.Files[238].SHA256); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`ALTER TABLE system.module_runtime_instances DISABLE TRIGGER process_metrics_declaration_immutable`); err != nil {
		t.Fatal(err)
	}
	if err = RepairDirtyProcessCgroupMigration240(ctx, db); err == nil {
		t.Fatal("disabled immutability guard accepted")
	}
	state(240, true)
	if _, err = db.Exec(`ALTER TABLE system.module_runtime_instances ENABLE TRIGGER process_metrics_declaration_immutable; ALTER TABLE system.module_runtime_instances DROP CONSTRAINT process_metrics_declaration_shape; ALTER TABLE system.module_runtime_instances ADD CONSTRAINT process_metrics_declaration_shape CHECK (process_metrics IS NULL)`); err != nil {
		t.Fatal(err)
	}
	if err = RepairDirtyProcessCgroupMigration240(ctx, db); err == nil {
		t.Fatal("changed pre-migration constraint accepted")
	}
	state(240, true)
	if _, err = db.Exec(`ALTER TABLE system.module_runtime_instances DROP CONSTRAINT process_metrics_declaration_shape; ALTER TABLE system.module_runtime_instances ADD CONSTRAINT process_metrics_declaration_shape ` + processMetricsConstraintBefore240); err != nil {
		t.Fatal(err)
	}
	if err = RepairDirtyProcessCgroupMigration240(ctx, db); err != nil {
		t.Fatal("exact rolled-back state rejected", err)
	}
	state(239, false)
	var checksums int
	if err = db.QueryRow(`SELECT count(*) FROM system.schema_migration_checksums`).Scan(&checksums); err != nil || checksums != 239 {
		t.Fatal("repair changed checksum history", checksums, err)
	}
	if err = RepairDirtyProcessCgroupMigration240(ctx, db); err == nil {
		t.Fatal("repair replay accepted")
	}
	if err = (&Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal("normal corrected forward migration", err)
	}
	state(240, false)
	if err = (&Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal("normal startup replay", err)
	}
	if _, err = db.Exec(`UPDATE system.schema_migrations SET dirty=true`); err != nil {
		t.Fatal(err)
	}
	if err = RepairDirtyProcessCgroupMigration240(ctx, db); err == nil {
		t.Fatal("already-recorded 240 accepted for rewind")
	}
	state(240, true)
}
