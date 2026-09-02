package service

import (
	"context"
	"errors"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProtectionBaselineChangesRecompileAffectedEnrollmentAtomically(t *testing.T) {
	db, enrollments, _, dataType, grade := prepareReviewablePhoneFinding(t)
	svc := newTestDefinitionService(db)
	var baseline models.ProtectionBaseline
	if err := db.Where("tenant_id = ? AND sensitive_data_type_id = ? AND security_grade_id = ?", 7, dataType.ID, grade.ID).First(&baseline).Error; err != nil {
		t.Fatal(err)
	}
	enabled := true
	updated, err := svc.UpdateBaseline(baseline.ID, 7, 31, models.ProtectionBaselineRequest{
		SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID,
		Effect: dataprotection.EffectSuppress, InvalidValueEffect: dataprotection.EffectSuppress,
		Enabled: &enabled, Version: baseline.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertLatestManagerProjection(t, enrollments, 7, dataprotection.ProjectionStateActive, dataprotection.EffectSuppress, 3)

	if err := svc.DeleteBaseline(updated.ID, 7, updated.Version-1); !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("stale baseline delete error = %v", err)
	}
	assertLatestManagerProjection(t, enrollments, 7, dataprotection.ProjectionStateActive, dataprotection.EffectSuppress, 3)
	if err := svc.DeleteBaseline(updated.ID, 7, updated.Version); err != nil {
		t.Fatal(err)
	}
	assertLatestManagerProjection(t, enrollments, 7, dataprotection.ProjectionStateEnrolling, "", 4)

	if _, err := svc.CreateBaseline(models.ProtectionBaselineRequest{
		SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID,
		Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1,
		KeepPrefix: 3, KeepSuffix: 4, InvalidValueEffect: dataprotection.EffectSuppress,
	}, 7, 31); err != nil {
		t.Fatal(err)
	}
	assertLatestManagerProjection(t, enrollments, 7, dataprotection.ProjectionStateActive, dataprotection.EffectMask, 5)
}

func TestProtectionBaselineUpdateRollsBackWhenProjectionCannotPublish(t *testing.T) {
	db, _, finding, dataType, grade := prepareReviewablePhoneFinding(t)
	svc := newTestDefinitionService(db)
	var baseline models.ProtectionBaseline
	if err := db.Where("tenant_id = ? AND sensitive_data_type_id = ? AND security_grade_id = ?", 7, dataType.ID, grade.ID).First(&baseline).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ProtectionProjectionRecord{}).
		Where("tenant_id = ? AND enrollment_id = ? AND consumer_owner = ?", 7, finding.EnrollmentID, "manager").
		Update("revision", "invalid").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateBaseline(baseline.ID, 7, 31, models.ProtectionBaselineRequest{
		SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID,
		Effect: dataprotection.EffectSuppress, InvalidValueEffect: dataprotection.EffectSuppress,
		Version: baseline.Version,
	}); err == nil {
		t.Fatal("UpdateBaseline() error = nil, want projection publication failure")
	}
	stored, err := svc.GetBaseline(baseline.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Version != baseline.Version || stored.Effect != dataprotection.EffectMask {
		t.Fatalf("baseline after failed projection = %#v", stored)
	}
}

func TestSensitiveDataTypeDefaultGradeChangeRecompilesOnlyCandidateFinding(t *testing.T) {
	db, enrollments, finding, dataType, oldGrade := prepareReviewablePhoneFinding(t)
	svc := newTestDefinitionService(db)
	newGrade, err := svc.CreateGrade(models.DefinitionRequest{Code: "l4", Name: "四级", RiskOrder: 4}, 7, 31)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.UpdateType(dataType.ID, 7, 31, models.SensitiveDataTypeRequest{
		Name: dataType.Name, Description: dataType.Description,
		SecurityClassificationID: dataType.SecurityClassificationID, DefaultSecurityGradeID: newGrade.ID,
		ProtectionThreshold: dataType.ProtectionThreshold, Version: dataType.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DefaultSecurityGradeID != newGrade.ID {
		t.Fatalf("updated type = %#v", updated)
	}
	assertLatestManagerProjection(t, enrollments, 7, dataprotection.ProjectionStateEnrolling, "", 3)

	assessments := NewAssessmentService(db)
	if _, err := assessments.ReviewFinding(context.Background(), 7, 32, finding.ID, models.FindingReviewRequest{
		Decision: models.FindingReviewDecisionConfirm, Rationale: "按当前类型默认等级确认",
	}); err != nil {
		t.Fatal(err)
	}
	assertLatestManagerProjection(t, enrollments, 7, dataprotection.ProjectionStateEnrolling, "", 4)
	current, err := svc.GetType(dataType.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateType(dataType.ID, 7, 31, models.SensitiveDataTypeRequest{
		Name: current.Name, Description: current.Description,
		SecurityClassificationID: current.SecurityClassificationID, DefaultSecurityGradeID: oldGrade.ID,
		ProtectionThreshold: current.ProtectionThreshold, Version: current.Version,
	}); err != nil {
		t.Fatal(err)
	}
	assertLatestManagerProjection(t, enrollments, 7, dataprotection.ProjectionStateEnrolling, "", 4)
}

func TestSensitiveDataTypeChangeDoesNotRecompileStaleFinding(t *testing.T) {
	db, enrollments, finding, dataType, _ := prepareReviewablePhoneFinding(t)
	if err := db.Model(&models.ProtectionEnrollment{}).
		Where("tenant_id = ? AND id = ?", 7, finding.EnrollmentID).
		Update("latest_source_snapshot_hash", "sha256:new-structure-without-phone").Error; err != nil {
		t.Fatal(err)
	}
	svc := newTestDefinitionService(db)
	if _, err := svc.UpdateType(dataType.ID, 7, 31, models.SensitiveDataTypeRequest{
		Name: dataType.Name, Description: dataType.Description,
		SecurityClassificationID: dataType.SecurityClassificationID, DefaultSecurityGradeID: dataType.DefaultSecurityGradeID,
		ProtectionThreshold: 0.95, Version: dataType.Version,
	}); err != nil {
		t.Fatal(err)
	}
	assertLatestManagerProjection(t, enrollments, 7, dataprotection.ProjectionStateActive, dataprotection.EffectMask, 2)
}

func TestSensitiveDataTypeCannotBeDeletedAfterFindingWhenBaselineIsRemoved(t *testing.T) {
	db, _, _, dataType, grade := prepareReviewablePhoneFinding(t)
	svc := newTestDefinitionService(db)
	var baseline models.ProtectionBaseline
	if err := db.Where("tenant_id = ? AND sensitive_data_type_id = ? AND security_grade_id = ?", 7, dataType.ID, grade.ID).First(&baseline).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBaseline(baseline.ID, 7, baseline.Version); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteType(dataType.ID, 7); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("DeleteType() error = %v, want Finding reference conflict", err)
	}
}

func assertLatestManagerProjection(t *testing.T, enrollments *EnrollmentService, tenantID int64, state, effect string, wantChanges int) {
	t.Helper()
	changes, err := enrollments.ListChanges(context.Background(), tenantID, "manager", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Changes) != wantChanges {
		t.Fatalf("manager change count = %d, want %d", len(changes.Changes), wantChanges)
	}
	projection := changes.Changes[len(changes.Changes)-1].Projection
	if projection == nil || projection.State != state {
		t.Fatalf("latest projection = %#v, want state %s", projection, state)
	}
	if effect == "" {
		if len(projection.Rules) != 0 {
			t.Fatalf("latest projection rules = %#v, want none", projection.Rules)
		}
		return
	}
	requireManagerProjectionEffects(t, projection, effect)
}

func TestDefinitionServiceKeepsTenantFactsIsolatedAndVersioned(t *testing.T) {
	db := openSecurityTestDB(t)
	svc := newTestDefinitionService(db)

	classification, err := svc.CreateClassification(models.DefinitionRequest{Code: "personal_information", Name: "个人信息"}, 7, 11)
	if err != nil {
		t.Fatalf("CreateClassification() error = %v", err)
	}
	if _, err := svc.GetClassification(classification.ID, 8); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("cross-tenant GetClassification() error = %v", err)
	}

	updated, err := svc.UpdateClassification(classification.ID, 7, 12, models.DefinitionRequest{Name: "个人信息更新", Version: 1})
	if err != nil {
		t.Fatalf("UpdateClassification() error = %v", err)
	}
	if updated.Version != 2 || updated.Name != "个人信息更新" {
		t.Fatalf("updated = %#v", updated)
	}
	if _, err := svc.UpdateClassification(classification.ID, 7, 12, models.DefinitionRequest{Name: "过期更新", Version: 1}); !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("stale update error = %v", err)
	}
}

func TestDefinitionServiceBuildsPhoneProtectionBaselineWithoutStandardIDs(t *testing.T) {
	db := openSecurityTestDB(t)
	svc := newTestDefinitionService(db)
	classification, _ := svc.CreateClassification(models.DefinitionRequest{Code: "personal_information", Name: "个人信息"}, 7, 11)
	grade, _ := svc.CreateGrade(models.DefinitionRequest{Code: "l3", Name: "较高风险", RiskOrder: 3}, 7, 11)
	dataType, err := svc.CreateType(models.SensitiveDataTypeRequest{Code: "phone_number", Name: "手机号码", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID, ProtectionThreshold: 0.9}, 7, 11)
	if err != nil {
		t.Fatalf("CreateType() error = %v", err)
	}
	baseline, err := svc.CreateBaseline(models.ProtectionBaselineRequest{SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID, Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1, KeepPrefix: 3, KeepSuffix: 4, InvalidValueEffect: dataprotection.EffectSuppress}, 7, 11)
	if err != nil {
		t.Fatalf("CreateBaseline() error = %v", err)
	}
	if baseline.KeepPrefix != 3 || baseline.KeepSuffix != 4 || !baseline.Enabled {
		t.Fatalf("baseline = %#v", baseline)
	}
}

func TestDefinitionServiceRejectsClassificationCycles(t *testing.T) {
	db := openSecurityTestDB(t)
	svc := newTestDefinitionService(db)
	root, _ := svc.CreateClassification(models.DefinitionRequest{Code: "root", Name: "根"}, 7, 11)
	child, _ := svc.CreateClassification(models.DefinitionRequest{Code: "child", Name: "子", ParentID: &root.ID}, 7, 11)

	_, err := svc.UpdateClassification(root.ID, 7, 11, models.DefinitionRequest{Name: root.Name, ParentID: &child.ID, Version: root.Version})
	if !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("cycle update error = %v, want ErrBadRequest", err)
	}
}

func TestDefinitionServiceRejectsDeletingReferencedDefinitions(t *testing.T) {
	db := openSecurityTestDB(t)
	svc := newTestDefinitionService(db)
	classification, _ := svc.CreateClassification(models.DefinitionRequest{Code: "personal", Name: "个人信息"}, 7, 11)
	grade, _ := svc.CreateGrade(models.DefinitionRequest{Code: "l3", Name: "三级", RiskOrder: 3}, 7, 11)
	dataType, _ := svc.CreateType(models.SensitiveDataTypeRequest{Code: "phone", Name: "手机号码", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID, ProtectionThreshold: 0.9}, 7, 11)
	_, _ = svc.CreateBaseline(models.ProtectionBaselineRequest{SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID, Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1, KeepPrefix: 3, KeepSuffix: 4}, 7, 11)

	for name, deleteDefinition := range map[string]func() error{
		"classification": func() error { return svc.DeleteClassification(classification.ID, 7) },
		"grade":          func() error { return svc.DeleteGrade(grade.ID, 7) },
		"type":           func() error { return svc.DeleteType(dataType.ID, 7) },
	} {
		if err := deleteDefinition(); !errors.Is(err, commonapi.ErrConflict) {
			t.Errorf("Delete %s error = %v, want ErrConflict", name, err)
		}
	}
}

func TestDefinitionServiceRequiresStableMaskingAlgorithm(t *testing.T) {
	db := openSecurityTestDB(t)
	svc := newTestDefinitionService(db)
	classification, _ := svc.CreateClassification(models.DefinitionRequest{Code: "personal", Name: "个人信息"}, 7, 11)
	grade, _ := svc.CreateGrade(models.DefinitionRequest{Code: "l3", Name: "三级", RiskOrder: 3}, 7, 11)
	dataType, _ := svc.CreateType(models.SensitiveDataTypeRequest{Code: "phone", Name: "手机号码", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID, ProtectionThreshold: 0.9}, 7, 11)

	_, err := svc.CreateBaseline(models.ProtectionBaselineRequest{SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID, Effect: dataprotection.EffectMask, Algorithm: "mask.keep_prefix_suffix", KeepPrefix: 3, KeepSuffix: 4}, 7, 11)
	if !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("unstable algorithm error = %v, want ErrBadRequest", err)
	}
}

func openSecurityTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS security").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func newTestDefinitionService(db *gorm.DB) *DefinitionService {
	return NewDefinitionService(db)
}
