package engineaccess

import (
	"context"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type independentVerifierFunc func(context.Context, int64, engineplugin.EngineCatalogPath) (int64, error)

func (f independentVerifierFunc) VerifyIndependentTable(c context.Context, id int64, p engineplugin.EngineCatalogPath) (int64, error) {
	return f(c, id, p)
}

// Runs inside the standard disposable PostgreSQL gate. Public commands create
// the approval basis, Grant and revocation; the fake Provider reads no source.
func exerciseIndependentGrantCommands(t *testing.T, db *gorm.DB, tenantID int64, base engineplugin.EngineCatalogPath, roles *iam.TenantRoleService, adminID int64,
	newUser func(*testing.T, time.Duration) (userProvenance, time.Time), seedDelegation func(*testing.T, int64, time.Time) *Delegation) {
	t.Run("independent grant formal commands", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.independent_grants", Name: "Independent grant fixture", ScopeTypes: []string{"tenant"}, PermissionKeys: []string{"system.engine_access_grant.create", "system.engine_access_grant.read", "system.engine_access_grant.revoke", "system.engine_access_approval_requirement.initialize"}, ActorPrincipalID: adminID})
		if err != nil {
			t.Fatal(err)
		}
		qualify := func(t *testing.T, delegate bool) Actor {
			user, expires := newUser(t, time.Hour)
			if delegate {
				seedDelegation(t, user.MembershipID, expires.Add(-time.Second))
			}
			if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ScopeType: "tenant", ActorPrincipalID: adminID, Reason: "Explicit fixture permission"}); err != nil {
				t.Fatal(err)
			}
			p, err := iam.NewRepository(db).GetPrincipal(ctx, user.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			return Actor{TenantID: tenantID, PrincipalID: user.PrincipalID, MembershipID: user.MembershipID, AuthorizationVersion: p.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}
		}
		actor := qualify(t, true)
		path := engineplugin.TabularItemPath(base.EngineID, "schema", "public", "independent_commands")
		// Source inspection must not hold any IAM lock. NOWAIT proves that an
		// independent connection can lock the operator during Provider IO.
		calls := 0
		service := NewService(NewRepository(db), func(*models.Engine) bool { return true }).WithIndependentTargetVerifier(independentVerifierFunc(func(c context.Context, tenant int64, p engineplugin.EngineCatalogPath) (int64, error) {
			calls++
			if tenant != tenantID || p.EngineID != base.EngineID {
				t.Fatal("wrong discovery scope")
			}
			if err := db.WithContext(c).Transaction(func(tx *gorm.DB) error {
				return tx.Exec("SELECT id FROM system.principals WHERE id=? FOR UPDATE NOWAIT", actor.PrincipalID).Error
			}); err != nil {
				return 0, err
			}
			engine, err := NewRepository(db).engine(c, tenantID, int64(base.EngineID), false)
			if err != nil {
				return 0, err
			}
			return engine.Version, nil
		}))
		basis, err := service.InitializeApprovalRequirement(ctx, InitializeApprovalRequirementInput{Actor: actor, EngineID: int64(base.EngineID), CatalogPath: path, Mode: "independent", Reason: "Explicit independent approval"})
		if err != nil {
			t.Fatal(err)
		}
		receiver, _ := newUser(t, time.Hour)
		input := CreateIndependentGrantInput{Actor: actor, EngineID: int64(base.EngineID), RequestID: uuid.New(), CatalogPath: path, RequirementVersion: basis.Version, RecipientType: "user", RecipientID: receiver.PrincipalID, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Explicit standalone read access"}
		assertRead := func(want string) {
			t.Helper()
			result, err := NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{TenantID: tenantID, Source: receiver, Targets: []engineplugin.EngineCatalogPath{path}})
			if err != nil || result == nil || result.Targets[0].Reason != want {
				t.Fatalf("want %s: %+v %v", want, result, err)
			}
		}
		assertRead("no_grant")
		withoutPermission, expires := newUser(t, time.Hour)
		seedDelegation(t, withoutPermission.MembershipID, expires.Add(-time.Second))
		for _, unauthorized := range []Actor{qualify(t, false), {TenantID: tenantID, PrincipalID: withoutPermission.PrincipalID, MembershipID: withoutPermission.MembershipID, AuthorizationVersion: withoutPermission.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}} {
			bad := input
			bad.Actor = unauthorized
			if row, err := service.CreateIndependentGrant(ctx, bad); row != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("unqualified command: %+v %v", row, err)
			}
		}
		if calls != 0 {
			t.Fatal("unqualified command inspected source")
		}
		for _, mutate := range []func(*CreateIndependentGrantInput){func(i *CreateIndependentGrantInput) { i.RequirementVersion++ }, func(i *CreateIndependentGrantInput) {
			i.CatalogPath = engineplugin.TabularItemPath(base.EngineID, "schema", "public", "missing_approval")
		}} {
			bad := input
			bad.RequestID = uuid.New()
			mutate(&bad)
			if _, err := service.CreateIndependentGrant(ctx, bad); !errors.Is(err, ErrIndependentGrantBasis) {
				t.Fatalf("unconfigured or stale basis: %v", err)
			}
		}
		badExpiry := input
		badExpiry.RequestID = uuid.New()
		past := time.Now().Add(-time.Minute)
		badExpiry.ExpiryMode = shared.SharingExpiryAtTime
		badExpiry.ExpiresAt = &past
		if _, err := service.CreateIndependentGrant(ctx, badExpiry); !errors.Is(err, ErrIndependentGrantExpiry) {
			t.Fatalf("expired first issuance: %v", err)
		}
		issued, err := service.CreateIndependentGrant(ctx, input)
		if err != nil || issued == nil || issued.ApprovalMode != "independent" || issued.CatalogRequestID != nil || issued.Revocation != nil {
			t.Fatalf("issue=%+v %v", issued, err)
		}
		assertRead("grant")
		beforeRetry := calls
		service.WithIndependentTargetVerifier(nil)
		retried, err := service.CreateIndependentGrant(ctx, input)
		if err != nil || retried == nil || !retried.GrantedAt.Equal(issued.GrantedAt) || calls != beforeRetry {
			t.Fatalf("historical retry: %+v %v", retried, err)
		}
		changed := input
		changed.Reason = "Different reason"
		if _, err := service.CreateIndependentGrant(ctx, changed); !errors.Is(err, ErrIndependentGrantConflict) {
			t.Fatalf("changed command: %v", err)
		}
		rows, total, err := service.ListSourceGrants(ctx, actor, input.EngineID, 1, 100)
		if err != nil || total < int64(len(rows)) || len(rows) == 0 {
			t.Fatalf("list: %+v %d %v", rows, total, err)
		}
		found := false
		for _, r := range rows {
			if r.RequestID == input.RequestID {
				found = true
			}
		}
		if !found {
			t.Fatal("issued grant missing from history")
		}
		revoked, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: input.EngineID, RequestID: input.RequestID, Reason: "End this read access"})
		if err != nil {
			t.Fatal(err)
		}
		assertRead("no_grant")
		retried, err = service.CreateIndependentGrant(ctx, input)
		if err != nil || retried == nil || retried.Revocation == nil || !retried.Revocation.RevokedAt.Equal(revoked.RevokedAt) {
			t.Fatalf("retry restored withdrawn grant: %+v %v", retried, err)
		}
		var auditCount, receiptCount int64
		if err := db.Table("system.audit_logs").Where("event_name='system.engine_access_grant.issued' AND entity_id=?", input.RequestID.String()).Count(&auditCount).Error; err != nil || auditCount != 1 {
			t.Fatalf("issuance audit=%d %v", auditCount, err)
		}
		if err := db.Table("system.engine_access_fulfillment_outcomes").Where("request_id=?", input.RequestID).Count(&receiptCount).Error; err != nil || receiptCount != 0 {
			t.Fatalf("fake Catalog receipt=%d %v", receiptCount, err)
		}
	})
}
