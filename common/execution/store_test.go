package execution

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEmbeddedMigrationsContainQualityQueueIndexes(t *testing.T) {
	names, err := executionMigrationNames(migrationFiles, "migrations")
	if err != nil {
		t.Fatalf("executionMigrationNames() error = %v", err)
	}
	if got := names[len(names)-1]; got != "012_allow_event_trigger_type.sql" {
		t.Fatalf("latest execution migration = %q", got)
	}
	eventTrigger, err := migrationFiles.ReadFile("migrations/012_allow_event_trigger_type.sql")
	if err != nil {
		t.Fatalf("read event trigger migration: %v", err)
	}
	for _, required := range []string{"DROP CONSTRAINT IF EXISTS chk_task_executions_trigger_type", "'event'"} {
		if !strings.Contains(string(eventTrigger), required) {
			t.Fatalf("event trigger migration missing %q", required)
		}
	}
	removedEffects, err := migrationFiles.ReadFile("migrations/011_remove_authorization_effects.sql")
	if err != nil {
		t.Fatalf("read authorization effects removal migration: %v", err)
	}
	for _, required := range []string{"DROP COLUMN IF EXISTS authorization_effects", "IN (0, 2)"} {
		if !strings.Contains(string(removedEffects), required) {
			t.Fatalf("authorization effects removal migration missing %q", required)
		}
	}
	contents, err := migrationFiles.ReadFile("migrations/008_quality_execution_queue_indexes.sql")
	if err != nil {
		t.Fatalf("read quality queue migration: %v", err)
	}
	for _, required := range []string{
		"task_type = 'check'",
		"execution_authorization_id IS NOT NULL",
		"idx_task_executions_quality_running_lease",
		"lease_expires_at IS NOT NULL",
	} {
		if !strings.Contains(string(contents), required) {
			t.Fatalf("quality queue migration missing %q", required)
		}
	}
	ownership, err := migrationFiles.ReadFile("migrations/009_bounded_execution_ownership.sql")
	if err != nil {
		t.Fatalf("read bounded ownership migration: %v", err)
	}
	for _, required := range []string{"execution_boundary", "retry_of_execution_id", "lease_token", "idx_task_executions_bounded_pending"} {
		if !strings.Contains(string(ownership), required) {
			t.Fatalf("bounded ownership migration missing %q", required)
		}
	}
	authorizationLineage, err := migrationFiles.ReadFile("migrations/010_execution_authorization_lineage.sql")
	if err != nil {
		t.Fatalf("read authorization lineage migration: %v", err)
	}
	for _, required := range []string{
		"actor_principal_id",
		"execution_authorization_id",
		"execution_authorization_id IS NULL",
		"IN (0, 3)",
	} {
		if !strings.Contains(string(authorizationLineage), required) {
			t.Fatalf("authorization lineage migration missing %q", required)
		}
	}
}

func TestExecutionMigrationNamesFiltersDownMigrations(t *testing.T) {
	t.Parallel()

	names, err := executionMigrationNames(fstest.MapFS{
		"migrations/003_add_source.sql":                  {Data: []byte("select 1;")},
		"migrations/001_restructure_task_executions.sql": {Data: []byte("select 1;")},
		"migrations/002_normalize_trigger_type_down.sql": {Data: []byte("select 1;")},
		"migrations/readme.md":                           {Data: []byte("ignored")},
	}, "migrations")
	if err != nil {
		t.Fatalf("executionMigrationNames() error = %v", err)
	}

	want := []string{
		"001_restructure_task_executions.sql",
		"003_add_source.sql",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("migration names = %#v, want %#v", names, want)
	}
}

func TestExecutionMigrationRecordIsIdempotent(t *testing.T) {
	db := newExecutionStoreTestDB(t)

	if err := ensureExecutionMigrationTable(db); err != nil {
		t.Fatalf("ensure migration table: %v", err)
	}

	version := "001_restructure_task_executions.sql"
	applied, err := executionMigrationApplied(db, version)
	if err != nil {
		t.Fatalf("check migration before record: %v", err)
	}
	if applied {
		t.Fatal("migration should not be applied before record")
	}

	if err := recordExecutionMigration(db, version); err != nil {
		t.Fatalf("record migration: %v", err)
	}
	if err := recordExecutionMigration(db, version); err != nil {
		t.Fatalf("record migration second time: %v", err)
	}

	applied, err = executionMigrationApplied(db, version)
	if err != nil {
		t.Fatalf("check migration after record: %v", err)
	}
	if !applied {
		t.Fatal("migration should be applied after record")
	}

	var count int64
	if err := db.Raw(`SELECT COUNT(*) FROM common.execution_schema_migrations WHERE version = ?`, version).Scan(&count).Error; err != nil {
		t.Fatalf("count migration records: %v", err)
	}
	if count != 1 {
		t.Fatalf("migration record count = %d, want 1", count)
	}
}

func newExecutionStoreTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}
	return db
}
