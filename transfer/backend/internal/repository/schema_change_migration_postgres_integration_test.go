package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/transfer/internal/testpg"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresSchemaChangeMetaScanClaimMigration(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	connInfo := testpg.ConnInfoFromEnv(t)
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN failed: %v", err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL failed: %v", err)
	}

	const schema = "transfer_schema_change_migration_test"
	if err := db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error; err != nil {
		t.Fatalf("drop stale migration test schema: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("create migration test schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error })

	if err := db.Exec(`
		CREATE TABLE transfer_schema_change_migration_test.schema_change_requests (
			id BIGSERIAL PRIMARY KEY,
			metadata_scan_status VARCHAR(20),
			metadata_scan_execution_id VARCHAR(36),
			metadata_scan_error TEXT
		);
		INSERT INTO transfer_schema_change_migration_test.schema_change_requests (metadata_scan_status)
		VALUES ('running');
	`).Error; err != nil {
		t.Fatalf("create pre-019 schema fixture: %v", err)
	}

	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "019_add_schema_change_meta_scan_claim.sql"))
	if err != nil {
		t.Fatalf("read 019 migration: %v", err)
	}
	migrationSQL := strings.ReplaceAll(string(migration), "transfer.", schema+".")
	if err := db.Exec(migrationSQL).Error; err != nil {
		t.Fatalf("apply 019 migration: %v", err)
	}
	if err := db.Exec(migrationSQL).Error; err != nil {
		t.Fatalf("reapply 019 migration: %v", err)
	}

	var migrated struct {
		Status      string
		ClaimToken  string
		LeaseUntil  *time.Time
		Attempt     int64
		ExecutionID string
		Error       string
	}
	if err := db.Raw(`
		SELECT metadata_scan_status AS status,
		       metadata_scan_claim_token AS claim_token,
		       metadata_scan_lease_until AS lease_until,
		       metadata_scan_attempt AS attempt,
		       metadata_scan_execution_id AS execution_id,
		       metadata_scan_error AS error
		FROM transfer_schema_change_migration_test.schema_change_requests
		WHERE id = 1
	`).Scan(&migrated).Error; err != nil {
		t.Fatalf("load migrated request: %v", err)
	}
	if migrated.Status != "pending" || migrated.ClaimToken != "" || migrated.LeaseUntil != nil || migrated.Attempt != 0 || migrated.ExecutionID != "" || migrated.Error != "" {
		t.Fatalf("migrated request = %#v", migrated)
	}

	if err := db.Exec(`
		UPDATE transfer_schema_change_migration_test.schema_change_requests
		SET metadata_scan_status = 'running'
		WHERE id = 1
	`).Error; err == nil {
		t.Fatal("running claim without token and lease must violate fencing constraint")
	}
	if err := db.Exec(`
		UPDATE transfer_schema_change_migration_test.schema_change_requests
		SET metadata_scan_status = 'running',
		    metadata_scan_claim_token = '11111111-1111-1111-1111-111111111111',
		    metadata_scan_lease_until = NOW() + INTERVAL '1 minute'
		WHERE id = 1
	`).Error; err != nil {
		t.Fatalf("valid running claim rejected: %v", err)
	}
	if err := db.Exec(`
		UPDATE transfer_schema_change_migration_test.schema_change_requests
		SET metadata_scan_attempt = -1
		WHERE id = 1
	`).Error; err == nil {
		t.Fatal("negative metadata scan attempt must violate constraint")
	}
}
