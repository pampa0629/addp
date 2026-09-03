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

func TestDetectorBindingUsesOnlyInstalledCapabilitiesAndProtectsTypeReference(t *testing.T) {
	db := openSecurityTestDB(t)
	svc := newTestDefinitionService(db)
	classification, err := svc.CreateClassification(models.DefinitionRequest{Code: "personal", Name: "个人信息"}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	grade, err := svc.CreateGrade(models.DefinitionRequest{Code: "l2", Name: "二级", RiskOrder: 2}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	dataType, err := svc.CreateType(models.SensitiveDataTypeRequest{Code: "contact", Name: "联系方式", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateDetector(models.DetectorRequest{CapabilityKey: "tenant.script/v1", SensitiveDataTypeID: dataType.ID, ConfidenceThreshold: 0.9}, 7, 11); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("unknown capability error = %v", err)
	}
	binding, err := svc.CreateDetector(models.DetectorRequest{CapabilityKey: models.FindingDetectorPhoneMetadataV2, SensitiveDataTypeID: dataType.ID, ConfidenceThreshold: 0.9}, 7, 11)
	if err != nil || !binding.Enabled || binding.ConfidenceThreshold != 0.9 || binding.Version != 1 {
		t.Fatalf("binding = %#v, err=%v", binding, err)
	}
	if _, err := svc.CreateDetector(models.DetectorRequest{CapabilityKey: models.FindingDetectorPhoneMetadataV2, SensitiveDataTypeID: dataType.ID, ConfidenceThreshold: 0.9}, 7, 11); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("duplicate capability error = %v", err)
	}
	if err := svc.DeleteType(dataType.ID, 7); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("delete referenced type error = %v", err)
	}
	if err := svc.DeleteDetector(binding.ID, 7, 11, binding.Version+1); !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("stale delete error = %v", err)
	}
	if err := svc.DeleteDetector(binding.ID, 7, 11, binding.Version); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteType(dataType.ID, 7); err != nil {
		t.Fatal(err)
	}

	capabilities := svc.ListDetectorCapabilities()
	if len(capabilities) != 3 || capabilities[0].Key != models.FindingDetectorPhoneMetadataV2 || capabilities[1].Key != models.FindingDetectorEmailMetadataV1 {
		t.Fatalf("capabilities = %#v", capabilities)
	}
	capabilities[0].SupportedItemTypes[0] = "mutated"
	if current := svc.ListDetectorCapabilities(); current[0].SupportedItemTypes[0] == "mutated" {
		t.Fatal("detector capability registry leaked mutable slices")
	}
	if current := svc.ListDetectorCapabilities(); current[0].RecommendedThreshold != 0.9 {
		t.Fatalf("recommended threshold = %v", current[0].RecommendedThreshold)
	}
	for _, capability := range svc.ListDetectorCapabilities() {
		if capability.MethodI18nKey == "" || capability.PrivacyI18nKey == "" || capability.LimitationsI18nKey == "" {
			t.Fatalf("capability explanation contract is incomplete: %#v", capability)
		}
	}
}

