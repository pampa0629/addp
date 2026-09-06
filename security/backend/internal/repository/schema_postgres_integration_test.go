package repository

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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
	now := time.Now().UTC().Truncate(time.Second)
	legacyPayload, err := json.Marshal(legacyProjectionV1{
		SchemaVersion: legacyProjectionSchemaV1,
		ProjectionID:  "11111111-1111-1111-1111-111111111111",
		Revision:      "00000000000000000001", ConsumerOwner: "manager", State: dataprotection.ProjectionStateActive,
		Target:             dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "sha256:postgres-migration"},
		SourceSnapshotHash: "sha256:postgres-migration-snapshot",
		Rules: []legacyProjectionRuleV1{{
			Action:    "preview",
			Component: dataprotection.Component{Key: "phone", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}}, ValueType: "string", SchemaFingerprint: "sha256:postgres-migration-schema"},
			Decision:  legacyProjectionDecisionV1{Effect: dataprotection.EffectSuppress, InvalidValueEffect: dataprotection.EffectSuppress},
		}},
		ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	legacyRecord := models.ProtectionProjectionRecord{
		ID: "11111111-1111-1111-1111-111111111111", TenantID: 7,
		EnrollmentID: "22222222-2222-2222-2222-222222222222", ConsumerOwner: "manager",
		Revision: "00000000000000000001", State: dataprotection.ProjectionStateActive,
		ProjectionPayload: string(legacyPayload), PublishedSequence: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&legacyRecord).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(tx); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
	if err := tx.First(&legacyRecord, "id = ?", legacyRecord.ID).Error; err != nil {
		t.Fatal(err)
	}
	var migratedProjection dataprotection.Projection
	if err := json.Unmarshal([]byte(legacyRecord.ProjectionPayload), &migratedProjection); err != nil {
		t.Fatal(err)
	}
	if migratedProjection.SchemaVersion != dataprotection.ProjectionSchemaV2 {
		t.Fatalf("migrated projection schema = %s", migratedProjection.SchemaVersion)
	}
	for _, table := range []string{
		"security_classifications", "security_grades", "sensitive_data_types", "detectors", "protection_baselines",
		"sensitive_findings", "sensitive_finding_reviews", "resource_security_assessments", "resource_security_assessment_revisions",
		"protection_policies", "protection_policy_revisions",
		"protection_exemptions", "protection_exemption_revisions", "protection_access_requests",
	} {
		var exists bool
		if err := tx.Raw("SELECT to_regclass(?) IS NOT NULL", "security."+table).Scan(&exists).Error; err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("missing security.%s", table)
		}
	}
	var detectorThresholdExists bool
	if err := tx.Raw(`SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'security' AND table_name = 'detectors' AND column_name = 'confidence_threshold'
	)`).Scan(&detectorThresholdExists).Error; err != nil {
		t.Fatal(err)
	}
	if !detectorThresholdExists {
		t.Fatal("security.detectors.confidence_threshold is missing")
	}
	var legacyThresholdExists bool
	if err := tx.Raw(`SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'security' AND table_name = 'sensitive_data_types' AND column_name = 'protection_threshold'
	)`).Scan(&legacyThresholdExists).Error; err != nil {
		t.Fatal(err)
	}
	if legacyThresholdExists {
		t.Fatal("legacy security.sensitive_data_types.protection_threshold still exists")
	}
	var assessmentRevisionColumns int64
	if err := tx.Raw(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'security'
		  AND table_name = 'resource_security_assessment_revisions'
		  AND ((column_name IN ('source_kind', 'conclusion') AND is_nullable = 'NO')
		    OR (column_name IN ('source_finding_id', 'source_review_id') AND is_nullable = 'YES'))`).Scan(&assessmentRevisionColumns).Error; err != nil {
		t.Fatal(err)
	}
	if assessmentRevisionColumns != 4 {
		t.Fatalf("assessment revision governance column contract count = %d, want 4", assessmentRevisionColumns)
	}
	var exemptionAssessmentRevisionExists bool
	if err := tx.Raw(`SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'security' AND table_name = 'protection_exemption_revisions'
		  AND column_name = 'assessment_revision' AND is_nullable = 'NO'
	)`).Scan(&exemptionAssessmentRevisionExists).Error; err != nil {
		t.Fatal(err)
	}
	if !exemptionAssessmentRevisionExists {
		t.Fatal("security.protection_exemption_revisions.assessment_revision is missing or nullable")
	}
}
