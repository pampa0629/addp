package migration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresRuleKeyIdentityV2Vector(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(qualityMigrationIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatalf("ensure execution store: %v", err)
	}
	if err := NewRunner(db).Run(context.Background()); err != nil {
		t.Fatalf("run quality migrations: %v", err)
	}

	catalog, err := ReadCatalog(EmbeddedSQL, DefaultMigrationsRoot)
	if err != nil {
		t.Fatalf("read migration catalog: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin migration vector transaction: %v", tx.Error)
	}
	defer tx.Rollback()

	const (
		tenantID    = int64(9_800_000_001)
		elementID   = int64(9_800_000_001)
		application = int64(9_800_000_001)
		oldRuleKey  = "00000000-0000-4000-8000-000000000001"
		expectedKey = "c0c3083c-9caf-8f5b-8f55-0811305fdee6"
	)
	if err := tx.Exec(`DELETE FROM quality.rule_applications WHERE id = ? OR tenant_id = ?`, application, tenantID).Error; err != nil {
		t.Fatalf("clear identity vector fixture: %v", err)
	}
	if err := tx.Exec(`INSERT INTO quality.rule_applications (
		id, tenant_id, element_id, element_revision_id, engine_id, schema_name, table_name, column_name, rule_config, created_by, created_at, updated_at
	) VALUES (?, ?, ?, ?, 1, 'public', 'identity_vector', 'value', ?::jsonb, 1, NOW(), NOW())`,
		application,
		tenantID,
		elementID,
		elementID+1,
		`{"schema_version":"addp.quality.rules/v1","rules":[{"rule_key":"`+oldRuleKey+`","type":"not_null","enabled":true,"severity":"error","message":"","params":{}}]}`,
	).Error; err != nil {
		t.Fatalf("insert identity vector application: %v", err)
	}
	if err := tx.Exec(`INSERT INTO quality.issues (
		tenant_id, execution_id, last_execution_id, rule_application_id, rule_key, rule_type, severity,
		message, column_name, table_name, schema_name, engine_id, failed_count, total_count, pass_rate,
		resolution_note, created_at, updated_at
	) VALUES (?, 'identity-vector', 'identity-vector', ?, ?::uuid, 'not_null', 'error',
		'', 'value', 'identity_vector', 'public', 1, 1, 1, 0, '', NOW(), NOW())`, tenantID, application, oldRuleKey).Error; err != nil {
		t.Fatalf("insert identity vector issue: %v", err)
	}
	if err := tx.Exec(catalog.Files[5].Contents).Error; err != nil {
		t.Fatalf("execute identity v2 migration: %v", err)
	}

	var keys struct {
		ApplicationKey string
		IssueKey       string
	}
	if err := tx.Raw(`SELECT
		application.rule_config->'rules'->0->>'rule_key' AS application_key,
		issue.rule_key::text AS issue_key
	FROM quality.rule_applications AS application
	JOIN quality.issues AS issue
	  ON issue.tenant_id = application.tenant_id
	 AND issue.rule_application_id = application.id
	WHERE application.id = ?`, application).Scan(&keys).Error; err != nil {
		t.Fatalf("load migrated identity vector: %v", err)
	}
	if keys.ApplicationKey != expectedKey || keys.IssueKey != expectedKey {
		t.Fatalf("migrated identity vector = application %q, issue %q, want %q", keys.ApplicationKey, keys.IssueKey, expectedKey)
	}
}

func qualityMigrationIntegrationDSN() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		qualityMigrationIntegrationEnv("ADDP_TEST_POSTGRES_HOST", "localhost"),
		qualityMigrationIntegrationEnv("ADDP_TEST_POSTGRES_PORT", "15432"),
		qualityMigrationIntegrationEnv("ADDP_TEST_POSTGRES_USER", "addp"),
		qualityMigrationIntegrationEnv("ADDP_TEST_POSTGRES_PASSWORD", "addp_password"),
		qualityMigrationIntegrationEnv("ADDP_TEST_POSTGRES_DATABASE", "addp_test"),
		qualityMigrationIntegrationEnv("ADDP_TEST_POSTGRES_SSLMODE", "disable"),
	)
}

func qualityMigrationIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
