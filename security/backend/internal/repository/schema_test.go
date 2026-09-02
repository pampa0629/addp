package repository

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
)

func TestMigrateCreatesOnlySecurityOwnedTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS security").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{
		"security_classifications", "security_grades", "sensitive_data_types", "protection_baselines",
		"protection_enrollments", "protection_projections", "protection_projection_changes", "protection_projection_acknowledgements",
		"sensitive_findings", "sensitive_finding_reviews", "resource_security_assessments", "resource_security_assessment_revisions",
		"protection_policies", "protection_policy_revisions",
	} {
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM security.sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("table %s count = %d", table, count)
		}
	}
}
