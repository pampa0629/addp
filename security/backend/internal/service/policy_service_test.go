package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/repository"
	"gorm.io/gorm"
)

func TestProtectionPolicyTightensAndRevokeFallsBackToBaseline(t *testing.T) {
	db, enrollments, finding, _, _ := prepareReviewablePhoneFinding(t)
	assessments := NewAssessmentService(db, nil)
	reviewed, err := assessments.ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{
		Decision: models.FindingReviewDecisionConfirm, Rationale: "确认手机号字段",
	})
	if err != nil {
		t.Fatal(err)
	}
	policies := NewPolicyService(db)
	created, err := policies.Create(context.Background(), 7, 31, models.CreateProtectionPolicyRequest{
		AssessmentID: reviewed.Assessment.ID, ConsumerOwner: "manager", Action: managerPreviewAction,
		Effect: dataprotection.EffectSuppress, Rationale: "预览不返回手机号字段",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 || created.CurrentRevision != 1 || created.Current.Effect != dataprotection.EffectSuppress || created.State != models.ProtectionPolicyStateActive || len(created.History) != 1 {
		t.Fatalf("created policy = %#v", created)
	}
	listed, err := policies.List(context.Background(), 7, 1, 100, finding.EnrollmentID)
	if err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 || len(listed.Data) != 1 || listed.Data[0].ID != created.ID {
		t.Fatalf("filtered policies = %#v", listed)
	}
	if _, err := policies.List(context.Background(), 7, 1, 100, "not-a-uuid"); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("invalid enrollment filter error = %v", err)
	}
	if _, err := policies.Create(context.Background(), 7, 31, models.CreateProtectionPolicyRequest{
		AssessmentID: reviewed.Assessment.ID, ConsumerOwner: "manager", Action: managerPreviewAction,
		Effect: dataprotection.EffectDeny, Rationale: "重复绑定",
	}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("duplicate create error = %v", err)
	}

	updated, err := policies.Update(context.Background(), 7, 32, created.ID, models.UpdateProtectionPolicyRequest{
		Version: created.Version, Effect: dataprotection.EffectDeny, Rationale: "预览整体拒绝",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 || updated.CurrentRevision != 2 || updated.Current.Effect != dataprotection.EffectDeny || len(updated.History) != 2 {
		t.Fatalf("updated policy = %#v", updated)
	}
	if _, err := policies.Update(context.Background(), 7, 32, created.ID, models.UpdateProtectionPolicyRequest{
		Version: 1, Effect: dataprotection.EffectSuppress, Rationale: "旧版本",
	}); !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("stale update error = %v", err)
	}

	revoked, err := policies.Revoke(context.Background(), 7, 33, created.ID, models.RevokeProtectionPolicyRequest{
		Version: updated.Version, Rationale: "撤销显式收紧，回落到基线",
	})
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Version != 3 || revoked.State != models.ProtectionPolicyStateRevoked || revoked.Current.State != models.ProtectionPolicyStateRevoked || len(revoked.History) != 3 {
		t.Fatalf("revoked policy = %#v", revoked)
	}
	if _, err := policies.Revoke(context.Background(), 7, 33, created.ID, models.RevokeProtectionPolicyRequest{Version: revoked.Version, Rationale: "重复撤销"}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("duplicate revoke error = %v", err)
	}

	changes, err := enrollments.ListChanges(context.Background(), 7, "manager", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Changes) != 6 {
		t.Fatalf("manager change count = %d, want 6", len(changes.Changes))
	}
	wantEffects := []string{dataprotection.EffectSuppress, dataprotection.EffectDeny, dataprotection.EffectMask}
	for index, want := range wantEffects {
		projection := changes.Changes[index+3].Projection
		requireManagerProjectionEffects(t, projection, want)
	}
}

func TestManagerPolicyDoesNotTightenDevelopOrServiceProjection(t *testing.T) {
	db, enrollments, finding, _, _ := prepareReviewablePhoneFinding(t)
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{
		Decision: models.FindingReviewDecisionConfirm, Rationale: "确认手机号字段",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPolicyService(db).Create(context.Background(), 7, 31, models.CreateProtectionPolicyRequest{
		AssessmentID: reviewed.Assessment.ID, ConsumerOwner: "manager", Action: managerPreviewAction,
		Effect: dataprotection.EffectSuppress, Rationale: "仅收紧 Manager 预览",
	}); err != nil {
		t.Fatal(err)
	}
	var enrollment models.ProtectionEnrollment
	if err := db.Where("tenant_id = ? AND id = ?", 7, finding.EnrollmentID).First(&enrollment).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return compileProtectionProjections(tx, enrollment, enrollment.LatestSourceSnapshotHash, time.Now().UTC(), allRequiredProtectionOwners())
	}); err != nil {
		t.Fatal(err)
	}
	managerChanges, err := enrollments.ListChanges(context.Background(), 7, "manager", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	requireManagerProjectionEffects(t, managerChanges.Changes[len(managerChanges.Changes)-1].Projection, dataprotection.EffectSuppress)
	developChanges, err := enrollments.ListChanges(context.Background(), 7, "develop", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	requireDevelopQueryProjection(t, developChanges.Changes[len(developChanges.Changes)-1].Projection, dataprotection.EffectMask)
	serviceChanges, err := enrollments.ListChanges(context.Background(), 7, "service", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	requireServiceExecuteProjection(t, serviceChanges.Changes[len(serviceChanges.Changes)-1].Projection, dataprotection.EffectMask)
	transferChanges, err := enrollments.ListChanges(context.Background(), 7, "transfer", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	requireTransferExportProjection(t, transferChanges.Changes[len(transferChanges.Changes)-1].Projection, dataprotection.EffectMask)
}

func TestFieldPoliciesRemainIndependentAcrossOwners(t *testing.T) {
	db, enrollments, finding, _, _ := prepareReviewablePhoneFinding(t)
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认字段"})
	if err != nil {
		t.Fatal(err)
	}
	service := NewPolicyService(db)
	scopes := supportedPolicyScopes()
	policies := make(map[string]*models.ProtectionPolicyResponse)
	latest := func(owner string) dataprotection.Projection {
		t.Helper()
		changes, err := enrollments.ListChanges(context.Background(), 7, owner, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		return *changes.Changes[len(changes.Changes)-1].Projection
	}
	for i, scope := range scopes {
		before := make(map[string]string)
		for _, other := range scopes {
			before[other.ConsumerOwner] = latest(other.ConsumerOwner).Revision
		}
		policy, err := service.Create(context.Background(), 7, 31, models.CreateProtectionPolicyRequest{AssessmentID: reviewed.Assessment.ID, ConsumerOwner: scope.ConsumerOwner, Action: scope.Action, Effect: "mask", Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, Parameters: map[string]any{"prefix_runes": i, "suffix_runes": 0, "mask_rune": "*"}, Rationale: "出口独立参数"})
		if err != nil {
			t.Fatal(err)
		}
		policies[scope.ConsumerOwner] = policy
		for _, other := range scopes {
			p := latest(other.ConsumerOwner)
			if (p.Revision != before[other.ConsumerOwner]) != (other.ConsumerOwner == scope.ConsumerOwner) {
				t.Fatalf("%s policy changed %s revision", scope.ConsumerOwner, other.ConsumerOwner)
			}
		}
	}
	listed, err := service.List(context.Background(), 7, 1, 100, finding.EnrollmentID)
	if err != nil || listed.Total != 4 || len(listed.SupportedScopes) != 4 {
		t.Fatalf("policy scope list: %#v %v", listed, err)
	}
	develop := policies[developProtectionOwner]
	updated, err := service.Update(context.Background(), 7, 31, develop.ID, models.UpdateProtectionPolicyRequest{Version: develop.Version, Effect: "deny", Rationale: "仅阻止查询"})
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range scopes {
		p := latest(scope.ConsumerOwner)
		for _, rule := range p.Rules {
			if rule.Action != scope.Action {
				continue
			}
			want := "mask"
			if scope.ConsumerOwner == developProtectionOwner {
				want = "deny"
			}
			if rule.Decision.Effect != want {
				t.Fatalf("%s effect=%s want %s", scope.ConsumerOwner, rule.Decision.Effect, want)
			}
		}
	}
	if _, err := service.Update(context.Background(), 7, 31, develop.ID, models.UpdateProtectionPolicyRequest{Version: develop.Version, Effect: "suppress", Rationale: "旧版本"}); !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("stale cross-outlet update=%v", err)
	}
	if _, err := service.Revoke(context.Background(), 7, 31, develop.ID, models.RevokeProtectionPolicyRequest{Version: updated.Version, Rationale: "仅查询恢复默认"}); err != nil {
		t.Fatal(err)
	}
	for i, scope := range scopes {
		p := latest(scope.ConsumerOwner)
		document := map[string]any{"userInfo": map[string]any{"phone": "13812345678"}}
		if err := dataprotection.ProtectDocument(document, scope.Action, p.Rules, dataprotection.SubjectReference{}); err != nil {
			t.Fatal(err)
		}
		want := "13812345678"[:i] + strings.Repeat("*", 11-i)
		if scope.ConsumerOwner == developProtectionOwner {
			want = "138****5678"
		}
		if got := document["userInfo"].(map[string]any)["phone"]; got != want {
			t.Fatalf("%s value=%v want %s", scope.ConsumerOwner, got, want)
		}
	}
}

func TestFieldPolicyRejectsUnsupportedOrMismatchedScope(t *testing.T) {
	db, _, finding, _, _ := prepareReviewablePhoneFinding(t)
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(context.Background(), 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认字段"})
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []models.ProtectionPolicyScope{{ConsumerOwner: "manager", Action: "profile"}, {ConsumerOwner: "develop", Action: "preview"}, {ConsumerOwner: "service", Action: "export"}, {ConsumerOwner: "transfer", Action: "query"}, {ConsumerOwner: "graph", Action: "graph_read"}, {ConsumerOwner: "workbench", Action: "service_execute"}} {
		if _, err := NewPolicyService(db).Create(context.Background(), 7, 31, models.CreateProtectionPolicyRequest{AssessmentID: reviewed.Assessment.ID, ConsumerOwner: scope.ConsumerOwner, Action: scope.Action, Effect: "deny", Rationale: "无效出口"}); !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("scope %#v accepted: %v", scope, err)
		}
	}
}
