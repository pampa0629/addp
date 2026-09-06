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

func TestProtectionAccessTargetsExposeAutomaticallyProtectedFieldsAsReviewRequired(t *testing.T) {
	db, enrollments, finding, _, _ := prepareReviewablePhoneFinding(t)
	enrollment, err := enrollments.Get(context.Background(), 7, finding.EnrollmentID)
	if err != nil {
		t.Fatal(err)
	}
	requests := NewAccessRequestService(db)
	targets, err := requests.Targets(context.Background(), 7, 41, enrollment.Target.ResourceIdentity, managerProtectionOwner, managerPreviewAction)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets.Data) != 1 || targets.Data[0].Component.Key != finding.ComponentKey || targets.Data[0].Requestable || targets.Data[0].UnavailableReason != "formal_assessment_required" {
		t.Fatalf("automatic access targets = %#v", targets.Data)
	}
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认手机号字段"})
	if err != nil {
		t.Fatal(err)
	}
	targets, err = requests.Targets(context.Background(), 7, 41, enrollment.Target.ResourceIdentity, managerProtectionOwner, managerPreviewAction)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets.Data) != 1 || !targets.Data[0].Requestable || targets.Data[0].AssessmentID != reviewed.Assessment.ID || targets.Data[0].UnavailableReason != "" {
		t.Fatalf("formal access targets = %#v", targets.Data)
	}
}

