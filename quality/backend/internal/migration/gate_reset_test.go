package migration

import (
	"os"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Invoked only by the standard Quality PG gate, before and after its packages.
// Do not weaken production migration checksums to accommodate test reruns.
func TestQualityGateResetSchema(t *testing.T) {
	if os.Getenv("ADDP_QUALITY_GATE_RESET") != "1" {
		t.Skip("standard gate lifecycle only")
	}
	db, err := gorm.Open(postgres.Open(qualityMigrationIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var database string
	if err = db.Raw("SELECT current_database()").Scan(&database).Error; err != nil {
		t.Fatal(err)
	}
	if database != "addp_test" && !(os.Getenv("CI") == "true" && (strings.Contains(database, "test") || strings.Contains(database, "disposable"))) {
		t.Fatal("quality gate reset requires an isolated test database")
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", migrationLockID).Error; err != nil {
			return err
		}
		if tx.Migrator().HasTable("common.task_executions") {
			if err := tx.Exec("DELETE FROM common.task_executions WHERE module='quality'").Error; err != nil {
				return err
			}
		}
		return tx.Exec("DROP SCHEMA IF EXISTS quality CASCADE").Error
	})
	if err != nil {
		t.Fatal(err)
	}
}
