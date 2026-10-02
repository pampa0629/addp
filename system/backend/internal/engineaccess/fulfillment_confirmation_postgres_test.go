package engineaccess

import (
	"context"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Runs inside the existing owner PostgreSQL gate and its migrated fixture.
// These tests prove current IAM qualification, not trusted HTTP provenance or
// Catalog responsibility: the business verifier remains an explicit test seam.
func exerciseFulfillmentConfirmers(t *testing.T, db *gorm.DB, identity *iam.Repository,
	newRequest func() fulfillmentRequest, newUser func(*testing.T, time.Duration) (userProvenance, time.Time),
	seedDelegation func(*testing.T, int64, time.Time) *Delegation,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	base := newRequest()
	repo := NewRepository(db)
	roles := iam.NewTenantRoleService(identity, time.Now)
	verified := func(*Repository) error { return nil }
	clock := func(t *testing.T) time.Time {
		t.Helper()
		var now time.Time
		if err := db.WithContext(ctx).Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			t.Fatal(err)
		}
		return now
	}
	newConfirmation := func(t *testing.T, permissions []string, expires *time.Time) (userProvenance, int64, int64) {
		t.Helper()
		source, _ := newUser(t, time.Hour)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: base.TenantID,
			RoleKey: "custom.confirmation_" + uuid.NewString()[:8], Name: "Confirmation fixture",
			ScopeTypes: []string{"tenant"}, PermissionKeys: permissions, ActorPrincipalID: base.Operator.PrincipalID})
		if err != nil {
			t.Fatal(err)
		}
		assigned, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: base.TenantID,
			MembershipID: source.MembershipID, RoleIDs: []int64{role.ID}, ScopeType: "tenant", ValidUntil: expires,
			Reason: "Explicit fixture qualification", ActorPrincipalID: base.Operator.PrincipalID})
		if err != nil || len(assigned) != 1 {
			t.Fatalf("confirmation assignment=%+v err=%v", assigned, err)
		}
		current, err := identity.GetPrincipal(ctx, source.PrincipalID)
		if err != nil {
			t.Fatal(err)
		}
		source.AuthorizationVersion = current.AuthorizationVersion
		return source, role.ID, assigned[0].ID
	}
	settle := func(request fulfillmentRequest, confirmation userProvenance, accept bool, verify func(*Repository) error) (*fulfillmentOutcome, error) {
		var result *fulfillmentOutcome
		err := repo.transaction(ctx, func(tx *Repository) error {
			var err error
			result, err = tx.settleFulfillment(ctx, request, accept, confirmation, verify)
			return err
		})
		return result, err
	}
	reject := func(t *testing.T, request fulfillmentRequest, source userProvenance, verify func(*Repository) error) {
		t.Helper()
		if _, err := settle(request, source, true, verify); err == nil {
			t.Fatal("invalid confirmer produced a new receipt")
		}
		for _, table := range []string{"system.engine_access_fulfillment_outcomes", "system.audit_logs"} {
			field, value := "request_id", any(request.RequestID)
			if table == "system.audit_logs" {
				field, value = "entity_id", request.RequestID.String()
			}
			var count int64
			if err := db.Table(table).Where(field+" = ?", value).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("failed confirmation left %s rows=%d err=%v", table, count, err)
			}
		}
		if closed, err := settle(request, userProvenance{}, false, nil); err != nil || closed.Outcome != "closed" {
			t.Fatalf("invalid confirmer stranded pending-request close: %+v %v", closed, err)
		}
	}
	permissions := []string{"catalog.entry.read", "catalog.sharing_decision.create"}
	t.Run("lower confirmer account precedes operator during real role write", func(t *testing.T) {
		source, _, _ := newConfirmation(t, permissions, nil)
		operator, expires := newUser(t, time.Hour)
		seedDelegation(t, operator.MembershipID, expires.Add(-time.Second))
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: base.TenantID,
			RoleKey: "custom.confirmer_lock_order", Name: "Confirmer lock order", ScopeTypes: []string{"tenant"},
			PermissionKeys: []string{"catalog.entry.update"}, ActorPrincipalID: operator.PrincipalID})
		if err != nil {
			t.Fatal(err)
		}
		request := newRequest()
		request.Operator = operator
		assertIAMQualificationRace(t, db, base.TenantID, source, func(writeCtx context.Context) error {
			_, err := roles.CreateAssignments(writeCtx, iam.CreateTenantRoleAssignmentsInput{TenantID: base.TenantID,
				MembershipID: source.MembershipID, RoleIDs: []int64{role.ID}, ScopeType: "tenant",
				ActorPrincipalID: operator.PrincipalID, Reason: "Confirmer concurrency fixture"})
			return err
		}, func(readCtx context.Context, tx *gorm.DB) error {
			_, err := NewRepository(tx).settleFulfillment(readCtx, request, true, source, verified)
			return err
		})
	})
	t.Run("real confirmer revocation waits for acceptance commit", func(t *testing.T) {
		source, _, assignmentID := newConfirmation(t, permissions, nil)
		actor, _ := newUser(t, time.Hour)
		request := newRequest()
		assertQualificationMutationWaitsForAcceptance(t, db, request, source, func(writeCtx context.Context) error {
			_, err := roles.RevokeAssignment(writeCtx, iam.RevokeTenantRoleAssignmentInput{TenantID: base.TenantID,
				AssignmentID: assignmentID, ActorPrincipalID: actor.PrincipalID, Reason: "Revocation after acceptance"})
			return err
		})
		request.RequestID = uuid.New()
		reject(t, request, source, verified)
	})
	t.Run("unrelated role grant and revoke preserve the original business decision", func(t *testing.T) {
		source, _, _ := newConfirmation(t, permissions, nil)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: base.TenantID,
			RoleKey: "custom.confirmation_unrelated", Name: "Unrelated role", ScopeTypes: []string{"tenant"},
			PermissionKeys: []string{"catalog.entry.update"}, ActorPrincipalID: base.Operator.PrincipalID})
		if err != nil {
			t.Fatal(err)
		}
		assigned, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: base.TenantID,
			MembershipID: source.MembershipID, RoleIDs: []int64{role.ID}, ScopeType: "tenant",
			ActorPrincipalID: base.Operator.PrincipalID, Reason: "Unrelated fixture role"})
		if err != nil || len(assigned) != 1 {
			t.Fatalf("unrelated assignment=%+v err=%v", assigned, err)
		}
		for _, revoke := range []bool{false, true} {
			if revoke {
				if _, err := roles.RevokeAssignment(ctx, iam.RevokeTenantRoleAssignmentInput{TenantID: base.TenantID,
					AssignmentID: assigned[0].ID, ActorPrincipalID: base.Operator.PrincipalID, Reason: "Unrelated removal"}); err != nil {
					t.Fatal(err)
				}
			}
			current, err := identity.GetPrincipal(ctx, source.PrincipalID)
			if err != nil || current.AuthorizationVersion <= source.AuthorizationVersion {
				t.Fatalf("real role change did not advance authorization version: %+v %v", current, err)
			}
			if receipt, err := settle(newRequest(), source, true, verified); err != nil || receipt.Outcome != "accepted" {
				t.Fatalf("unrelated change invalidated original confirmation: %+v %v", receipt, err)
			}
		}
	})
	for _, permissionSet := range [][]string{{"catalog.entry.read"}, {"catalog.sharing_decision.create"}} {
		t.Run("missing required permission "+permissionSet[0], func(t *testing.T) {
			source, _, _ := newConfirmation(t, permissionSet, nil)
			reject(t, newRequest(), source, verified)
		})
	}
	t.Run("original membership is mandatory", func(t *testing.T) {
		source, _, _ := newConfirmation(t, permissions, nil)
		source.MembershipID = base.Operator.MembershipID
		reject(t, newRequest(), source, verified)
	})
	t.Run("missing owner confirmer cannot be inferred from operator", func(t *testing.T) {
		reject(t, newRequest(), userProvenance{}, verified)
	})
	for _, mutation := range []string{"revoke assignment", "remove confirmation from role", "suspend membership"} {
		t.Run(mutation+" rejects new acceptance but not historical recovery", func(t *testing.T) {
			source, roleID, assignmentID := newConfirmation(t, permissions, nil)
			request := newRequest()
			original, err := settle(request, source, true, verified)
			if err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "revoke assignment":
				_, err = roles.RevokeAssignment(ctx, iam.RevokeTenantRoleAssignmentInput{TenantID: base.TenantID,
					AssignmentID: assignmentID, ActorPrincipalID: base.Operator.PrincipalID, Reason: "Remove qualification"})
			case "remove confirmation from role":
				_, err = roles.UpdateRole(ctx, iam.UpdateTenantRoleInput{TenantID: base.TenantID, RoleID: roleID,
					Name: "Read only now", ScopeTypes: []string{"tenant"}, PermissionKeys: []string{"catalog.entry.read"},
					ActorPrincipalID: base.Operator.PrincipalID})
			case "suspend membership":
				principalType, contextType := iam.PrincipalTypeUser, iam.ContextTypeTenant
				_, err = iam.NewTenantMembershipService(identity, time.Now).SuspendMembership(ctx, iam.ChangeTenantMembershipInput{
					TenantID: base.TenantID, PrincipalID: source.PrincipalID, Reason: "Suspend fixture member",
					Audit: iam.AuditMetadata{PrincipalID: &base.Operator.PrincipalID, PrincipalType: &principalType,
						ContextType: &contextType, TenantID: &base.TenantID}})
			}
			if err != nil {
				t.Fatal(err)
			}
			// Recovery has no new business source: it returns the exact old outcome.
			recovered, err := settle(request, userProvenance{}, true, nil)
			if err != nil || !recovered.RecordedAt.Equal(original.RecordedAt) || !recovered.Deadline.Equal(*original.Deadline) {
				t.Fatalf("later confirmer invalidation rewrote or stranded history: %+v %v", recovered, err)
			}
			if _, err := repo.readFulfillment(ctx, request); err != nil {
				t.Fatalf("later invalidation blocked read-only reconciliation: %v", err)
			}
			request.RequestID = uuid.New()
			reject(t, request, source, verified)
		})
	}
	t.Run("role assignment expiry during verification is not frozen by locks", func(t *testing.T) {
		expires := clock(t).Add(500 * time.Millisecond)
		source, _, _ := newConfirmation(t, permissions, &expires)
		entered := false
		reject(t, newRequest(), source, func(tx *Repository) error {
			entered = true
			now, err := tx.wallClock(ctx)
			if err != nil {
				return err
			}
			time.Sleep(expires.Sub(now) + 50*time.Millisecond)
			// Database time, not host time, is authoritative for the assertion.
			if remaining := expires.Sub(now); remaining > time.Second {
				return errors.New("unexpected fixture time span")
			}
			return nil
		})
		if !entered {
			t.Fatal("fixture expired before reaching business verification")
		}
	})
	t.Run("same person still needs the operator's original authorization version", func(t *testing.T) {
		source, _, _ := newConfirmation(t, permissions, nil)
		seedDelegation(t, source.MembershipID, clock(t).Add(30*time.Minute))
		request := newRequest()
		request.Operator = source
		if err := db.Table("system.principals").Where("id = ?", source.PrincipalID).
			UpdateColumn("authorization_version", gorm.Expr("authorization_version + 1")).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := settle(request, source, true, verified); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("business audit policy weakened the operator version gate: %v", err)
		}
		current, err := identity.GetPrincipal(ctx, source.PrincipalID)
		if err != nil {
			t.Fatal(err)
		}
		request.Operator.AuthorizationVersion = current.AuthorizationVersion
		request.RequestID = uuid.New()
		if _, err := settle(request, source, true, verified); err != nil {
			t.Fatalf("fresh operator source could not consume qualified old confirmation: %v", err)
		}
	})
}
