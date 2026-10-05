// Package testfixture provides process-local fixtures, not production migrations.
package testfixture

import (
	"testing"

	"gorm.io/gorm"
)

// ContentIndexSQLite avoids SQLite GORM's unsupported schema-qualified index
// migration. The production models and constraints are verified by the PG gate.
func ContentIndexSQLite(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`ATTACH DATABASE ':memory:' AS manager`,
		`CREATE TABLE manager.content_index_outlets (
		 index_name TEXT PRIMARY KEY, epoch TEXT NOT NULL, endpoint_id TEXT NOT NULL,
		 configured BOOLEAN NOT NULL, isolated BOOLEAN NOT NULL, updated_at DATETIME)`,
		`CREATE TABLE manager.content_index_deliveries (
		 id TEXT PRIMARY KEY, index_name TEXT NOT NULL, tenant_id INTEGER NOT NULL,
		 document_id TEXT NOT NULL, kind TEXT NOT NULL, filter TEXT NOT NULL,
		 endpoint_id TEXT NOT NULL, status TEXT NOT NULL, task_uid INTEGER,
		 task_enqueued_at TEXT NOT NULL, task_correlation TEXT NOT NULL DEFAULT '', created_at DATETIME, updated_at DATETIME,
		 UNIQUE (index_name, endpoint_id, task_uid))`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
}
