package api

import (
	"testing"

	"gorm.io/gorm"
)

func ensureDerivedTaskDefinitionTestTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE manager.task_definitions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, task_type TEXT NOT NULL,
			version INTEGER NOT NULL DEFAULT 1, name TEXT NOT NULL, description TEXT, enabled BOOLEAN,
			last_execution_id TEXT, last_execution_status TEXT, last_run_at DATETIME, next_run_at DATETIME,
			binding_status TEXT NOT NULL DEFAULT 'active', binding_issue TEXT NOT NULL DEFAULT '',
			schedule TEXT, semantic_key TEXT NOT NULL DEFAULT '', created_by INTEGER, config JSON,
			created_at DATETIME, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE manager.task_resource_bindings (
			id INTEGER PRIMARY KEY AUTOINCREMENT, task_definition_id INTEGER NOT NULL, tenant_id INTEGER NOT NULL,
			role TEXT NOT NULL, engine_id INTEGER NOT NULL, locator TEXT NOT NULL, item_id INTEGER,
			item_fingerprint TEXT, ordinal INTEGER NOT NULL DEFAULT 0, created_at DATETIME, updated_at DATETIME)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create derived task definition test table: %v", err)
		}
	}
}
