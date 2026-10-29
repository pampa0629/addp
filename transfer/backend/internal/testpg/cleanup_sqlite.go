package testpg

import (
	"gorm.io/gorm"
	"testing"
)

// EnsureCleanupSQLite creates the deterministic fixture's attached-schema
// control tables. Production schema and indexes are verified by the PG gate.
func EnsureCleanupSQLite(t testing.TB, db *gorm.DB) {
	t.Helper()
	for _, sql := range []string{
		`CREATE TABLE IF NOT EXISTS transfer.runtime_sessions (execution_id TEXT NOT NULL, owner_instance_id TEXT NOT NULL, fencing_token INTEGER NOT NULL, task_id INTEGER NOT NULL, tenant_id INTEGER NOT NULL, started_at DATETIME NOT NULL, stopped_at DATETIME, PRIMARY KEY(execution_id,owner_instance_id,fencing_token))`,
		`CREATE TABLE IF NOT EXISTS transfer.cleanup_operations (target_key TEXT PRIMARY KEY, operation_id TEXT NOT NULL UNIQUE, tenant_id INTEGER NOT NULL, task_id INTEGER NOT NULL, kind TEXT NOT NULL, target TEXT NOT NULL, cleanup_execution_id TEXT, attempt INTEGER, capture_resource_id INTEGER, resource_version INTEGER, status TEXT NOT NULL, created_at DATETIME, completed_at DATETIME)`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
}
