package service

import (
	"context"
	"errors"
	"strconv"
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
	requests := NewAccessRequestService(db, testAccessActorResolver)
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
	requests := NewAccessRequestService(db, testAccessActorResolver)
	requests.now = func() time.Time { return now }
	if _, err := requests.ListReviewQueue(context.Background(), 7, 42, models.ProtectionAccessRequestReviewFilter{}, 1, 20); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("missing review scope error = %v", err)
	}
	created, err := requests.Create(context.Background(), 7, 41, models.CreateProtectionAccessRequest{
		AssessmentID: reviewed.Assessment.ID, ConsumerOwner: managerProtectionOwner, Action: managerPreviewAction,
		RequestedExpiresAt: now.Add(24 * time.Hour), Rationale: "工单 SEC-2026-001 需要核验客户联系方式",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.State != models.ProtectionAccessRequestStatePending || created.Requester.ID != "41" || created.Requester.DisplayName != "用户 41" {
		t.Fatalf("created request = %#v", created)
	}
	selfQueue, err := requests.ListReviewQueue(context.Background(), 7, 41, reviewAccessRequestFilter(models.ProtectionAccessRequestReviewScopePending), 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if selfQueue.Total != 1 || len(selfQueue.Data) != 1 || selfQueue.Data[0].ID != created.ID || selfQueue.Data[0].CanDecide || selfQueue.Data[0].DecisionUnavailableReason != "self_approval_forbidden" {
		t.Fatalf("requester review queue = %#v", selfQueue)
	}
	requests.now = func() time.Time { return now.Add(25 * time.Hour) }
	expiredQueue, err := requests.ListReviewQueue(context.Background(), 7, 42, reviewAccessRequestFilter(models.ProtectionAccessRequestReviewScopePending), 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if expiredQueue.Total != 0 || len(expiredQueue.Data) != 0 {
		t.Fatalf("expired review queue = %#v", expiredQueue)
	}
	expiredHistory, err := requests.ListReviewQueue(context.Background(), 7, 42, reviewAccessRequestFilter(models.ProtectionAccessRequestReviewScopeHistory), 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if expiredHistory.Total != 1 || len(expiredHistory.Data) != 1 || expiredHistory.Data[0].State != models.ProtectionAccessRequestStateExpired || expiredHistory.Data[0].CanDecide {
		t.Fatalf("expired review history = %#v", expiredHistory)
	}
	if _, err := requests.Decide(context.Background(), 7, 42, created.ID, models.DecideProtectionAccessRequest{
		Version: created.Version, Decision: "approve", ExpiresAt: created.RequestedExpiresAt, Rationale: "过期后审批",
	}); !errors.Is(err, ErrProtectionAccessRequestExpired) {
		t.Fatalf("expired approval error = %v", err)
	}
	requests.now = func() time.Time { return now }
	reviewerQueue, err := requests.ListReviewQueue(context.Background(), 7, 42, reviewAccessRequestFilter(models.ProtectionAccessRequestReviewScopePending), 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if reviewerQueue.Total != 1 || len(reviewerQueue.Data) != 1 || reviewerQueue.Data[0].ID != created.ID || !reviewerQueue.Data[0].CanDecide || reviewerQueue.Data[0].DecisionUnavailableReason != "" {
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
	if approved.State != models.ProtectionAccessRequestStateApproved || approved.EnrollmentID != reviewed.Assessment.EnrollmentID || approved.ExemptionID == "" || approved.AuthorizationState != models.ProtectionExemptionStateActive || approved.AuthorizedUntil == nil || !approved.AuthorizedUntil.Equal(now.Add(time.Hour)) {
		t.Fatalf("approved request = %#v", approved)
	}
	pendingAfterApproval, err := requests.ListReviewQueue(context.Background(), 7, 42, reviewAccessRequestFilter(models.ProtectionAccessRequestReviewScopePending), 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if pendingAfterApproval.Total != 0 || len(pendingAfterApproval.Data) != 0 {
		t.Fatalf("pending queue after approval = %#v", pendingAfterApproval)
	}
	approvalHistory, err := requests.ListReviewQueue(context.Background(), 7, 42, reviewAccessRequestFilter(models.ProtectionAccessRequestReviewScopeHistory), 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if approvalHistory.Total != 1 || len(approvalHistory.Data) != 1 || approvalHistory.Data[0].State != models.ProtectionAccessRequestStateApproved || approvalHistory.Data[0].EnrollmentID != reviewed.Assessment.EnrollmentID || approvalHistory.Data[0].AuthorizationState != models.ProtectionExemptionStateActive || approvalHistory.Data[0].AuthorizedUntil == nil || !approvalHistory.Data[0].AuthorizedUntil.Equal(now.Add(time.Hour)) || approvalHistory.Data[0].Reviewer == nil || approvalHistory.Data[0].Reviewer.ID != "42" || approvalHistory.Data[0].Reviewer.DisplayName != "用户 42" || approvalHistory.Data[0].DecisionRationale != "复核通过" {
		t.Fatalf("approval review history = %#v", approvalHistory)
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

	exemptions := NewExemptionService(db)
	exemptions.now = func() time.Time { return now.Add(30 * time.Minute) }
	currentExemption, err := exemptions.Get(context.Background(), 7, approved.ExemptionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = exemptions.Revoke(context.Background(), 7, 42, approved.ExemptionID, models.RevokeProtectionExemptionRequest{
		Version: currentExemption.Version, Rationale: "提前撤销原值访问授权",
	}); err != nil {
		t.Fatal(err)
	}
	requests.now = func() time.Time { return now.Add(30 * time.Minute) }
	revokedHistory, err := requests.ListReviewQueue(context.Background(), 7, 42, reviewAccessRequestFilter(models.ProtectionAccessRequestReviewScopeHistory), 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if revokedHistory.Total != 1 || len(revokedHistory.Data) != 1 || revokedHistory.Data[0].AuthorizationState != models.ProtectionExemptionStateRevoked || revokedHistory.Data[0].AuthorizedUntil == nil || !revokedHistory.Data[0].AuthorizedUntil.Equal(now.Add(time.Hour)) {
		t.Fatalf("revoked authorization review history = %#v", revokedHistory)
	}
}

func TestEffectiveAccessRequestAuthorizationState(t *testing.T) {
	now := time.Date(2026, time.September, 8, 10, 0, 0, 0, time.UTC)
	baseExemption := models.ProtectionExemption{State: models.ProtectionExemptionStateActive}
	baseRevision := models.ProtectionExemptionRevision{
		SourceRequestID: "request-1", AssessmentRevision: 3,
		State: models.ProtectionExemptionStateActive, ExpiresAt: now.Add(time.Hour),
	}
	tests := []struct {
		name               string
		requestID          string
		exemption          models.ProtectionExemption
		current            models.ProtectionExemptionRevision
		assessmentRevision int64
		at                 time.Time
		want               string
	}{
		{name: "active", requestID: "request-1", exemption: baseExemption, current: baseRevision, assessmentRevision: 3, at: now, want: models.ProtectionExemptionStateActive},
		{name: "expired", requestID: "request-1", exemption: baseExemption, current: baseRevision, assessmentRevision: 3, at: now.Add(time.Hour), want: models.ProtectionExemptionStateExpired},
		{name: "revoked", requestID: "request-1", exemption: models.ProtectionExemption{State: models.ProtectionExemptionStateRevoked}, current: models.ProtectionExemptionRevision{SourceRequestID: "request-1", AssessmentRevision: 3, State: models.ProtectionExemptionStateRevoked, ExpiresAt: now.Add(time.Hour)}, assessmentRevision: 3, at: now, want: models.ProtectionExemptionStateRevoked},
		{name: "assessment superseded", requestID: "request-1", exemption: baseExemption, current: baseRevision, assessmentRevision: 4, at: now, want: models.ProtectionExemptionStateSuperseded},
		{name: "later grant superseded", requestID: "request-1", exemption: baseExemption, current: models.ProtectionExemptionRevision{SourceRequestID: "request-2", AssessmentRevision: 3, State: models.ProtectionExemptionStateActive, ExpiresAt: now.Add(time.Hour)}, assessmentRevision: 3, at: now, want: models.ProtectionExemptionStateSuperseded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := effectiveAccessRequestAuthorizationState(test.requestID, test.exemption, test.current, test.assessmentRevision, test.at); got != test.want {
				t.Fatalf("state = %q, want %q", got, test.want)
			}
		})
	}
}

func reviewAccessRequestFilter(scope string) models.ProtectionAccessRequestReviewFilter {
	return models.ProtectionAccessRequestReviewFilter{Scope: scope}
}

func TestProtectionAccessReviewQueueFiltersAndPaginatesOnServer(t *testing.T) {
	db, _, finding, _, _ := prepareReviewablePhoneFinding(t)
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认手机号字段"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.September, 8, 10, 0, 0, 0, time.UTC)
	requests := NewAccessRequestService(db, testAccessActorResolver)
	requests.now = func() time.Time { return now }

	create := func(userID int64, expiresAt time.Time, rationale string) *models.ProtectionAccessRequestResponse {
		t.Helper()
		created, createErr := requests.Create(context.Background(), 7, userID, models.CreateProtectionAccessRequest{
			AssessmentID: reviewed.Assessment.ID, ConsumerOwner: managerProtectionOwner, Action: managerPreviewAction,
			RequestedExpiresAt: expiresAt, Rationale: rationale,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return created
	}
	approvedRequest := create(41, now.Add(24*time.Hour), "批准申请")
	if _, err = requests.Decide(context.Background(), 7, 99, approvedRequest.ID, models.DecideProtectionAccessRequest{
		Version: approvedRequest.Version, Decision: "approve", ExpiresAt: now.Add(time.Hour), Rationale: "批准",
	}); err != nil {
		t.Fatal(err)
	}
	rejectedRequest := create(43, now.Add(24*time.Hour), "驳回申请")
	if _, err = requests.Decide(context.Background(), 7, 99, rejectedRequest.ID, models.DecideProtectionAccessRequest{
		Version: rejectedRequest.Version, Decision: "reject", Rationale: "驳回",
	}); err != nil {
		t.Fatal(err)
	}
	expiredRequest := create(44, now.Add(time.Hour), "过期申请")
	if err := db.Model(&models.ProtectionAccessRequest{}).Where("id = ?", approvedRequest.ID).Update("created_at", now.Add(-48*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	requests.now = func() time.Time { return now.Add(2 * time.Hour) }

	history := reviewAccessRequestFilter(models.ProtectionAccessRequestReviewScopeHistory)
	firstPage, err := requests.ListReviewQueue(context.Background(), 7, 99, history, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if firstPage.Total != 3 || firstPage.TotalPages != 3 || len(firstPage.Data) != 1 {
		t.Fatalf("paginated review history = %#v", firstPage)
	}

	history.State = models.ProtectionAccessRequestStateApproved
	approvedOnly, err := requests.ListReviewQueue(context.Background(), 7, 99, history, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if approvedOnly.Total != 1 || approvedOnly.Data[0].ID != approvedRequest.ID {
		t.Fatalf("approved filter = %#v", approvedOnly)
	}
	history.State = models.ProtectionAccessRequestStateExpired
	expiredOnly, err := requests.ListReviewQueue(context.Background(), 7, 99, history, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if expiredOnly.Total != 1 || expiredOnly.Data[0].ID != expiredRequest.ID || expiredOnly.Data[0].State != models.ProtectionAccessRequestStateExpired {
		t.Fatalf("expired filter = %#v", expiredOnly)
	}

	history = reviewAccessRequestFilter(models.ProtectionAccessRequestReviewScopeHistory)
	history.RequesterSearch = "用户 43"
	requesterOnly, err := requests.ListReviewQueue(context.Background(), 7, 99, history, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if requesterOnly.Total != 1 || requesterOnly.Data[0].ID != rejectedRequest.ID {
		t.Fatalf("requester filter = %#v", requesterOnly)
	}
	history.RequesterSearch = ""
	history.ResourceSearch = "userinfo.phone"
	resourceOnly, err := requests.ListReviewQueue(context.Background(), 7, 99, history, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if resourceOnly.Total != 3 {
		t.Fatalf("resource filter = %#v", resourceOnly)
	}

	from, to := now.Add(-time.Hour), now.Add(3*time.Hour)
	history.ResourceSearch = ""
	history.CreatedFrom, history.CreatedTo = &from, &to
	recentOnly, err := requests.ListReviewQueue(context.Background(), 7, 99, history, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if recentOnly.Total != 2 {
		t.Fatalf("created-at filter = %#v", recentOnly)
	}

	invalidState := reviewAccessRequestFilter(models.ProtectionAccessRequestReviewScopePending)
	invalidState.State = models.ProtectionAccessRequestStateApproved
	if _, err := requests.ListReviewQueue(context.Background(), 7, 99, invalidState, 1, 20); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("pending state filter error = %v", err)
	}
	invalidRange := reviewAccessRequestFilter(models.ProtectionAccessRequestReviewScopeHistory)
	invalidRange.CreatedFrom, invalidRange.CreatedTo = &to, &from
	if _, err := requests.ListReviewQueue(context.Background(), 7, 99, invalidRange, 1, 20); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("invalid time range error = %v", err)
	}
}

func testAccessActorResolver(_ context.Context, tenantID int64, ids []int64) (map[int64]string, error) {
	if tenantID <= 0 {
		return nil, errors.New("invalid tenant")
	}
	result := make(map[int64]string, len(ids))
	for _, id := range ids {
		result[id] = "用户 " + strconv.FormatInt(id, 10)
	}
	return result, nil
}

func TestProtectionAccessRequestBackfillsImmutableActorSnapshots(t *testing.T) {
	db := openSecurityTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	reviewerID := int64(42)
	row := models.ProtectionAccessRequest{
		ID: "15a0191b-4773-4217-837c-44bfaf148bad", TenantID: 7,
		AssessmentID: "92826a2d-5bb4-48c6-a632-09683cb247ca", AssessmentRevision: 1,
		ConsumerOwner: managerProtectionOwner, Action: managerPreviewAction,
		SubjectType: "user", SubjectID: "41", RequestedExpiresAt: now.Add(time.Hour),
		Rationale: "历史申请", State: models.ProtectionAccessRequestStateApproved, Version: 2,
		DecidedBy: &reviewerID, DecidedAt: &now, DecisionRationale: "复核通过",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	requests := NewAccessRequestService(db, testAccessActorResolver)
	if err := requests.BackfillActorSnapshots(context.Background()); err != nil {
		t.Fatal(err)
	}
	var stored models.ProtectionAccessRequest
	if err := db.First(&stored, "id = ?", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SubjectDisplayName != "用户 41" || stored.DecidedByDisplayName != "用户 42" {
		t.Fatalf("actor snapshots = %#v", stored)
	}
}

func TestExpiredProtectionAccessRequestStopsPollingAndCanBeResubmitted(t *testing.T) {
	db, enrollments, finding, _, _ := prepareReviewablePhoneFinding(t)
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认手机号字段"})
	if err != nil {
		t.Fatal(err)
	}
	enrollment, err := enrollments.Get(context.Background(), 7, finding.EnrollmentID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	requests := NewAccessRequestService(db, testAccessActorResolver)
	requests.now = func() time.Time { return now }
	first, err := requests.Create(context.Background(), 7, 41, models.CreateProtectionAccessRequest{
		AssessmentID: reviewed.Assessment.ID, ConsumerOwner: managerProtectionOwner, Action: managerPreviewAction,
		RequestedExpiresAt: now.Add(time.Hour), Rationale: "首次申请",
	})
	if err != nil {
		t.Fatal(err)
	}

	requests.now = func() time.Time { return now.Add(2 * time.Hour) }
	targets, err := requests.Targets(context.Background(), 7, 41, enrollment.Target.ResourceIdentity, managerProtectionOwner, managerPreviewAction)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets.Data) != 1 || targets.Data[0].AccessRequest == nil || targets.Data[0].AccessRequest.ID != first.ID || targets.Data[0].AccessRequest.State != models.ProtectionAccessRequestStateExpired {
		t.Fatalf("expired access target = %#v", targets.Data)
	}
	second, err := requests.Create(context.Background(), 7, 41, models.CreateProtectionAccessRequest{
		AssessmentID: reviewed.Assessment.ID, ConsumerOwner: managerProtectionOwner, Action: managerPreviewAction,
		RequestedExpiresAt: now.Add(3 * time.Hour), Rationale: "过期后重新申请",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || second.State != models.ProtectionAccessRequestStatePending {
		t.Fatalf("resubmitted request = %#v", second)
	}
	var expired models.ProtectionAccessRequest
	if err := db.Where("id = ?", first.ID).First(&expired).Error; err != nil {
		t.Fatal(err)
	}
	if expired.State != models.ProtectionAccessRequestStateExpired {
		t.Fatalf("persisted expired state = %q", expired.State)
	}
}

func TestProtectionAccessRequestRejectsUnsupportedOutletAndDuplicatePending(t *testing.T) {
	db, _, finding, _, _ := prepareReviewablePhoneFinding(t)
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认手机号字段"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	requests := NewAccessRequestService(db, testAccessActorResolver)
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
	requests := NewAccessRequestService(db, testAccessActorResolver)
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