func TestDefinitionProfileExplicitlyAddsOnlyMissingTenantDefinitions(t *testing.T) {
	db := openSecurityTestDB(t)
	svc := newTestDefinitionService(db)
	if profiles := svc.ListDefinitionProfiles(); len(profiles) != 1 || profiles[0].Key != recommendedDefinitionProfileKey || profiles[0].ClassificationCount != 5 || profiles[0].GradeCount != 4 {
		t.Fatalf("profiles = %#v", profiles)
	}
	existing, err := svc.CreateGrade(models.DefinitionRequest{Code: "l3", Name: "租户自定义三级", RiskOrder: 30}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.ApplyDefinitionProfile(recommendedDefinitionProfileKey, "en", 7, 12)
	if err != nil {
		t.Fatal(err)
	}
	if result.CreatedClassifications != 5 || result.CreatedGrades != 3 {
		t.Fatalf("application result = %#v", result)
	}
	classifications, err := svc.ListClassifications(7)
	if err != nil || len(classifications) != 5 {
		t.Fatalf("classifications = %#v, err=%v", classifications, err)
	}
	var personalID int64
	var sensitivePersonal *models.SecurityClassification
	for index := range classifications {
		switch classifications[index].Code {
		case "personal_information":
			personalID = classifications[index].ID
			if classifications[index].Name != "Personal information" {
				t.Fatalf("localized classification = %#v", classifications[index])
			}
		case "sensitive_personal_information":
			sensitivePersonal = &classifications[index]
		}
	}
	if sensitivePersonal == nil || sensitivePersonal.ParentID == nil || *sensitivePersonal.ParentID != personalID {
		t.Fatalf("sensitive personal classification = %#v, personal id=%d", sensitivePersonal, personalID)
	}
	storedExisting, err := svc.GetGrade(existing.ID, 7)
	if err != nil || storedExisting.Name != "租户自定义三级" || storedExisting.RiskOrder != 30 {
		t.Fatalf("existing grade overwritten = %#v, err=%v", storedExisting, err)
	}
	second, err := svc.ApplyDefinitionProfile(recommendedDefinitionProfileKey, "en", 7, 12)
	if err != nil || second.CreatedClassifications != 0 || second.CreatedGrades != 0 {
		t.Fatalf("idempotent application = %#v, err=%v", second, err)
	}
	otherTenant, err := svc.ListClassifications(8)
	if err != nil || len(otherTenant) != 0 {
		t.Fatalf("other tenant classifications = %#v, err=%v", otherTenant, err)
	}
	if _, err := svc.ApplyDefinitionProfile("unknown", "en", 7, 12); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("unknown profile error = %v", err)
	}
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

func TestCandidateCompilerUsesDetectorBindingConfidenceThreshold(t *testing.T) {
	db, enrollments, finding, dataType, grade := prepareReviewablePhoneFinding(t)
	if err := db.Model(&models.SensitiveFinding{}).Where("id = ?", finding.ID).Update("confidence", 0.8).Error; err != nil {
		t.Fatal(err)
	}
	var baseline models.ProtectionBaseline
	if err := db.Where("tenant_id = ? AND sensitive_data_type_id = ? AND security_grade_id = ?", 7, dataType.ID, grade.ID).First(&baseline).Error; err != nil {
		t.Fatal(err)
	}
	svc := newTestDefinitionService(db)
	updated, err := svc.UpdateBaseline(baseline.ID, 7, 31, models.ProtectionBaselineRequest{
		SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID,
		Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1,
		KeepPrefix: 3, KeepSuffix: 4, InvalidValueEffect: dataprotection.EffectSuppress,
		Version: baseline.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertLatestManagerProjection(t, enrollments, 7, dataprotection.ProjectionStateEnrolling, "", 3)

	if err := db.Model(&models.Detector{}).
		Where("tenant_id = ? AND capability_key = ?", 7, models.FindingDetectorPhoneMetadataV2).
		Update("confidence_threshold", 0.7).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateBaseline(updated.ID, 7, 31, models.ProtectionBaselineRequest{
		SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID,
		Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1,
		KeepPrefix: 3, KeepSuffix: 4, InvalidValueEffect: dataprotection.EffectSuppress,
		Version: updated.Version,
	}); err != nil {
		t.Fatal(err)
	}
	assertLatestManagerProjection(t, enrollments, 7, dataprotection.ProjectionStateActive, dataprotection.EffectMask, 4)
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
		Version: dataType.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DefaultSecurityGradeID != newGrade.ID {
		t.Fatalf("updated type = %#v", updated)
	}
	assertLatestManagerProjection(t, enrollments, 7, dataprotection.ProjectionStateEnrolling, "", 3)

	assessments := NewAssessmentService(db, nil)
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
		Version: current.Version,
	}); err != nil {
		t.Fatal(err)
	}
	assertLatestManagerProjection(t, enrollments, 7, dataprotection.ProjectionStateEnrolling, "", 4)
}

func TestSensitiveDataTypeChangeDoesNotRecompileStaleFinding(t *testing.T) {
	db, enrollments, finding, dataType, _ := prepareReviewablePhoneFinding(t)
	if err := db.Model(&models.ProtectionEnrollment{}).
		Where("tenant_id = ? AND id = ?", 7, finding.EnrollmentID).
		Updates(map[string]interface{}{
			"latest_source_snapshot_hash":   "sha256:new-structure-without-phone",
			"latest_discovery_execution_id": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		}).Error; err != nil {
		t.Fatal(err)
	}
	svc := newTestDefinitionService(db)
	if _, err := svc.UpdateType(dataType.ID, 7, 31, models.SensitiveDataTypeRequest{
		Name: dataType.Name, Description: dataType.Description,
		SecurityClassificationID: dataType.SecurityClassificationID, DefaultSecurityGradeID: dataType.DefaultSecurityGradeID,
		Version: dataType.Version,
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
	dataType, err := svc.CreateType(models.SensitiveDataTypeRequest{Code: "phone_number", Name: "手机号码", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID}, 7, 11)
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
	dataType, _ := svc.CreateType(models.SensitiveDataTypeRequest{Code: "phone", Name: "手机号码", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID}, 7, 11)
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
	dataType, _ := svc.CreateType(models.SensitiveDataTypeRequest{Code: "phone", Name: "手机号码", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID}, 7, 11)

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
