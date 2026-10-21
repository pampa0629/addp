package service

import (
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The standard Meta gate owns addp_test. Fixtures live in a rolled-back
// transaction; the exercise checks PostgreSQL fact/identity semantics.
func TestDataItemAuthorizationFactsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard Meta PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec(`DROP SCHEMA IF EXISTS meta CASCADE; CREATE SCHEMA meta;
CREATE TABLE meta.meta_node (
id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL, engine_id BIGINT NOT NULL, parent_node_id BIGINT,
node_type TEXT NOT NULL, name TEXT NOT NULL, depth INTEGER NOT NULL, path TEXT, full_name TEXT,
scan_status TEXT, scanned_depth TEXT, scanned_at TIMESTAMPTZ, scan_error TEXT, attributes JSONB,
created_at TIMESTAMPTZ, deleted_at TIMESTAMPTZ);
CREATE TABLE meta.meta_item (
id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL, engine_id BIGINT NOT NULL, node_id BIGINT NOT NULL,
item_type TEXT NOT NULL, name TEXT NOT NULL, full_name TEXT, fingerprint TEXT NOT NULL,
row_count BIGINT, size_bytes BIGINT, data_updated_at TIMESTAMPTZ, scanned_at TIMESTAMPTZ,
scanned_depth TEXT, attributes JSONB, created_at TIMESTAMPTZ, deleted_at TIMESTAMPTZ);`).Error; err != nil {
		t.Fatal(err)
	}
	exerciseAuthorizationFacts(t, tx)
	exerciseFileAuthorizationFacts(t, tx)
 exerciseScannedFileAuthorizationFacts(t, tx)
}
