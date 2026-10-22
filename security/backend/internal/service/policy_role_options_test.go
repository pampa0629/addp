package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/repository"
	"gorm.io/gorm"
)

func TestRolePolicyOptionsRevisionAndFourOutletCompilation(t *testing.T) {
	verifyRolePolicyOptions(t, openSecurityTestDB(t))
}

func verifyRolePolicyOptions(t *testing.T, db *gorm.DB) {
	t.Helper()
	db, enrollments, finding, kind, grade := prepareReviewablePhoneFindingOnDB(t, db)
	ctx := context.Background()
	definitions := newTestDefinitionService(db)
	baselines, err := definitions.ListBaselines(7)
	if err != nil {
		t.Fatal(err)
	}
	baseline := baselines[0]
	request := models.ProtectionBaselineRequest{SensitiveDataTypeID: kind.ID, SecurityGradeID: grade.ID, Effect: baseline.Effect, Algorithm: baseline.Algorithm, Parameters: baseline.Parameters, AllowedAlgorithms: []string{baseline.Algorithm, dataprotection.AlgorithmSM3V1}, InvalidValueEffect: baseline.InvalidValueEffect, Version: baseline.Version}
	updated, err := definitions.UpdateBaseline(baseline.ID, 7, 11, request)
	if err != nil {
		t.Fatal(err)
	}
	baseline = *updated
	reviewed, err := NewAssessmentService(db, nil).ReviewFinding(ctx, 7, 21, finding.ID, models.FindingReviewRequest{Decision: models.FindingReviewDecisionConfirm, Rationale: "确认字段"})
	if err != nil {
		t.Fatal(err)
	}
	policies := NewPolicyService(db)
	option := dataprotection.RoleMaskingOption{Name: "关联分析", RoleKeys: []string{"tenant.reader", "tenant.analyst", "tenant.reader"}, Decision: dataprotection.Decision{Effect: "mask", Algorithm: dataprotection.AlgorithmSM3V1, InvalidValueEffect: baseline.InvalidValueEffect}}
	created := make([]*models.ProtectionPolicyResponse, 0, 4)
	for _, scope := range supportedPolicyScopes() {
		create := models.CreateProtectionPolicyRequest{AssessmentID: reviewed.Assessment.ID, ConsumerOwner: scope.ConsumerOwner, Action: scope.Action, Effect: baseline.Effect, Algorithm: baseline.Algorithm, Parameters: baseline.Parameters, InvalidValueEffect: baseline.InvalidValueEffect, Rationale: "提供业务选择", RoleOptions: []dataprotection.RoleMaskingOption{option}}
		invalid := create
		invalid.Effect = "deny"
		invalid.Algorithm, invalid.Parameters, invalid.InvalidValueEffect = "", nil, "deny"
		if _, err := policies.Create(ctx, 7, 31, invalid); !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("options bypass deny: %v", err)
		}
		policy, err := policies.Create(ctx, 7, 31, create)
		if err != nil {
			t.Fatal(err)
		}
		if len(policy.Current.RoleOptions) != 1 || policy.Current.RoleOptions[0].ID == "" || !reflect.DeepEqual(policy.Current.RoleOptions[0].RoleKeys, []string{"tenant.analyst", "tenant.reader"}) {
			t.Fatalf("options not normalized: %+v", policy.Current.RoleOptions)
		}
		created = append(created, policy)
		changes, err := enrollments.ListChanges(ctx, 7, scope.ConsumerOwner, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		projection := changes.Changes[len(changes.Changes)-1].Projection
		if err := projection.Validate(time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		for _, rule := range projection.Rules {
			if rule.Action != scope.Action {
				if rule.RoleOptions != nil {
					t.Fatal("option leaked into another action")
				}
				continue
			}
			if rule.RoleOptions == nil || rule.RoleOptions.PolicyID != policy.ID || rule.RoleOptions.PolicyRevision != 1 || rule.Decision.Algorithm != baseline.Algorithm {
				t.Fatalf("incorrect compiled rule: %+v", rule)
			}
			choice := dataprotection.RoleOptionSelection{PolicyID: policy.ID, AssessmentRevision: 1, PolicyRevision: 1, OptionID: policy.Current.RoleOptions[0].ID}
			if decision, err := rule.ResolveRoleOption(choice, []string{"tenant.reader"}); err != nil || decision.Algorithm != dataprotection.AlgorithmSM3V1 {
				t.Fatalf("option resolve: %+v %v", decision, err)
			}
		}
		if _, err := policies.Get(ctx, 8, policy.ID); !errors.Is(err, commonapi.ErrNotFound) {
			t.Fatal("cross tenant policy access")
		}
		update := models.UpdateProtectionPolicyRequest{Version: 1, Effect: baseline.Effect, Algorithm: baseline.Algorithm, Parameters: baseline.Parameters, InvalidValueEffect: baseline.InvalidValueEffect, Rationale: "修改选项", RoleOptions: policy.Current.RoleOptions}
		second, err := policies.Update(ctx, 7, 31, policy.ID, update)
		if err != nil {
			t.Fatal(err)
		}
		if second.Current.RoleOptions[0].ID != policy.Current.RoleOptions[0].ID || len(second.History) != 2 {
			t.Fatal("option identity/history changed")
		}
		if _, err := policies.Update(ctx, 7, 31, policy.ID, update); !errors.Is(err, repository.ErrVersionConflict) {
			t.Fatal("stale policy version accepted")
		}
		created[len(created)-1] = second
	}
	request.Version = baseline.Version
	request.AllowedAlgorithms = []string{baseline.Algorithm}
	if _, err := definitions.UpdateBaseline(baseline.ID, 7, 11, request); err != nil {
		t.Fatal(err)
	}
	for _, policy := range created {
		changes, err := enrollments.ListChanges(ctx, 7, policy.ConsumerOwner, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, rule := range changes.Changes[len(changes.Changes)-1].Projection.Rules {
			if rule.Action == policy.Action && (rule.RoleOptions != nil || rule.Decision.Algorithm != baseline.Algorithm) {
				t.Fatal("withdrawn option published or default changed")
			}
		}
		revoked, err := policies.Revoke(ctx, 7, 31, policy.ID, models.RevokeProtectionPolicyRequest{Version: policy.Version, Rationale: "恢复默认"})
		if err != nil {
			t.Fatal(err)
		}
		if len(revoked.Current.RoleOptions) != 1 {
			t.Fatal("revocation erased audit")
		}
	}
}
