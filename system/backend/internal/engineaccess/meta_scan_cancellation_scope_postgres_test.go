package engineaccess

import (
	"context"
	"errors"
	commonapi "github.com/addp/common/api"

	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
	"testing"
	"time"
)

func exerciseMetaCollectionManagement(t *testing.T, db *gorm.DB, tenantID, adminID int64, engine models.Engine, newUser func(*testing.T, time.Duration) (userProvenance, time.Time), seedDelegation func(*testing.T, int64, time.Time) *Delegation) {
	t.Run("collection current function and engine scope", func(t *testing.T) {
		ctx := context.Background()
		identity := iam.NewRepository(db)
		roles := iam.NewTenantRoleService(identity, testDatabaseClock(t, db))
		var assigned int64
		if err := db.Raw("SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='meta.scan_task.cancel'").Scan(&assigned).Error; err != nil || assigned != 0 {
			t.Fatal("cancel must not be automatically assigned", assigned, err)
		}
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.collection_cancel", Name: "Explicit cancellation fixture", PermissionKeys: []string{"meta.scan_task.cancel"}, ActorPrincipalID: adminID})
		if err != nil {
			t.Fatal(err)
		}
		user, expires := newUser(t, time.Hour)
		actor := Actor{TenantID: tenantID, PrincipalID: user.PrincipalID, MembershipID: user.MembershipID, AuthorizationVersion: user.AuthorizationVersion, TokenExpiresAt: expires}
		svc := NewService(NewRepository(db), nil)
		checkDenied := func() {
			t.Helper()
			if _, err := svc.GetMetaScanCancellationScope(ctx, actor, int64(engine.ID)); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatal("missing qualification allowed", err)
			}
		}
		checkDenied()
		if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: adminID, Reason: "Explicit fixture permission"}); err != nil {
			t.Fatal(err)
		}
		principal, err := identity.GetPrincipal(ctx, user.PrincipalID)
		if err != nil {
			t.Fatal(err)
		}
		actor.AuthorizationVersion = principal.AuthorizationVersion
		checkDenied() // function permission alone is insufficient
		delegation := seedDelegation(t, user.MembershipID, expires.Add(-time.Second))
		observation, err := svc.GetMetaScanCancellationScope(ctx, actor, int64(engine.ID))
		if err != nil || observation == nil || observation.EngineID != int64(engine.ID) || observation.Operator.PrincipalID != user.PrincipalID {
			t.Fatalf("current scope %v %v", observation, err)
		}
		if err := db.Table("system.engines").Where("id=?", engine.ID).Update("lifecycle_state", models.EngineLifecycleDisabled).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := svc.GetMetaScanCancellationScope(ctx, actor, int64(engine.ID)); err != nil {
			t.Fatal("disabled engine must remain cancellable", err)
		}
		if err := db.Table("system.engines").Where("id=?", engine.ID).Update("lifecycle_state", engine.LifecycleState).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(delegation).Updates(map[string]any{"status": "revoked", "version": delegation.Version + 1, "revoked_at": gorm.Expr("clock_timestamp()"), "revoked_by_principal_id": adminID, "revoked_reason": "Cancellation scope fixture"}).Error; err != nil {
			t.Fatal(err)
		}
		checkDenied() // no stale scope reuse
	})
}
