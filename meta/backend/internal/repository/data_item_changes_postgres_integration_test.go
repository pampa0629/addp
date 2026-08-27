package repository

import (
	"os"
	"testing"

	metaMigrations "github.com/addp/meta/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDataItemChangeMigrationAgainstPostgres(t *testing.T) {
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
		row_count BIGINT, size_bytes BIGINT, data_updated_at TIMESTAMPTZ, scanned_at TIMESTAMPTZ,
		scanned_depth VARCHAR(10), attributes JSONB, created_at TIMESTAMPTZ, deleted_at TIMESTAMPTZ
	)`).Error; err != nil {
		t.Fatalf("create meta_item: %v", err)
	}
	migration, err := metaMigrations.FS.ReadFile("019_add_data_item_changes.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if err := tx.Exec(string(migration)).Error; err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	structuredIdentityMigration, err := metaMigrations.FS.ReadFile("020_add_structured_table_identity_to_catalog_changes.sql")
	if err != nil {
		t.Fatalf("read structured identity migration: %v", err)
	}
	if err := tx.Exec(string(structuredIdentityMigration)).Error; err != nil {
		t.Fatalf("apply structured identity migration: %v", err)
	}
	if err := tx.Exec(`INSERT INTO meta.meta_item
		(tenant_id, engine_id, node_id, item_type, name, full_name, fingerprint, scanned_depth, attributes, created_at)
		VALUES (7, 12, 3, 'table', 'orders', 'public.orders', 'fingerprint-1', 'deep',
		'{"storage":{"schema_name":"public"},"type_info":{"table":{"fields":[{"name":"id","type":"int64"}]}}}'::jsonb, NOW())`).Error; err != nil {
		t.Fatalf("insert meta_item: %v", err)
	}
	if err := tx.Exec("UPDATE meta.meta_item SET deleted_at = NOW() WHERE fingerprint = 'fingerprint-1'").Error; err != nil {
		t.Fatalf("soft delete meta_item: %v", err)
	}
	if err := tx.Exec("UPDATE meta.meta_item SET deleted_at = NULL WHERE fingerprint = 'fingerprint-1'").Error; err != nil {
		t.Fatalf("restore meta_item: %v", err)
	}
	if err := tx.Exec("DELETE FROM meta.meta_item WHERE fingerprint = 'fingerprint-1'").Error; err != nil {
		t.Fatalf("hard delete meta_item: %v", err)
	}
	var operations []string
	if err := tx.Raw(`SELECT operation FROM meta.data_item_changes WHERE tenant_id = 7 ORDER BY id`).Scan(&operations).Error; err != nil {
		t.Fatalf("read changes: %v", err)
	}
	if len(operations) != 4 || operations[0] != "upsert" || operations[1] != "missing" || operations[2] != "upsert" || operations[3] != "missing" {
		t.Fatalf("operations = %#v", operations)
	}
	var fieldName string
	if err := tx.Raw(`SELECT snapshot->'fields'->0->>'name' FROM meta.data_item_changes WHERE tenant_id = 7 ORDER BY id LIMIT 1`).Scan(&fieldName).Error; err != nil {
		t.Fatalf("read field snapshot: %v", err)
	}
	if fieldName != "id" {
		t.Fatalf("field snapshot = %q, want id", fieldName)
	}
	var schemaName, tableName string
	if err := tx.Raw(`SELECT snapshot->>'schema_name', snapshot->>'table_name' FROM meta.data_item_changes WHERE tenant_id = 7 ORDER BY id LIMIT 1`).Row().Scan(&schemaName, &tableName); err != nil {
		t.Fatalf("read structured table identity: %v", err)
	}
	if schemaName != "public" || tableName != "orders" {
		t.Fatalf("structured table identity = %q.%q", schemaName, tableName)
	}
}
