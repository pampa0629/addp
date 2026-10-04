package engineaccess

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Reuse formal IAM and engine fixtures under the standard disposable PG gate.
func exerciseSourceDenies(t *testing.T, db *gorm.DB, tenantID int64, path engineplugin.EngineCatalogPath,
	roles *iam.TenantRoleService, adminID int64, newOperator func(*testing.T, time.Duration) (userProvenance, time.Time),
	seedDelegation func(*testing.T, int64, time.Time) *Delegation) {
	t.Run("source Deny establishment", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		service := NewService(NewRepository(db), nil)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.deny_creator", Name: "Explicit Deny fixture", ScopeTypes: []string{"tenant"}, PermissionKeys: []string{"system.engine_access_deny.create"}, ActorPrincipalID: adminID})
		if err != nil {
			t.Fatal(err)
		}
		qualified := func(t *testing.T, permission, delegation bool) (Actor, *Delegation) {
			t.Helper()
			user, expires := newOperator(t, time.Hour)
			var delegated *Delegation
			if delegation {
				delegated = seedDelegation(t, user.MembershipID, expires.Add(-time.Second))
			}
			if permission {
				if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ScopeType: "tenant", ActorPrincipalID: adminID, Reason: "Explicit Deny test"}); err != nil {
					t.Fatal(err)
				}
			}
			current, err := iam.NewRepository(db).GetPrincipal(ctx, user.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			return Actor{TenantID: tenantID, PrincipalID: user.PrincipalID, MembershipID: user.MembershipID, AuthorizationVersion: current.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}, delegated
		}
		recipient, _ := newOperator(t, time.Hour)
		actor, delegation := qualified(t, true, true)
		inputFor := func() CreateDenyInput {
			return CreateDenyInput{Actor: actor, EngineID: int64(path.EngineID), DenyID: uuid.New(), CatalogPath: path, RecipientType: "user", RecipientID: recipient.PrincipalID, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Restrict precise reading"}
		}
		assertCount := func(t *testing.T, id uuid.UUID, want int64) {
			t.Helper()
			for _, table := range []string{"system.engine_access_denies", "system.audit_logs"} {
				query := db.Table(table).Where("deny_id=?", id)
				if table == "system.audit_logs" {
					query = db.Table(table).Where("event_name='system.engine_access_deny.created' AND entity_id=?", id.String())
				}
				var n int64
				if err := query.Count(&n).Error; err != nil || n != want {
					t.Fatalf("%s count=%d want=%d err=%v", table, n, want, err)
				}
			}
		}
		t.Run("both independent qualifications required", func(t *testing.T) {
			for _, pair := range [][2]bool{{false, true}, {true, false}, {false, false}} {
				v := inputFor()
				v.Actor, _ = qualified(t, pair[0], pair[1])
				if row, err := service.CreateDeny(ctx, v); row != nil || !errors.Is(err, commonapi.ErrForbidden) {
					t.Fatalf("qualifications=%v result=%+v err=%v", pair, row, err)
				}
				assertCount(t, v.DenyID, 0)
			}
		})
		t.Run("disabled engine and no Catalog permit restriction", func(t *testing.T) {
			if err := db.Table("system.engines").Where("id=?", path.EngineID).Update("lifecycle_state", "disabled").Error; err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := db.Table("system.engines").Where("id=?", path.EngineID).Update("lifecycle_state", "active").Error; err != nil {
					t.Error(err)
				}
			}()
			v := inputFor()
			original, err := service.CreateDeny(ctx, v)
			if err != nil || original == nil {
				t.Fatalf("disabled=%+v %v", original, err)
			}
			v.Reason = "  " + v.Reason + "\n"
			replay, err := service.CreateDeny(ctx, v)
			if err != nil || replay == nil || !replay.EstablishedAt.Equal(original.EstablishedAt) {
				t.Fatalf("replay=%+v %v", replay, err)
			}
			assertCount(t, v.DenyID, 1)
			v.Reason = "Different command"
			if _, err := service.CreateDeny(ctx, v); !errors.Is(err, ErrDenyConflict) {
				t.Fatalf("changed retry=%v", err)
			}
			v.Reason = original.Reason
			v.Actor.AuthorizationVersion--
			if _, err := service.CreateDeny(ctx, v); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("stale history access=%v", err)
			}
			v.Actor = actor
			v.Actor.TokenExpiresAt = time.Now().Add(-time.Second)
			if _, err := service.CreateDeny(ctx, v); !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("expired credential=%v", err)
			}
			for _, statement := range []string{"UPDATE system.engine_access_denies SET reason='overwrite' WHERE deny_id=?", "DELETE FROM system.engine_access_denies WHERE deny_id=?", "TRUNCATE system.engine_access_denies"} {
				tx := db.Begin()
				if tx.Error != nil {
					t.Fatal(tx.Error)
				}
				var err error
				if statement == "TRUNCATE system.engine_access_denies" {
					err = tx.Exec(statement).Error
				} else {
					err = tx.Exec(statement, original.DenyID).Error
				}
				tx.Rollback()
				if err == nil {
					t.Fatalf("history mutable: %s", statement)
				}
			}
			assertCount(t, v.DenyID, 1)
		})
		t.Run("group and department use authoritative recipients", func(t *testing.T) {
			org := iam.NewOrganizationService(iam.NewRepository(db), time.Now)
			dept, err := org.CreateDepartment(ctx, iam.CreateDepartmentInput{TenantID: tenantID, ActorPrincipalID: adminID, Code: "deny_department", Name: "Deny Department"})
			if err != nil {
				t.Fatal(err)
			}
			group, err := org.CreateProjectGroup(ctx, iam.CreateProjectGroupInput{TenantID: tenantID, ActorPrincipalID: adminID, Code: "deny_group", Name: "Deny Group"})
			if err != nil {
				t.Fatal(err)
			}
			for kind, id := range map[string]int64{"department": dept.ID, "project_group": group.ID} {
				v := inputFor()
				v.RecipientType, v.RecipientID = kind, id
				if _, err := service.CreateDeny(ctx, v); err != nil {
					t.Fatalf("%s: %v", kind, err)
				}
				assertCount(t, v.DenyID, 1)
			}
		})
		t.Run("expiry before or during commit cannot establish a rule", func(t *testing.T) {
			v := inputFor()
			var expires time.Time
			if err := db.Raw("SELECT clock_timestamp()-interval '1 second'").Scan(&expires).Error; err != nil {
				t.Fatal(err)
			}
			v.ExpiryMode, v.ExpiresAt = shared.SharingExpiryAtTime, &expires
			if _, err := service.CreateDeny(ctx, v); !errors.Is(err, ErrDenyExpiry) {
				t.Fatalf("past=%v", err)
			}
			assertCount(t, v.DenyID, 0)
			row, err := prepareSourceDeny(v)
			if err != nil {
				t.Fatal(err)
			}
			row.EstablishedAt = expires.Add(-time.Hour)
			tx := db.Begin()
			if err := mapError(tx.Create(row).Error); !errors.Is(err, ErrDenyExpiry) {
				tx.Rollback()
				t.Fatalf("backdate bypass=%v", err)
			}
			tx.Rollback()
			v.DenyID = uuid.New()
			if err := db.Raw("SELECT clock_timestamp()+interval '0.4 seconds'").Scan(&expires).Error; err != nil {
				t.Fatal(err)
			}
			v.ExpiresAt = &expires
			const callback = "fixture:deny_audit_expiry"
			reached := false
			if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
				if log, ok := tx.Statement.Dest.(*iam.AuditLog); ok && log.EventName == "system.engine_access_deny.created" {
					reached = tx.Error == nil
					tx.AddError(tx.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Exec("SELECT pg_sleep(GREATEST(EXTRACT(EPOCH FROM (?::timestamptz-clock_timestamp()))+0.03,0))", expires).Error)
				}
			}); err != nil {
				t.Fatal(err)
			}
			_, err = service.CreateDeny(ctx, v)
			db.Callback().Create().Remove(callback)
			if !reached || !errors.Is(err, ErrDenyExpiry) {
				t.Fatalf("commit expiry reached=%v err=%v", reached, err)
			}
			assertCount(t, v.DenyID, 0)
		})
		t.Run("target lock wait rechecks expiry", func(t *testing.T) {
			v := inputFor()
			var expires time.Time
			if err := db.Raw("SELECT clock_timestamp()+interval '0.5 seconds'").Scan(&expires).Error; err != nil {
				t.Fatal(err)
			}
			v.ExpiryMode, v.ExpiresAt = shared.SharingExpiryAtTime, &expires
			encoded, err := shared.EncodeSharingTarget(path)
			if err != nil {
				t.Fatal(err)
			}
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			if err := NewRepository(tx).lockFulfillmentTarget(ctx, tenantID, encoded); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() { _, err := service.CreateDeny(ctx, v); result <- err }()
			blocked := false
			for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
				if err := db.Raw("SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))").Scan(&blocked).Error; err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !blocked {
				t.Fatal("Deny bypassed the precise target write boundary")
			}
			if err := db.Exec("SELECT pg_sleep(GREATEST(EXTRACT(EPOCH FROM (?::timestamptz-clock_timestamp()))+0.03,0))", expires).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Rollback().Error; err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, ErrDenyExpiry) {
				t.Fatalf("wait expiry=%v", err)
			}
			assertCount(t, v.DenyID, 0)
		})
		t.Run("audit failure rolls back rule", func(t *testing.T) {
			v := inputFor()
			const callback = "fixture:deny_audit_failure"
			if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if log, ok := tx.Statement.Dest.(*iam.AuditLog); ok && log.EventName == "system.engine_access_deny.created" {
					tx.AddError(errors.New("Deny fixture audit failure"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			_, err := service.CreateDeny(ctx, v)
			db.Callback().Create().Remove(callback)
			if err == nil {
				t.Fatal("audit failure accepted")
			}
			assertCount(t, v.DenyID, 0)
		})
		t.Run("concurrent same command writes one fact", func(t *testing.T) {
			v := inputFor()
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); _, err := service.CreateDeny(ctx, v); results <- err }()
			}
			wg.Wait()
			close(results)
			for err := range results {
				if err != nil {
					t.Fatal(err)
				}
			}
			assertCount(t, v.DenyID, 1)
		})
		t.Run("expired history remains immutable and subject inactivity does not erase it", func(t *testing.T) {
			v := inputFor()
			var expires time.Time
			if err := db.Raw("SELECT clock_timestamp()+interval '0.3 seconds'").Scan(&expires).Error; err != nil {
				t.Fatal(err)
			}
			v.ExpiryMode, v.ExpiresAt = shared.SharingExpiryAtTime, &expires
			original, err := service.CreateDeny(ctx, v)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("SELECT pg_sleep(GREATEST(EXTRACT(EPOCH FROM (?::timestamptz-clock_timestamp()))+0.03,0))", expires).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := iam.NewTenantMembershipService(iam.NewRepository(db), time.Now).SuspendMembership(ctx, iam.ChangeTenantMembershipInput{TenantID: tenantID, PrincipalID: recipient.PrincipalID, Reason: "Deny recipient test"}); err != nil {
				t.Fatal(err)
			}
			replay, err := service.CreateDeny(ctx, v)
			if err != nil || replay == nil || !replay.EstablishedAt.Equal(original.EstablishedAt) {
				t.Fatalf("history=%+v %v", replay, err)
			}
			assertCount(t, v.DenyID, 1)
			v.DenyID = uuid.New()
			v.ExpiryMode, v.ExpiresAt = shared.SharingExpiryUntilRevoked, nil
			if _, err := service.CreateDeny(ctx, v); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("new inactive subject=%v", err)
			}
			assertCount(t, v.DenyID, 0)
			if err := db.Table("system.engine_access_delegations").Where("id=?", delegation.ID).Updates(map[string]any{"status": "revoked", "version": delegation.Version + 1, "revoked_at": time.Now(), "revoked_by_principal_id": adminID, "revoked_reason": "Deny qualification test"}).Error; err != nil {
				t.Fatal(err)
			}
			v.DenyID, v.ExpiryMode, v.ExpiresAt = original.DenyID, original.ExpiryMode, original.ExpiresAt
			if _, err := service.CreateDeny(ctx, v); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("revoked delegation permitted historical retry: %v", err)
			}
		})
	})
}
