package repository

import (
	"testing"

	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestStructuredParametersMigrateLegacyColumnsAndPolicy(t *testing.T) {
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
	verifyStructuredParametersMigration(t, db)
}

func verifyStructuredParametersMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	baseline := models.ProtectionBaseline{TenantID: 7, SensitiveDataTypeID: 41, SecurityGradeID: 43, Effect: "mask", Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, InvalidValueEffect: "deny", Enabled: true, CreatedBy: 11}
	if err := db.Create(&baseline).Error; err != nil {
		t.Fatal(err)
	}
	assessmentID := "aaaaaaaa-1111-4111-8111-111111111111"
	policyID := "bbbbbbbb-1111-4111-8111-111111111111"
	for _, value := range []any{
		&models.ResourceSecurityAssessment{ID: assessmentID, TenantID: 7, EnrollmentID: "cccccccc-1111-4111-8111-111111111111", ComponentKey: "phone", CurrentRevision: 1},
		&models.ResourceSecurityAssessmentRevision{ID: "dddddddd-1111-4111-8111-111111111111", TenantID: 7, AssessmentID: assessmentID, Revision: 1, SensitiveDataTypeID: 41, SecurityGradeID: 43, Component: dataprotection.Component{Key: "phone", ValueType: "string"}},
		&models.ProtectionPolicy{ID: policyID, TenantID: 7, AssessmentID: assessmentID, ConsumerOwner: "manager", Action: "preview", State: "active", CurrentRevision: 1},
		&models.ProtectionPolicyRevision{ID: "eeeeeeee-1111-4111-8111-111111111111", TenantID: 7, PolicyID: policyID, Revision: 1, State: "active", Effect: "mask"},
	} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{
		"ALTER TABLE security.protection_baselines ADD COLUMN keep_prefix INTEGER DEFAULT 2",
		"ALTER TABLE security.protection_baselines ADD COLUMN keep_suffix INTEGER DEFAULT 3",
		"ALTER TABLE security.protection_baselines DROP COLUMN parameters",
		"ALTER TABLE security.protection_baselines DROP COLUMN allowed_algorithms",
		"ALTER TABLE security.protection_policy_revisions DROP COLUMN algorithm",
		"ALTER TABLE security.protection_policy_revisions DROP COLUMN parameters",
		"ALTER TABLE security.protection_policy_revisions DROP COLUMN invalid_value_effect",
	} {
		if err := db.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateStructuredProtectionParameters(db); err != nil {
		t.Fatal(err)
	}
	if err := migrateStructuredProtectionParameters(db); err != nil {
		t.Fatal(err)
	}
	prefix, err := securityColumnExists(db, "protection_baselines", "keep_prefix")
	if err != nil {
		t.Fatal(err)
	}
	suffix, err := securityColumnExists(db, "protection_baselines", "keep_suffix")
	if err != nil {
		t.Fatal(err)
	}
	if prefix || suffix {
		t.Fatal("old columns remain")
	}
	if err := db.First(&baseline, baseline.ID).Error; err != nil {
		t.Fatal(err)
	}
	if baseline.Parameters["prefix_runes"] != float64(2) || baseline.Parameters["suffix_runes"] != float64(3) || len(baseline.AllowedAlgorithms) != 1 {
		t.Fatal("baseline parameters were not migrated")
	}
	var revision models.ProtectionPolicyRevision
	if err := db.First(&revision, "policy_id = ?", policyID).Error; err != nil {
		t.Fatal(err)
	}
	if revision.Algorithm != baseline.Algorithm || revision.Parameters["mask_rune"] != "*" || revision.InvalidValueEffect != "deny" {
		t.Fatal("legacy policy was not frozen from its baseline")
	}
}
