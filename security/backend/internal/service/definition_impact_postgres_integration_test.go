package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDefinitionImpactAgainstPostgres(t *testing.T) {
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
	if err := repository.Migrate(tx); err != nil {
		t.Fatal(err)
	}

	definitions := NewDefinitionService(tx)
	classification, err := definitions.CreateClassification(models.DefinitionRequest{Code: "personal", Name: "个人信息"}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	grade, err := definitions.CreateGrade(models.DefinitionRequest{Code: "l3", Name: "三级", RiskOrder: 3}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	phoneType, err := definitions.CreateType(models.SensitiveDataTypeRequest{
		Code: "phone", Name: "手机号", SecurityClassificationID: classification.ID,
		DefaultSecurityGradeID: grade.ID, ProtectionThreshold: 0.9,
	}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	otherType, err := definitions.CreateType(models.SensitiveDataTypeRequest{
		Code: "other", Name: "其他", SecurityClassificationID: classification.ID,
		DefaultSecurityGradeID: grade.ID, ProtectionThreshold: 0.9,
	}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := definitions.CreateBaseline(models.ProtectionBaselineRequest{
		SensitiveDataTypeID: phoneType.ID, SecurityGradeID: grade.ID,
		Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1,
		KeepPrefix: 3, KeepSuffix: 4, InvalidValueEffect: dataprotection.EffectSuppress,
	}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}

	phoneEnrollment := createPostgresImpactEnrollment(t, tx, phoneType.ID, "phone", "sha256:phone")
	otherEnrollment := createPostgresImpactEnrollment(t, tx, otherType.ID, "other", "sha256:other")
	enabled := true
	updated, err := definitions.UpdateBaseline(baseline.ID, 7, 12, models.ProtectionBaselineRequest{
		SensitiveDataTypeID: phoneType.ID, SecurityGradeID: grade.ID,
		Effect: dataprotection.EffectSuppress, InvalidValueEffect: dataprotection.EffectSuppress,
		Enabled: &enabled, Version: baseline.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertPostgresManagerProjection(t, tx, phoneEnrollment.ID, "00000000000000000002", dataprotection.EffectSuppress)
	assertPostgresManagerProjection(t, tx, otherEnrollment.ID, "00000000000000000001", "")

	if err := tx.Model(&models.ProtectionProjectionRecord{}).
		Where("tenant_id = ? AND enrollment_id = ? AND consumer_owner = ?", 7, phoneEnrollment.ID, "manager").
		Update("revision", "invalid").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := definitions.UpdateBaseline(updated.ID, 7, 13, models.ProtectionBaselineRequest{
		SensitiveDataTypeID: phoneType.ID, SecurityGradeID: grade.ID,
		Effect: dataprotection.EffectDeny, InvalidValueEffect: dataprotection.EffectDeny,
		Enabled: &enabled, Version: updated.Version,
	}); err == nil {
		t.Fatal("UpdateBaseline() error = nil, want projection publication failure")
	}
	stored, err := definitions.GetBaseline(updated.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Version != updated.Version || stored.Effect != dataprotection.EffectSuppress {
		t.Fatalf("baseline after failed impact publication = %#v", stored)
	}
}

func createPostgresImpactEnrollment(t *testing.T, db *gorm.DB, dataTypeID int64, identity, snapshotHash string) models.ProtectionEnrollment {
	t.Helper()
	enrollments := NewEnrollmentService(db)
	created, err := enrollments.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(11, resourcetree.TypeTable, "public."+identity))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	component := dataprotection.Component{
		Key: "userInfo.phone", Path: []dataprotection.PathSegment{{Name: "userInfo", Container: "object"}, {Name: "phone", Container: "scalar"}},
		ValueType: "string", SchemaFingerprint: "sha256:schema-" + identity,
	}
	finding := models.SensitiveFinding{
		ID: uuid.NewString(), TenantID: 7, EnrollmentID: created.ID, ComponentKey: component.Key,
		SensitiveDataTypeID: dataTypeID, DetectorCode: models.FindingDetectorPhoneMetadataV1,
		DetectorVersion: "v1", Confidence: 0.99, Evidence: commonmodels.JSONMap{"signal": "field_name"},
		Component: component, SourceSnapshotHash: snapshotHash, ObservedAt: now, CreatedAt: now,
	}
	if err := db.Create(&finding).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ProtectionEnrollment{}).Where("tenant_id = ? AND id = ?", 7, created.ID).Updates(map[string]interface{}{
		"state": models.EnrollmentStateActive, "latest_source_snapshot_hash": snapshotHash, "last_discovered_at": now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	var enrollment models.ProtectionEnrollment
	if err := db.Where("tenant_id = ? AND id = ?", 7, created.ID).First(&enrollment).Error; err != nil {
		t.Fatal(err)
	}
	return enrollment
}

func assertPostgresManagerProjection(t *testing.T, db *gorm.DB, enrollmentID, revision, effect string) {
	t.Helper()
	var record models.ProtectionProjectionRecord
	if err := db.Where("tenant_id = ? AND enrollment_id = ? AND consumer_owner = ?", 7, enrollmentID, "manager").First(&record).Error; err != nil {
		t.Fatal(err)
	}
	if record.Revision != revision {
		t.Fatalf("manager projection revision = %s, want %s", record.Revision, revision)
	}
	if effect == "" {
		return
	}
	var projection dataprotection.Projection
	if err := json.Unmarshal([]byte(record.ProjectionPayload), &projection); err != nil {
		t.Fatal(err)
	}
	requireManagerProjectionEffects(t, &projection, effect)
	if err := projection.Validate(time.Now().UTC()); err != nil {
		t.Fatalf("manager projection validation: %v", err)
	}
}
