package service

import (
	"context"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
)

func TestProtectionExemptionPublishesTimedAllowAndRevokesToPolicyFallback(t *testing.T) {
	db, enrollments, finding, _, _ := prepareReviewablePhoneFinding(t)
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{
		Decision: models.FindingReviewDecisionConfirm, Rationale: "确认手机号字段",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPolicyService(db).Create(context.Background(), 7, 31, models.CreateProtectionPolicyRequest{
		AssessmentID: reviewed.Assessment.ID, ConsumerOwner: "manager", Action: managerPreviewAction,
		Effect: dataprotection.EffectSuppress, Rationale: "默认不返回手机号字段",
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	exemptions := NewExemptionService(db)
	exemptions.now = func() time.Time { return now }
	created, err := exemptions.Create(context.Background(), 7, 41, models.CreateProtectionExemptionRequest{
		AssessmentID: reviewed.Assessment.ID, ConsumerOwner: "manager", Action: managerPreviewAction,
		ExpiresAt: now.Add(24 * time.Hour), Rationale: "工单 SEC-2026-001 已批准临时核验原值",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.EffectiveState != models.ProtectionExemptionStateActive || created.Version != 1 || len(created.History) != 1 {
		t.Fatalf("created exemption = %#v", created)
	}
	listed, err := exemptions.List(context.Background(), 7, reviewed.Assessment.EnrollmentID, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 || len(listed.Data) != 1 || listed.Data[0].ID != created.ID || len(listed.Data[0].History) != 0 {
		t.Fatalf("listed exemptions = %#v", listed)
	}
	if _, err := exemptions.Create(context.Background(), 7, 41, models.CreateProtectionExemptionRequest{
		AssessmentID: reviewed.Assessment.ID, ConsumerOwner: "manager", Action: managerPreviewAction,
		ExpiresAt: now.Add(time.Hour), Rationale: "重复绑定",
	}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("duplicate create error = %v", err)
	}

	changes, err := enrollments.ListChanges(context.Background(), 7, "manager", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	projection := changes.Changes[len(changes.Changes)-1].Projection
	preview := projectionRule(t, projection, managerPreviewAction)
	if preview.Decision.Effect != dataprotection.EffectAllow || preview.Decision.ValidUntil == nil || preview.Decision.Fallback == nil || preview.Decision.Fallback.Effect != dataprotection.EffectSuppress {
		t.Fatalf("exemption decision = %#v", preview.Decision)
	}
	profile := projectionRule(t, projection, managerProfileAction)
	if profile.Decision.Effect != dataprotection.EffectSuppress || profile.Decision.ValidUntil != nil {
		t.Fatalf("profile decision = %#v", profile.Decision)
	}

	renewed, err := exemptions.Renew(context.Background(), 7, 42, created.ID, models.RenewProtectionExemptionRequest{
		Version: created.Version, ExpiresAt: now.Add(48 * time.Hour), Rationale: "工单延期获批",
	})
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Version != 2 || renewed.CurrentRevision != 2 || len(renewed.History) != 2 {
		t.Fatalf("renewed exemption = %#v", renewed)
	}
	if _, err := exemptions.Renew(context.Background(), 7, 42, created.ID, models.RenewProtectionExemptionRequest{
		Version: renewed.Version, ExpiresAt: now.Add(31 * 24 * time.Hour), Rationale: "超过期限",
	}); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("oversized renewal error = %v", err)
	}

	revoked, err := exemptions.Revoke(context.Background(), 7, 43, created.ID, models.RevokeProtectionExemptionRequest{
		Version: renewed.Version, Rationale: "核验完成立即撤销",
	})
	if err != nil {
		t.Fatal(err)
	}
	if revoked.EffectiveState != models.ProtectionExemptionStateRevoked || revoked.Version != 3 || len(revoked.History) != 3 {
		t.Fatalf("revoked exemption = %#v", revoked)
	}
	changes, err = enrollments.ListChanges(context.Background(), 7, "manager", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	preview = projectionRule(t, changes.Changes[len(changes.Changes)-1].Projection, managerPreviewAction)
	if preview.Decision.Effect != dataprotection.EffectSuppress || preview.Decision.ValidUntil != nil {
		t.Fatalf("revoked fallback decision = %#v", preview.Decision)
	}
}

func TestProtectionExemptionExpiredStateAndBindingValidation(t *testing.T) {
	db, _, finding, _, _ := prepareReviewablePhoneFinding(t)
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{
		Decision: models.FindingReviewDecisionConfirm, Rationale: "确认手机号字段",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	exemptions := NewExemptionService(db)
	exemptions.now = func() time.Time { return now }
	created, err := exemptions.Create(context.Background(), 7, 41, models.CreateProtectionExemptionRequest{
		AssessmentID: reviewed.Assessment.ID, ConsumerOwner: "transfer", Action: transferExportAction,
		ExpiresAt: now.Add(time.Hour), Rationale: "批准导出原值核验",
	})
	if err != nil {
		t.Fatal(err)
	}
	exemptions.now = func() time.Time { return now.Add(2 * time.Hour) }
	loaded, err := exemptions.Get(context.Background(), 7, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EffectiveState != models.ProtectionExemptionStateExpired || loaded.State != models.ProtectionExemptionStateActive {
		t.Fatalf("expired exemption = %#v", loaded)
	}
	if _, err := exemptions.Create(context.Background(), 7, 41, models.CreateProtectionExemptionRequest{
		AssessmentID: reviewed.Assessment.ID, ConsumerOwner: "manager", Action: managerProfileAction,
		ExpiresAt: now.Add(3 * time.Hour), Rationale: "不支持的动作",
	}); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("unsupported binding error = %v", err)
	}
}

func TestProtectionExemptionDoesNotSurviveAssessmentRevision(t *testing.T) {
	db, enrollments, finding, _, _ := prepareReviewablePhoneFinding(t)
	assessmentService := NewAssessmentService(db, nil)
	reviewed, err := assessmentService.ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{
		Decision: models.FindingReviewDecisionConfirm, Rationale: "确认手机号字段",
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	exemptions := NewExemptionService(db)
	exemptions.now = func() time.Time { return now }
	created, err := exemptions.Create(context.Background(), 7, 41, models.CreateProtectionExemptionRequest{
		AssessmentID: reviewed.Assessment.ID, ConsumerOwner: "manager", Action: managerPreviewAction,
		ExpiresAt: now.Add(time.Hour), Rationale: "按当前评估修订临时核验",
	})
	if err != nil {
		t.Fatal(err)
	}
	revised, err := assessmentService.Revise(context.Background(), 7, 22, reviewed.Assessment.ID, models.AssessmentRevisionRequest{
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
	if loaded.EffectiveState != models.ProtectionExemptionStateSuperseded || loaded.Current.AssessmentRevision != 1 {
		t.Fatalf("superseded exemption = %#v", loaded)
	}
	changes, err := enrollments.ListChanges(context.Background(), 7, "manager", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	preview := projectionRule(t, changes.Changes[len(changes.Changes)-1].Projection, managerPreviewAction)
	if preview.Decision.Effect != dataprotection.EffectMask {
		t.Fatalf("assessment revision fallback decision = %#v", preview.Decision)
	}
	reactivated, err := exemptions.Renew(context.Background(), 7, 42, created.ID, models.RenewProtectionExemptionRequest{
		Version: created.Version, ExpiresAt: now.Add(2 * time.Hour), Rationale: "新评估修订重新批准",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reactivated.EffectiveState != models.ProtectionExemptionStateActive || reactivated.Current.AssessmentRevision != revised.CurrentRevision {
		t.Fatalf("reactivated exemption = %#v, assessment = %#v", reactivated, revised)
	}
}

func projectionRule(t *testing.T, projection *dataprotection.Projection, action string) dataprotection.Rule {
	t.Helper()
	if projection == nil {
		t.Fatal("projection is nil")
	}
	for _, rule := range projection.Rules {
		if rule.Action == action {
			return rule
		}
	}
	t.Fatalf("projection action %s is missing", action)
	return dataprotection.Rule{}
}
