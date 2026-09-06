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
		"security_classifications", "security_grades", "sensitive_data_types", "detectors", "protection_baselines",
		"protection_enrollments", "protection_projections", "protection_projection_changes", "protection_projection_acknowledgements",
		"sensitive_findings", "sensitive_finding_reviews", "resource_security_assessments", "resource_security_assessment_revisions",
		"protection_policies", "protection_policy_revisions",
		"protection_exemptions", "protection_exemption_revisions",
		"protection_access_requests",
	} {
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM security.sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("table %s count = %d", table, count)
		}
	}
	var detectorColumns []struct{ Name string }
	if err := db.Raw("PRAGMA security.table_info(detectors)").Scan(&detectorColumns).Error; err != nil {
		t.Fatal(err)
	}
	hasThreshold := false
	for _, column := range detectorColumns {
		if column.Name == "confidence_threshold" {
			hasThreshold = true
		}
	}
	if !hasThreshold {
		t.Fatal("detectors.confidence_threshold was not created")
	}
	var typeColumns []struct{ Name string }
	if err := db.Raw("PRAGMA security.table_info(sensitive_data_types)").Scan(&typeColumns).Error; err != nil {
		t.Fatal(err)
	}
	for _, column := range typeColumns {
		if column.Name == "protection_threshold" {
			t.Fatal("legacy sensitive_data_types.protection_threshold still exists")
		}
	}
	var revisionColumns []struct {
		Name    string
		NotNull int `gorm:"column:notnull"`
	}
	if err := db.Raw("PRAGMA security.table_info(resource_security_assessment_revisions)").Scan(&revisionColumns).Error; err != nil {
		t.Fatal(err)
	}
	revisionColumnByName := make(map[string]int, len(revisionColumns))
	for _, column := range revisionColumns {
		revisionColumnByName[column.Name] = column.NotNull
	}
	if revisionColumnByName["source_kind"] != 1 || revisionColumnByName["conclusion"] != 1 {
		t.Fatalf("assessment revision governance columns = %#v", revisionColumnByName)
	}
	if revisionColumnByName["source_finding_id"] != 0 || revisionColumnByName["source_review_id"] != 0 {
		t.Fatalf("manual assessment source columns must be nullable: %#v", revisionColumnByName)
	}
	var exemptionRevisionColumns []struct {
		Name    string
		NotNull int `gorm:"column:notnull"`
	}
	if err := db.Raw("PRAGMA security.table_info(protection_exemption_revisions)").Scan(&exemptionRevisionColumns).Error; err != nil {
		t.Fatal(err)
	}
	exemptionColumnByName := make(map[string]int, len(exemptionRevisionColumns))
	for _, column := range exemptionRevisionColumns {
		exemptionColumnByName[column.Name] = column.NotNull
	}
	if exemptionColumnByName["assessment_revision"] != 1 {
		t.Fatalf("protection exemption assessment revision column = %#v", exemptionColumnByName)
	}
}
