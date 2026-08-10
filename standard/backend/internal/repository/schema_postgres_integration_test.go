package repository

import (
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMigrateAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("STANDARD_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	for _, name := range []string{
		"uq_standard_domains_tenant_code",
		"uq_standard_code_items_set_code",
		"uq_standard_metric_dependencies_from_to",
		"uq_standard_dimension_hierarchy_levels_hierarchy_level",
	} {
		var count int64
		if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = 'standard' AND indexname = ?`, name).Scan(&count).Error; err != nil {
			t.Fatalf("query index %s: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("index %s count = %d, want 1", name, count)
		}
	}
	for _, name := range []string{
		"ck_standard_metric_dependencies_distinct",
		"ck_standard_dimension_hierarchy_levels_level_num",
	} {
		var count int64
		if err := db.Raw(`SELECT COUNT(*) FROM pg_constraint WHERE connamespace = 'standard'::regnamespace AND conname = ?`, name).Scan(&count).Error; err != nil {
			t.Fatalf("query constraint %s: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("constraint %s count = %d, want 1", name, count)
		}
	}
}
