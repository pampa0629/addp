package repository

import (
	"os"
	"testing"

	metaMigrations "github.com/addp/meta/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestLineageLifecycleMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("META_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	defer tx.Rollback()

	if err := tx.Exec("DROP SCHEMA IF EXISTS meta CASCADE; CREATE SCHEMA meta").Error; err != nil {
		t.Fatalf("reset Meta schema: %v", err)
	}
	if err := tx.Exec(`CREATE TABLE meta.meta_item (
		id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL, engine_id BIGINT NOT NULL, node_id BIGINT NOT NULL,
		item_type VARCHAR(64) NOT NULL, name VARCHAR(255) NOT NULL, full_name TEXT, fingerprint VARCHAR(64) NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), deleted_at TIMESTAMPTZ
	)`).Error; err != nil {
		t.Fatalf("create meta_item: %v", err)
	}
	lineageMigration, err := metaMigrations.FS.ReadFile("018_add_lineage_relations.sql")
	if err != nil {
		t.Fatalf("read lineage migration: %v", err)
	}
	if err := tx.Exec(string(lineageMigration)).Error; err != nil {
		t.Fatalf("apply lineage migration: %v", err)
	}

	if err := tx.Exec(`INSERT INTO meta.meta_item
		(id, tenant_id, engine_id, node_id, item_type, name, full_name, fingerprint, deleted_at)
		VALUES
		(1, 7, 9, 1, 'table', 'source', 'public.source', 'fp-source', NULL),
		(2, 7, 9, 1, 'table', 'deleted-target', 'public.deleted_target', 'fp-deleted-target', NOW()),
		(3, 7, 9, 1, 'table', 'current-target', 'public.current_target', 'fp-current-target', NULL),
		(4, 7, 9, 1, 'table', 'future-deleted', 'public.future_deleted', 'fp-future-deleted', NULL)`).Error; err != nil {
		t.Fatalf("insert meta items: %v", err)
	}
	if err := tx.Exec(`INSERT INTO meta.lineage_item_relations
		(tenant_id, source_item_id, target_item_id, relation_kind, granularity, status, first_observed_at, last_observed_at)
		VALUES
		(7, 1, 2, 'derive', 'item', 'active', NOW(), NOW()),
		(7, 1, 3, 'derive', 'item', 'active', NOW(), NOW())`).Error; err != nil {
		t.Fatalf("insert lineage relations: %v", err)
	}
	if err := tx.Exec(`INSERT INTO meta.lineage_service_dependencies
		(tenant_id, source_item_id, service_id, published_revision, dependency_kind, granularity, status, first_observed_at, last_observed_at)
		VALUES (7, 2, 101, 'revision-1', 'table', 'item', 'active', NOW(), NOW())`).Error; err != nil {
		t.Fatalf("insert lineage service dependency: %v", err)
	}

	lifecycleMigration, err := metaMigrations.FS.ReadFile("022_stale_lineage_for_soft_deleted_items.sql")
	if err != nil {
		t.Fatalf("read lineage lifecycle migration: %v", err)
	}
	if err := tx.Exec(string(lifecycleMigration)).Error; err != nil {
		t.Fatalf("apply lineage lifecycle migration: %v", err)
	}

	assertLineageRelationStatus(t, tx, 2, "stale")
	assertLineageRelationStatus(t, tx, 3, "active")
	assertLineageDependencyStatus(t, tx, 101, "stale")

	if err := tx.Exec(`INSERT INTO meta.lineage_item_relations
		(tenant_id, source_item_id, target_item_id, relation_kind, granularity, status, first_observed_at, last_observed_at)
		VALUES (7, 1, 4, 'derive', 'item', 'active', NOW(), NOW())`).Error; err != nil {
		t.Fatalf("insert trigger relation: %v", err)
	}
	if err := tx.Exec(`INSERT INTO meta.lineage_service_dependencies
		(tenant_id, source_item_id, service_id, published_revision, dependency_kind, granularity, status, first_observed_at, last_observed_at)
		VALUES (7, 4, 102, 'revision-1', 'table', 'item', 'active', NOW(), NOW())`).Error; err != nil {
		t.Fatalf("insert trigger dependency: %v", err)
	}
	if err := tx.Exec("UPDATE meta.meta_item SET deleted_at = NOW() WHERE id = 4").Error; err != nil {
		t.Fatalf("soft delete trigger item: %v", err)
	}
	assertLineageRelationStatus(t, tx, 4, "stale")
	assertLineageDependencyStatus(t, tx, 102, "stale")
}

func assertLineageRelationStatus(t *testing.T, db *gorm.DB, targetItemID uint, want string) {
	t.Helper()
	var status string
	if err := db.Raw(`SELECT status FROM meta.lineage_item_relations
		WHERE tenant_id = 7 AND source_item_id = 1 AND target_item_id = ?`, targetItemID).Scan(&status).Error; err != nil {
		t.Fatalf("read relation status for target %d: %v", targetItemID, err)
	}
	if status != want {
		t.Fatalf("relation status for target %d = %q, want %q", targetItemID, status, want)
	}
}

func assertLineageDependencyStatus(t *testing.T, db *gorm.DB, serviceID uint, want string) {
	t.Helper()
	var status string
	if err := db.Raw(`SELECT status FROM meta.lineage_service_dependencies
		WHERE tenant_id = 7 AND service_id = ?`, serviceID).Scan(&status).Error; err != nil {
		t.Fatalf("read dependency status for service %d: %v", serviceID, err)
	}
	if status != want {
		t.Fatalf("dependency status for service %d = %q, want %q", serviceID, status, want)
	}
}
