package service

import (
	"context"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/resourcetree"
	"github.com/addp/security/internal/models"
	"gorm.io/gorm"
)

func TestFindingReviewCreatesAssessmentAndFormalCompilerRevision(t *testing.T) {
	db, enrollments, finding, dataType, grade := prepareReviewablePhoneFinding(t)
	assessments := NewAssessmentService(db)
	result, err := assessments.ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{
		Decision: models.FindingReviewDecisionConfirm, Rationale: "字段含义与检测证据一致",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Assessment == nil || result.Assessment.Version != 1 || result.Assessment.CurrentRevision != 1 || result.Assessment.Current.SensitiveDataTypeID != dataType.ID || result.Assessment.Current.SecurityGradeID != grade.ID || len(result.Assessment.History) != 1 {
		t.Fatalf("review result = %#v", result)
	}
	discoveries := NewDiscoveryService(db, nil)
	listed, err := discoveries.ListFindings(context.Background(), 7, finding.EnrollmentID, finding.SourceSnapshotHash, 1, 20)
	if err != nil || listed.Total != 1 || listed.Data[0].Review == nil || listed.Data[0].Review.Decision != models.FindingReviewDecisionConfirm {
		t.Fatalf("review-enriched findings = %#v, err=%v", listed, err)
	}
	currentEnrollment, err := enrollments.Get(context.Background(), 7, finding.EnrollmentID)
	if err != nil || currentEnrollment.DiscoverySummary.FindingCount != 1 || currentEnrollment.DiscoverySummary.PendingReviewCount != 0 || currentEnrollment.DiscoverySummary.ReviewedCount != 1 {
		t.Fatalf("reviewed discovery summary = %#v, err=%v", currentEnrollment, err)
	}
	if _, err := assessments.ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "重复"}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("duplicate review error = %v", err)
	}

	revised, err := assessments.Revise(context.Background(), 7, 22, result.Assessment.ID, models.AssessmentRevisionRequest{
		Version: 1, SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID, Rationale: "正式复核修订",
	})
	if err != nil {
		t.Fatal(err)
	}
	if revised.Version != 2 || revised.CurrentRevision != 2 || revised.Current.Revision != 2 || revised.Current.CreatedBy != 22 || len(revised.History) != 2 {
		t.Fatalf("revised assessment = %#v", revised)
	}
	changes, err := enrollments.ListChanges(context.Background(), 7, "manager", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Changes) != 4 || changes.Changes[2].Projection == nil || changes.Changes[2].Projection.State != dataprotection.ProjectionStateActive || changes.Changes[3].Projection == nil || changes.Changes[3].Projection.State != dataprotection.ProjectionStateActive {
		t.Fatalf("manager changes = %#v", changes.Changes)
	}
}

func TestFindingReviewRejectsHistoricalSnapshot(t *testing.T) {
	db, _, finding, _, _ := prepareReviewablePhoneFinding(t)
	if err := db.Model(&models.ProtectionEnrollment{}).
		Where("tenant_id = ? AND id = ?", 7, finding.EnrollmentID).
		Update("latest_source_snapshot_hash", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Error; err != nil {
		t.Fatal(err)
	}
	_, err := NewAssessmentService(db).ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{
		Decision: models.FindingReviewDecisionConfirm, Rationale: "历史快照不应再复核",
	})
	if !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("historical finding review error = %v", err)
	}
}

func TestRejectedFindingReturnsManagerToEnrollingDeny(t *testing.T) {
	db, enrollments, finding, _, _ := prepareReviewablePhoneFinding(t)
	assessments := NewAssessmentService(db)
	result, err := assessments.ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{
		Decision: models.FindingReviewDecisionReject, Rationale: "字段名与业务含义不一致",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Assessment != nil || result.Review.Decision != models.FindingReviewDecisionReject {
		t.Fatalf("reject result = %#v", result)
	}
	listed, err := assessments.List(context.Background(), 7, 1, 20)
	if err != nil || listed.Total != 0 {
		t.Fatalf("assessments = %#v, err=%v", listed, err)
	}
	changes, err := enrollments.ListChanges(context.Background(), 7, "manager", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Changes) != 3 || changes.Changes[2].Projection == nil || changes.Changes[2].Projection.State != dataprotection.ProjectionStateEnrolling || len(changes.Changes[2].Projection.Rules) != 0 {
		t.Fatalf("manager changes = %#v", changes.Changes)
	}
}

func prepareReviewablePhoneFinding(t *testing.T) (*gorm.DB, *EnrollmentService, models.SensitiveFinding, *models.SensitiveDataType, *models.SecurityGrade) {
	t.Helper()
	db := openSecurityTestDB(t)
	definitions := newTestDefinitionService(db)
	classification, err := definitions.CreateClassification(models.DefinitionRequest{Code: "personal", Name: "个人信息"}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	grade, err := definitions.CreateGrade(models.DefinitionRequest{Code: "l3", Name: "三级", RiskOrder: 3}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	dataType, err := definitions.CreateType(models.SensitiveDataTypeRequest{Code: "phone", Name: "手机号", SecurityClassificationID: classification.ID, DefaultSecurityGradeID: grade.ID, ProtectionThreshold: 0.9}, 7, 11)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := definitions.CreateBaseline(models.ProtectionBaselineRequest{SensitiveDataTypeID: dataType.ID, SecurityGradeID: grade.ID, Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV1, KeepPrefix: 3, KeepSuffix: 4, InvalidValueEffect: dataprotection.EffectSuppress}, 7, 11); err != nil {
		t.Fatal(err)
	}
	enrollments := NewEnrollmentService(db)
	created, err := enrollments.Create(context.Background(), 7, 11, testDataItemEnrollmentRequest(11, resourcetree.TypeCollection, "Outdoor.Persons"))
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range requiredProtectionOwners {
		changes, err := enrollments.ListChanges(context.Background(), 7, owner, "", 20)
		if err != nil {
			t.Fatal(err)
		}
		if err := enrollments.Acknowledge(context.Background(), 7, owner, changes.NextCursor); err != nil {
			t.Fatal(err)
		}
	}
	fields := []datatype.FieldInfo{
		{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "userInfo.phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	hash, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	facts := &dataprotection.DataItemSecurityFacts{SchemaVersion: dataprotection.DataItemSecurityFactsSchemaV1, ItemFingerprint: created.Target.ResourceIdentity, ItemType: "collection", Fields: fields, SourceSnapshotHash: hash, ObservedAt: time.Now().UTC()}
	discoveries := NewDiscoveryService(db, func(uint) SecurityFactsReader { return staticSecurityFactsReader{facts: facts} })
	execution, lease, err := discoveries.ClaimNext(context.Background(), "security-review-test", time.Now().UTC(), time.Minute)
	if err != nil || execution == nil || lease == nil {
		t.Fatalf("claim = %#v/%#v, err=%v", execution, lease, err)
	}
	if err := discoveries.Execute(context.Background(), execution, *lease); err != nil {
		t.Fatal(err)
	}
	findings, err := discoveries.ListFindings(context.Background(), 7, created.ID, hash, 1, 20)
	if err != nil || findings.Total != 1 {
		t.Fatalf("findings = %#v, err=%v", findings, err)
	}
	return db, enrollments, findings.Data[0].SensitiveFinding, dataType, grade
}
