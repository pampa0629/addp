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
		 index_name TEXT PRIMARY KEY, epoch TEXT NOT NULL, endpoint_id TEXT NOT NULL,search_revision INTEGER NOT NULL DEFAULT 0,next_search_ordinal INTEGER NOT NULL DEFAULT 0,
		 configured BOOLEAN NOT NULL, isolated BOOLEAN NOT NULL, cleanup_hold_id TEXT NOT NULL DEFAULT '', updated_at DATETIME)`,
		`CREATE TABLE manager.search_material_generation(singleton BOOLEAN PRIMARY KEY,generation TEXT NOT NULL)`,
		`INSERT INTO manager.search_material_generation VALUES(true,'03c40e08-fcbf-468c-932e-d54da85ca8e7')`,
		`CREATE TABLE manager.search_vector_revisions(tenant_id INTEGER PRIMARY KEY,version INTEGER NOT NULL)`,
		`CREATE TABLE manager.search_document_ordinals(index_name TEXT NOT NULL,document_id TEXT NOT NULL,search_ordinal INTEGER NOT NULL,PRIMARY KEY(index_name,document_id),UNIQUE(index_name,search_ordinal))`,
		`CREATE TABLE manager.content_index_deliveries (
		 id TEXT PRIMARY KEY, index_name TEXT NOT NULL, tenant_id INTEGER NOT NULL,
		 cleanup_operation_id TEXT NOT NULL DEFAULT '', engine_ids TEXT NOT NULL DEFAULT '[]', document_id TEXT NOT NULL, document_ids TEXT NOT NULL DEFAULT '[]', kind TEXT NOT NULL, filter TEXT NOT NULL,
		 endpoint_id TEXT NOT NULL, status TEXT NOT NULL, task_uid INTEGER,
		 task_enqueued_at TEXT NOT NULL, task_correlation TEXT NOT NULL DEFAULT '', created_at DATETIME, updated_at DATETIME,
		 UNIQUE (index_name, endpoint_id, task_uid))`,
		`CREATE TABLE manager.cleanup_index_operations (id TEXT PRIMARY KEY,index_name TEXT NOT NULL,tenant_id INTEGER NOT NULL,execution_id TEXT NOT NULL UNIQUE,attempt INTEGER NOT NULL,request_digest TEXT NOT NULL,epoch TEXT NOT NULL,endpoint_id TEXT NOT NULL,inventory_digest TEXT NOT NULL,state TEXT NOT NULL,created_at DATETIME,settled_at DATETIME)`,
		`CREATE TABLE manager.cleanup_index_assessments (domain_digest TEXT NOT NULL DEFAULT '', request_id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, requested_by INTEGER NOT NULL, scope_engine_id INTEGER NOT NULL, index_name TEXT NOT NULL, epoch TEXT NOT NULL, endpoint_id TEXT NOT NULL, inventory_digest TEXT NOT NULL, engine_ids TEXT NOT NULL, candidate_count INTEGER NOT NULL, created_at DATETIME)`,
		`CREATE TABLE manager.cleanup_index_targets (index_name TEXT NOT NULL,tenant_id INTEGER NOT NULL,engine_id INTEGER NOT NULL,operation_id TEXT NOT NULL,PRIMARY KEY(index_name,tenant_id,engine_id))`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
}