func TestProtectionAccessRequestApprovalPublishesSubjectScopedAuthorization(t *testing.T) {
	db, enrollments, finding, _, _ := prepareReviewablePhoneFinding(t)
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认手机号字段"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPolicyService(db).Create(context.Background(), 7, 31, models.CreateProtectionPolicyRequest{
		AssessmentID: reviewed.Assessment.ID, ConsumerOwner: managerProtectionOwner, Action: managerPreviewAction,
		Effect: dataprotection.EffectSuppress, Rationale: "默认移除手机号字段",
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	requests := NewAccessRequestService(db)
	requests.now = func() time.Time { return now }
	created, err := requests.Create(context.Background(), 7, 41, models.CreateProtectionAccessRequest{
		AssessmentID: reviewed.Assessment.ID, ConsumerOwner: managerProtectionOwner, Action: managerPreviewAction,
		RequestedExpiresAt: now.Add(24 * time.Hour), Rationale: "工单 SEC-2026-001 需要核验客户联系方式",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.State != models.ProtectionAccessRequestStatePending || created.SubjectID != "41" {
		t.Fatalf("created request = %#v", created)
	}
	selfQueue, err := requests.ListReviewQueue(context.Background(), 7, 41, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if selfQueue.Total != 0 || len(selfQueue.Data) != 0 {
		t.Fatalf("requester review queue = %#v", selfQueue)
	}
	reviewerQueue, err := requests.ListReviewQueue(context.Background(), 7, 42, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if reviewerQueue.Total != 1 || len(reviewerQueue.Data) != 1 || reviewerQueue.Data[0].ID != created.ID {
		t.Fatalf("reviewer queue = %#v", reviewerQueue)
	}
	if _, err := requests.Decide(context.Background(), 7, 41, created.ID, models.DecideProtectionAccessRequest{
		Version: created.Version, Decision: "approve", ExpiresAt: now.Add(time.Hour), Rationale: "本人审批",
	}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("self approval error = %v", err)
	}
	approved, err := requests.Decide(context.Background(), 7, 42, created.ID, models.DecideProtectionAccessRequest{
		Version: created.Version, Decision: "approve", ExpiresAt: now.Add(time.Hour), Rationale: "复核通过",
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved.State != models.ProtectionAccessRequestStateApproved || approved.ExemptionID == "" {
		t.Fatalf("approved request = %#v", approved)
	}
	changes, err := enrollments.ListChanges(context.Background(), 7, managerProtectionOwner, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	preview := projectionRule(t, changes.Changes[len(changes.Changes)-1].Projection, managerPreviewAction)
	if preview.Decision.Effect != dataprotection.EffectSuppress || len(preview.Authorizations) != 1 || preview.Authorizations[0].Subject.ID != "41" {
		t.Fatalf("subject-scoped projection rule = %#v", preview)
	}
	document := map[string]any{"userInfo": map[string]any{"phone": "13661384499"}}
	if err := dataprotection.ProtectDocument(document, managerPreviewAction, []dataprotection.Rule{preview}, dataprotection.SubjectReference{Type: "user", ID: "41"}); err != nil {
		t.Fatal(err)
	}
	if got := document["userInfo"].(map[string]any)["phone"]; got != "13661384499" {
		t.Fatalf("approved subject phone = %#v", got)
	}
	other := map[string]any{"userInfo": map[string]any{"phone": "13661384499"}}
	if err := dataprotection.ProtectDocument(other, managerPreviewAction, []dataprotection.Rule{preview}, dataprotection.SubjectReference{Type: "user", ID: "43"}); err != nil {
		t.Fatal(err)
	}
	if _, exists := other["userInfo"].(map[string]any)["phone"]; exists {
		t.Fatal("authorization leaked to another user")
	}
}

func TestProtectionAccessRequestRejectsUnsupportedOutletAndDuplicatePending(t *testing.T) {
	db, _, finding, _, _ := prepareReviewablePhoneFinding(t)
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认手机号字段"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	requests := NewAccessRequestService(db)
	requests.now = func() time.Time { return now }
	request := models.CreateProtectionAccessRequest{AssessmentID: reviewed.Assessment.ID, ConsumerOwner: managerProtectionOwner, Action: managerPreviewAction, RequestedExpiresAt: now.Add(time.Hour), Rationale: "核验"}
	if _, err := requests.Create(context.Background(), 7, 41, request); err != nil {
		t.Fatal(err)
	}
	if _, err := requests.Create(context.Background(), 7, 41, request); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("duplicate pending error = %v", err)
	}
	request.ConsumerOwner, request.Action = transferProtectionOwner, transferExportAction
	if _, err := requests.Create(context.Background(), 7, 41, request); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("unsupported outlet error = %v", err)
	}
}

func TestProtectionAccessRequestApprovalCannotExtendRequestedDeadlineAndRejectDoesNotGrant(t *testing.T) {
	db, enrollments, finding, _, _ := prepareReviewablePhoneFinding(t)
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认手机号字段"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	requests := NewAccessRequestService(db)
	requests.now = func() time.Time { return now }
	created, err := requests.Create(context.Background(), 7, 41, models.CreateProtectionAccessRequest{
		AssessmentID: reviewed.Assessment.ID, ConsumerOwner: managerProtectionOwner, Action: managerPreviewAction,
		RequestedExpiresAt: now.Add(time.Hour), Rationale: "临时核验客户联系方式",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := requests.Decide(context.Background(), 7, 42, created.ID, models.DecideProtectionAccessRequest{
		Version: created.Version, Decision: "approve", ExpiresAt: now.Add(2 * time.Hour), Rationale: "超出申请期限",
	}); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("extended approval deadline error = %v", err)
	}
	rejected, err := requests.Decide(context.Background(), 7, 42, created.ID, models.DecideProtectionAccessRequest{
		Version: created.Version, Decision: "reject", Rationale: "业务依据不足",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rejected.State != models.ProtectionAccessRequestStateRejected || rejected.ExemptionID != "" {
		t.Fatalf("rejected request = %#v", rejected)
	}
	changes, err := enrollments.ListChanges(context.Background(), 7, managerProtectionOwner, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	preview := projectionRule(t, changes.Changes[len(changes.Changes)-1].Projection, managerPreviewAction)
	if len(preview.Authorizations) != 0 {
		t.Fatalf("rejected request published authorization = %#v", preview.Authorizations)
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
