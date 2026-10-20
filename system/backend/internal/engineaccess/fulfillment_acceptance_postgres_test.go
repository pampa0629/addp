package engineaccess

import (
	"context"
	"errors"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type fulfillmentBasisFunc func(context.Context, uint, uuid.UUID, shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentBasis, error)

func (f fulfillmentBasisFunc) ReadFulfillmentBasis(c context.Context, tenant uint, id uuid.UUID, binding shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentBasis, error) {
	return f(c, tenant, id, binding)
}

func exerciseFulfillmentAcceptance(t *testing.T, db *gorm.DB, base fulfillmentRequest, confirmation userProvenance,
	newOperator func(*testing.T, time.Duration) (userProvenance, time.Time), seedDelegation func(*testing.T, int64, time.Time) *Delegation,
) {
	t.Run("production first acceptance and historical retry", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		hash, err := bcrypt.GenerateFromPassword([]byte(strings.Repeat("accept-fixture-", 3)), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.oauth_clients").Where("client_id='addp-catalog'").Updates(map[string]any{"status": "active", "client_secret_hash": string(hash)}).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Table("system.oauth_clients").Where("client_id='addp-catalog'").Update("status", "disabled")
		var principal iam.Principal
		var member iam.TenantMembership
		if err := db.First(&principal, base.CallerPrincipalID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("tenant_id=? AND principal_id=?", base.TenantID, principal.ID).Take(&member).Error; err != nil {
			t.Fatal(err)
		}
		actor := FulfillmentRuntimeActor{Actor: Actor{TenantID: base.TenantID, PrincipalID: principal.ID, MembershipID: member.ID, AuthorizationVersion: principal.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}, ClientID: "addp-catalog"}
		operator, memberExpiry := newOperator(t, time.Hour)
		seedDelegation(t, operator.MembershipID, memberExpiry.Add(-time.Second))
		roles := iam.NewTenantRoleService(iam.NewRepository(db), testDatabaseClock(t, db))
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: base.TenantID, RoleKey: "custom.first_acceptance", Name: "Explicit acceptance fixture", PermissionKeys: []string{"system.engine_access_fulfillment.create", "system.engine_access_grant.revoke"}, ActorPrincipalID: base.Operator.PrincipalID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: base.TenantID, MembershipID: operator.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: base.Operator.PrincipalID, Reason: "Explicit fixture handler"}); err != nil {
			t.Fatal(err)
		}
		current, err := iam.NewRepository(db).GetPrincipal(ctx, operator.PrincipalID)
		if err != nil {
			t.Fatal(err)
		}
		operator.AuthorizationVersion = current.AuthorizationVersion
		binding := shared.SharingFulfillmentBinding{ItemFingerprint: factstest.Binding(base.Path).ItemFingerprint, BoundaryHash: factstest.Binding(base.Path).BoundaryHash, CallerPrincipalID: base.CallerPrincipalID, Operator: shared.SharingFulfillmentOperator{PrincipalID: operator.PrincipalID, MembershipID: operator.MembershipID, AuthorizationVersion: operator.AuthorizationVersion},
			Path: base.Path, DecisionID: base.DecisionID, RequirementVersion: 1, RecipientType: base.RecipientType, RecipientID: base.RecipientID, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked}
		service := NewService(NewRepository(db), nil).WithAuthorizationFactsReader(testMetaFacts{paths: []engineplugin.EngineCatalogPath{base.Path}})
		if _, err := service.AcceptFulfillment(ctx, actor, uuid.New(), binding); !errors.Is(err, ErrFulfillmentCapability) {
			t.Fatalf("missing credential=%v", err)
		}
		calls := 0
		closeBeforeAccept := false
		forgeBasis := false
		service.WithFulfillmentBasisReader(fulfillmentBasisFunc(func(ctx context.Context, tenant uint, id uuid.UUID, actual shared.SharingFulfillmentBinding) (*shared.SharingFulfillmentBasis, error) {
			calls++
			// Independent writer can acquire the caller Principal during remote IO.
			if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				return tx.Exec("SELECT id FROM system.principals WHERE id=? FOR UPDATE NOWAIT", actor.PrincipalID).Error
			}); err != nil {
				return nil, err
			}
			if closeBeforeAccept {
				if _, err := service.CloseFulfillment(ctx, actor, id, actual); err != nil {
					return nil, err
				}
			}
			if forgeBasis {
				actual.Operator.PrincipalID++
			}
			return &shared.SharingFulfillmentBasis{RequestID: id, TenantID: int64(tenant), Binding: actual, Confirmation: shared.SharingFulfillmentOperator{PrincipalID: confirmation.PrincipalID, MembershipID: confirmation.MembershipID, AuthorizationVersion: confirmation.AuthorizationVersion}}, nil
		}))
		id := uuid.New()
		accepted, err := service.AcceptFulfillment(ctx, actor, id, binding)
		if err != nil || accepted.Outcome != "accepted" || accepted.Deadline == nil || accepted.Deadline.Sub(accepted.RecordedAt) != 5*time.Minute || calls != 1 {
			t.Fatalf("accepted=%+v calls=%d err=%v", accepted, calls, err)
		}
		exerciseFulfillmentGrants(t, db, service, actor, binding, roles, role.ID, base.Operator.PrincipalID, newOperator, seedDelegation)
		exerciseGrantRevocations(t, db, service, actor, binding, roles, role.ID, base.Operator.PrincipalID, newOperator, seedDelegation)
		exerciseSourceDenies(t, db, actor.TenantID, binding.Path, roles, base.Operator.PrincipalID, newOperator, seedDelegation)
		exerciseDenyReleases(t, db, actor.TenantID, binding.Path, roles, base.Operator.PrincipalID, newOperator, seedDelegation)
		exerciseInitialProductionSources(t, db, service, actor, binding, roles, role.ID, base.Operator.PrincipalID, newOperator, seedDelegation)
		exerciseInitialProductionGrants(t, db, service, actor, binding, roles, base.Operator.PrincipalID, newOperator)
		exerciseCurrentSourceRules(t, db, service, actor, binding, roles, role.ID, base.Operator.PrincipalID, newOperator, seedDelegation)
		exerciseIndependentGrantCommands(t, db, actor.TenantID, binding.Path, roles, base.Operator.PrincipalID, newOperator, seedDelegation)
		exerciseFileCompositionGrants(t, db, actor.TenantID, base.Operator.PrincipalID, roles, newOperator)
		// The following acceptance checks are a new Runtime request phase. Keep
		// their operation budget and one-minute credential independent of preceding serial exercises;
		// explicit expired-token cases continue to supply their own expired actor.
		cancel()
		ctx, cancel = context.WithTimeout(t.Context(), 90*time.Second)
		defer cancel()
		var runtimeNow time.Time
		if err := db.WithContext(ctx).Raw("SELECT clock_timestamp()").Scan(&runtimeNow).Error; err != nil {
			t.Fatal(err)
		}
		actor.TokenExpiresAt = runtimeNow.Add(time.Minute)
		for _, mutate := range []func(*shared.SharingFulfillmentBinding){
			func(b *shared.SharingFulfillmentBinding) { b.Operator.AuthorizationVersion++ },
			func(b *shared.SharingFulfillmentBinding) { b.RequirementVersion++ },
			func(b *shared.SharingFulfillmentBinding) { b.RecipientID = 9007199254740993 },
		} {
			bad := binding
			mutate(&bad)
			if _, err := service.AcceptFulfillment(ctx, actor, uuid.New(), bad); err == nil {
				t.Fatal("unqualified first acceptance succeeded")
			}
		}
		unprivileged, unprivilegedExpiry := newOperator(t, time.Hour)
		seedDelegation(t, unprivileged.MembershipID, unprivilegedExpiry.Add(-time.Second))
		noPermission := binding
		noPermission.Operator = shared.SharingFulfillmentOperator{PrincipalID: unprivileged.PrincipalID, MembershipID: unprivileged.MembershipID, AuthorizationVersion: unprivileged.AuthorizationVersion}
		if _, err := service.AcceptFulfillment(ctx, actor, uuid.New(), noPermission); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("delegation without independent Permission=%v", err)
		}
		originalConfirmation := confirmation
		confirmation = unprivileged
		if _, err := service.AcceptFulfillment(ctx, actor, uuid.New(), binding); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("confirmer without current Permission=%v", err)
		}
		confirmation = originalConfirmation
		forgeBasis = true
		if _, err := service.AcceptFulfillment(ctx, actor, uuid.New(), binding); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("forged owner response=%v", err)
		}
		forgeBasis = false
		closeBeforeAccept = true
		closedID := uuid.New()
		closed, err := service.AcceptFulfillment(ctx, actor, closedID, binding)
		if err != nil || closed.Outcome != "closed" {
			t.Fatalf("close committed during owner IO=%+v %v", closed, err)
		}
		closeBeforeAccept = false
		// Expiry clips the 5-minute system window; it is not silently extended.
		short := binding
		expires := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
		short.ExpiryMode = shared.SharingExpiryAtTime
		short.ExpiresAt = &expires
		clipped, err := service.AcceptFulfillment(ctx, actor, uuid.New(), short)
		if err != nil || clipped.Deadline == nil || !clipped.Deadline.Equal(expires) {
			t.Fatalf("clipped=%+v %v", clipped, err)
		}
		// Confirmer historical version is audit-only, not a current-version check.
		oldConfirmationVersion := confirmation.AuthorizationVersion
		confirmation.AuthorizationVersion = 1
		if _, err := service.AcceptFulfillment(ctx, actor, uuid.New(), binding); err != nil {
			t.Fatalf("confirmer audit version blocked acceptance: %v", err)
		}
		confirmation.AuthorizationVersion = oldConfirmationVersion
		if err := db.Table("system.principals").Where("id=?", operator.PrincipalID).Update("status", "suspended").Error; err != nil {
			t.Fatal(err)
		}
		service.WithFulfillmentBasisReader(nil)
		retried, err := service.AcceptFulfillment(ctx, actor, id, binding)
		if err != nil || !retried.RecordedAt.Equal(accepted.RecordedAt) || !retried.Deadline.Equal(*accepted.Deadline) {
			t.Fatalf("historical retry=%+v %v", retried, err)
		}
		changed := binding
		changed.RequirementVersion++
		if _, err := service.AcceptFulfillment(ctx, actor, id, changed); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("changed retry=%v", err)
		}
		badActor := actor
		badActor.AuthorizationVersion++
		if _, err := service.AcceptFulfillment(ctx, badActor, id, changed); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("invalid caller learned binding conflict: %v", err)
		}
		var n int64
		if err := db.Table("system.audit_logs").Where("entity_type='engine_access_fulfillment' AND entity_id=?", id.String()).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("duplicate audit=%d %v", n, err)
		}
	})
}
