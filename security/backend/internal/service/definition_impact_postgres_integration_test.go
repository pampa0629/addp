package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestEnrollmentLifecycleAgainstPostgres(t *testing.T) {
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

	service := NewEnrollmentService(tx)
	created, err := service.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(2, resourcetree.TypeTable, "business.customers"))
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Model(&models.ProtectionEnrollment{}).Where("tenant_id = ? AND id = ?", 7, created.ID).Updates(map[string]any{
		"state": models.EnrollmentStateReleased, "release_basis": models.ReleaseBasisManual,
		"release_reason": "postgres lifecycle test", "released_at": time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	source, err := service.Get(context.Background(), 7, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	reenrolled, err := service.ReEnroll(context.Background(), 7, 21, source.ID, models.ReEnrollProtectionEnrollmentRequest{Version: source.Version})
	if err != nil {
		t.Fatal(err)
	}
	if reenrolled.ID == source.ID || reenrolled.State != models.EnrollmentStateActivating || reenrolled.Target != source.Target {
		t.Fatalf("postgres re-enrollment = %#v", reenrolled)
	}
	if _, err := service.ReEnroll(context.Background(), 7, 21, source.ID, models.ReEnrollProtectionEnrollmentRequest{Version: source.Version}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("postgres duplicate active lifecycle error = %v", err)
	}
	unchanged, err := service.Get(context.Background(), 7, source.ID)
	if err != nil || unchanged.State != models.EnrollmentStateReleased || unchanged.ReleaseReason != "postgres lifecycle test" {
		t.Fatalf("postgres released audit = %#v, err=%v", unchanged, err)
	}
}

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
		DefaultSecurityGradeID: grade.ID,
	}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	otherType, err := definitions.CreateType(models.SensitiveDataTypeRequest{
		Code: "other", Name: "其他", SecurityClassificationID: classification.ID,
		DefaultSecurityGradeID: grade.ID,
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
	if _, err := definitions.CreateDetector(models.DetectorRequest{CapabilityKey: models.FindingDetectorPhoneMetadataV2, SensitiveDataTypeID: phoneType.ID, ConfidenceThreshold: 0.9}, 7, 11); err != nil {
		t.Fatal(err)
	}

	phoneEnrollment := createPostgresImpactEnrollment(t, tx, phoneType.ID, "phone", "sha256:phone")
	otherEnrollment := createPostgresImpactEnrollment(t, tx, otherType.ID, "other", "sha256:other")
	queueTypeID := phoneType.ID
	queue, err := NewDiscoveryService(tx, nil).ListFindings(context.Background(), 7, FindingListFilter{
		SnapshotScope: models.FindingSnapshotScopeCurrent, ReviewState: models.FindingReviewStatePending,
		SensitiveDataTypeID: &queueTypeID, DetectorVersion: models.FindingDetectorPhoneMetadataV2,
	}, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if queue.Total != 1 || queue.Data[0].EnrollmentID != phoneEnrollment.ID || queue.Data[0].Review != nil || queue.Data[0].TargetSnapshot.FullName != "public.phone" {
		t.Fatalf("postgres current pending queue = %#v", queue)
	}
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

func TestProtectionExemptionAssessmentRevisionAgainstPostgres(t *testing.T) {
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
		DefaultSecurityGradeID: grade.ID,
	}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := definitions.CreateBaseline(models.ProtectionBaselineRequest{
		SensitiveDataTypeID: phoneType.ID, SecurityGradeID: grade.ID,
		Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1,
		KeepPrefix: 3, KeepSuffix: 4, InvalidValueEffect: dataprotection.EffectSuppress,
	}, 7, 11); err != nil {
		t.Fatal(err)
	}

	enrollment := createPostgresImpactEnrollment(t, tx, phoneType.ID, "exemption_phone", "sha256:exemption-phone")
	var finding models.SensitiveFinding
	if err := tx.Where("tenant_id = ? AND enrollment_id = ? AND component_key = ?", 7, enrollment.ID, "userInfo.phone").First(&finding).Error; err != nil {
		t.Fatal(err)
	}
	assessments := NewAssessmentService(tx, nil)
	reviewed, err := assessments.ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{
		Decision: models.FindingReviewDecisionConfirm, Rationale: "确认 PostgreSQL 手机号字段",
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	exemptions := NewExemptionService(tx)
	exemptions.now = func() time.Time { return now }
	created, err := exemptions.Create(context.Background(), 7, 41, models.CreateProtectionExemptionRequest{
		AssessmentID: reviewed.Assessment.ID, ConsumerOwner: "manager", Action: managerPreviewAction,
		ExpiresAt: now.Add(time.Hour), Rationale: "按当前评估修订临时核验",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertLatestPostgresManagerProjectionEffect(t, tx, enrollment.ID, managerPreviewAction, dataprotection.EffectAllow)

	revised, err := assessments.Revise(context.Background(), 7, 22, reviewed.Assessment.ID, models.AssessmentRevisionRequest{
		Version: reviewed.Assessment.Version, SensitiveDataTypeID: reviewed.Assessment.Current.SensitiveDataTypeID,
		SecurityGradeID: reviewed.Assessment.Current.SecurityGradeID, Rationale: "复核后形成新的正式修订",
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := exemptions.Get(context.Background(), 7, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EffectiveState != models.ProtectionExemptionStateSuperseded || loaded.Current.AssessmentRevision == revised.CurrentRevision {
		t.Fatalf("postgres superseded exemption = %#v, assessment = %#v", loaded, revised)
	}
	assertLatestPostgresManagerProjectionEffect(t, tx, enrollment.ID, managerPreviewAction, dataprotection.EffectMask)

	reactivated, err := exemptions.Renew(context.Background(), 7, 42, created.ID, models.RenewProtectionExemptionRequest{
		Version: created.Version, ExpiresAt: now.Add(2 * time.Hour), Rationale: "新评估修订重新批准",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reactivated.EffectiveState != models.ProtectionExemptionStateActive || reactivated.Current.AssessmentRevision != revised.CurrentRevision {
		t.Fatalf("postgres reactivated exemption = %#v, assessment = %#v", reactivated, revised)
	}
	assertLatestPostgresManagerProjectionEffect(t, tx, enrollment.ID, managerPreviewAction, dataprotection.EffectAllow)
}

func createPostgresImpactEnrollment(t *testing.T, db *gorm.DB, dataTypeID int64, identity, snapshotHash string) models.ProtectionEnrollment {
	t.Helper()
	enrollments := NewEnrollmentService(db)
	created, err := enrollments.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(11, resourcetree.TypeTable, "public."+identity))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	discoveryExecutionID := uuid.NewString()
	component := dataprotection.Component{
		Key: "userInfo.phone", Path: []dataprotection.PathSegment{{Name: "userInfo", Container: "object"}, {Name: "phone", Container: "scalar"}},
		ValueType: "string", SchemaFingerprint: "sha256:schema-" + identity,
	}
	finding := models.SensitiveFinding{
		ID: uuid.NewString(), TenantID: 7, EnrollmentID: created.ID, DiscoveryExecutionID: discoveryExecutionID, ComponentKey: component.Key,
		SensitiveDataTypeID: dataTypeID, DetectorCode: models.FindingDetectorPhoneMetadataV2,
		DetectorVersion: models.FindingDetectorPhoneMetadataV2, Confidence: 0.99, Evidence: commonmodels.JSONMap{"signal": "field_name"},
		Component: component, SourceSnapshotHash: snapshotHash, ObservedAt: now, CreatedAt: now,
	}
	if err := db.Create(&finding).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ProtectionEnrollment{}).Where("tenant_id = ? AND id = ?", 7, created.ID).Updates(map[string]interface{}{
		"state": models.EnrollmentStateActive, "latest_source_snapshot_hash": snapshotHash, "latest_discovery_execution_id": discoveryExecutionID, "last_discovered_at": now,
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

func assertLatestPostgresManagerProjectionEffect(t *testing.T, db *gorm.DB, enrollmentID, action, effect string) {
	t.Helper()
	var record models.ProtectionProjectionRecord
	if err := db.Where("tenant_id = ? AND enrollment_id = ? AND consumer_owner = ?", 7, enrollmentID, "manager").First(&record).Error; err != nil {
		t.Fatal(err)
	}
	var projection dataprotection.Projection
	if err := json.Unmarshal([]byte(record.ProjectionPayload), &projection); err != nil {
		t.Fatal(err)
	}
	rule := projectionRule(t, &projection, action)
	if rule.Decision.Effect != effect {
		t.Fatalf("manager projection action %s effect = %s, want %s", action, rule.Decision.Effect, effect)
	}
	if err := projection.Validate(time.Now().UTC()); err != nil {
		t.Fatalf("manager projection validation: %v", err)
	}
}
