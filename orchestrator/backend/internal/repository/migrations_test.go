package repository

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMigrationNamesFiltersDownMigrations(t *testing.T) {
	t.Parallel()

	names, err := migrationNames(fstest.MapFS{
		"003_drop_legacy_step_orchestrations.sql":      {Data: []byte("select 1;")},
		"002_drop_old_executions.sql":                  {Data: []byte("select 1;")},
		"001_add_basetask_fields.sql":                  {Data: []byte("select 1;")},
		"003_drop_legacy_step_orchestrations_down.sql": {Data: []byte("select 1;")},
		"notes.md": {Data: []byte("ignored")},
	}, ".")
	if err != nil {
		t.Fatalf("migrationNames() error = %v", err)
	}

	want := []string{
		"001_add_basetask_fields.sql",
		"002_drop_old_executions.sql",
		"003_drop_legacy_step_orchestrations.sql",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("migration names = %#v, want %#v", names, want)
	}
}

func TestIntegrationApplySQLMigrationsDropsLegacyStepOrchestrations(t *testing.T) {
	db := openOrchestratorMigrationIntegrationDB(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() {
		_ = tx.Rollback().Error
	})

	ensureOrchestratorMigrationTestTables(t, tx)
	clearOrchestratorMigrationRecords(t, tx)

	legacyName := fmt.Sprintf("legacy-step-%d", time.Now().UnixNano())
	currentName := fmt.Sprintf("current-step-%d", time.Now().UnixNano())
	insertOrchestratorMigrationTestRow(t, tx, legacyName, `[
		{
			"id": "transfer-1",
			"name": "导入",
			"module": "transfer",
			"action": "execute",
			"method": "POST",
			"endpoint": "",
			"depends_on": [],
			"parameters": {},
			"timeout": 300
		}
	]`)
	insertOrchestratorMigrationTestRow(t, tx, currentName, `[
		{
			"id": "meta-scan-1",
			"name": "扫描",
			"provider": "meta",
			"task_type": "scan",
			"task_id": 1,
			"depends_on": [],
			"parameters": {},
			"timeout": 300
		}
	]`)

	if err := ApplySQLMigrations(tx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	assertOrchestratorMigrationRowCount(t, tx, legacyName, 0)
	assertOrchestratorMigrationRowCount(t, tx, currentName, 1)

	if err := ApplySQLMigrations(tx); err != nil {
		t.Fatalf("apply migrations second time: %v", err)
	}
	assertOrchestratorMigrationRowCount(t, tx, currentName, 1)
	assertOrchestratorMigrationRecorded(t, tx, "003_drop_legacy_step_orchestrations.sql")
}

func openOrchestratorMigrationIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s TimeZone=Asia/Shanghai",
		orchestratorMigrationIntegrationEnv("ADDP_TEST_POSTGRES_HOST", "localhost"),
		orchestratorMigrationIntegrationEnv("ADDP_TEST_POSTGRES_PORT", "15432"),
		orchestratorMigrationIntegrationEnv("ADDP_TEST_POSTGRES_USER", "addp"),
		orchestratorMigrationIntegrationEnv("ADDP_TEST_POSTGRES_PASSWORD", "addp_password"),
		orchestratorMigrationIntegrationEnv("ADDP_TEST_POSTGRES_DATABASE", "addp"),
		orchestratorMigrationIntegrationEnv("ADDP_TEST_POSTGRES_SSLMODE", "disable"),
	)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("PostgreSQL is not available: %v", err)
	}
	if err := db.Exec("SELECT 1").Error; err != nil {
		t.Skipf("PostgreSQL is not available: %v", err)
	}
	return db
}

func orchestratorMigrationIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func ensureOrchestratorMigrationTestTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`CREATE SCHEMA IF NOT EXISTS orchestrator`).Error; err != nil {
		t.Fatalf("create orchestrator schema: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS orchestrator.orchestrations (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL,
			name VARCHAR(128) NOT NULL,
			description VARCHAR(512),
			steps JSONB NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT false,
			schedule VARCHAR(128),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			deleted_at TIMESTAMPTZ
		)
	`).Error; err != nil {
		t.Fatalf("create orchestrations table: %v", err)
	}
}

func clearOrchestratorMigrationRecords(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := ensureMigrationTable(db); err != nil {
		t.Fatalf("ensure migration table: %v", err)
	}
	if err := db.Exec(`
		DELETE FROM orchestrator.schema_migrations
		WHERE version IN (
			'001_add_basetask_fields.sql',
			'002_drop_old_executions.sql',
			'003_drop_legacy_step_orchestrations.sql'
		)
	`).Error; err != nil {
		t.Fatalf("clear migration records: %v", err)
	}
}

func insertOrchestratorMigrationTestRow(t *testing.T, db *gorm.DB, name string, steps string) {
	t.Helper()
	if err := db.Exec(`
		INSERT INTO orchestrator.orchestrations (
			tenant_id, name, description, steps, enabled, created_at, updated_at
		)
		VALUES (?, ?, '', ?::jsonb, false, NOW(), NOW())
	`, 1, name, steps).Error; err != nil {
		t.Fatalf("insert orchestration %q: %v", name, err)
	}
}

func assertOrchestratorMigrationRowCount(t *testing.T, db *gorm.DB, name string, want int64) {
	t.Helper()
	var got int64
	if err := db.Raw(`
		SELECT COUNT(*)
		FROM orchestrator.orchestrations
		WHERE name = ?
	`, name).Scan(&got).Error; err != nil {
		t.Fatalf("count orchestration %q: %v", name, err)
	}
	if got != want {
		t.Fatalf("orchestration %q count = %d, want %d", name, got, want)
	}
}

func assertOrchestratorMigrationRecorded(t *testing.T, db *gorm.DB, version string) {
	t.Helper()
	applied, err := migrationApplied(db, version)
	if err != nil {
		t.Fatalf("check migration applied: %v", err)
	}
	if !applied {
		t.Fatalf("migration %q was not recorded", version)
	}
}
