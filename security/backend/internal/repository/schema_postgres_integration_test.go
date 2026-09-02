package repository

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
)

func TestSecurityMigrateAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SECURITY_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("SECURITY_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("DROP SCHEMA IF EXISTS security CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(tx); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(tx); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
	for _, table := range []string{
		"security_classifications", "security_grades", "sensitive_data_types", "protection_baselines",
		"sensitive_findings", "sensitive_finding_reviews", "resource_security_assessments", "resource_security_assessment_revisions",
		"protection_policies", "protection_policy_revisions",
	} {
		var exists bool
		if err := tx.Raw("SELECT to_regclass(?) IS NOT NULL", "security."+table).Scan(&exists).Error; err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("missing security.%s", table)
		}
	}
}
