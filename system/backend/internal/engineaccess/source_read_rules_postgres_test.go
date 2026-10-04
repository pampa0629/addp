package engineaccess

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Under the existing engineaccess PG gate: formal acceptance, grant issuance,
// organization commands, revocation and Deny commands create the facts. No
// business source is connected, and no HTTP/current-credential claim is made.
func exerciseCurrentSourceRules(t *testing.T, db *gorm.DB, acceptor *Service, runtime FulfillmentRuntimeActor,
	base shared.SharingFulfillmentBinding, roles *iam.TenantRoleService, fulfillmentRoleID, adminID int64,
	newUser func(*testing.T, time.Duration) (userProvenance, time.Time), seedDelegation func(*testing.T, int64, time.Time) *Delegation,
) {
	t.Run("current complete source read rules", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		repo := NewRepository(db)
		service := NewService(repo, nil) // No Catalog connection for observations / restriction commands.
		identity := iam.NewRepository(db)
		organizations := iam.NewOrganizationService(identity, time.Now)
		refresh := func(t *testing.T, user userProvenance) userProvenance {
			t.Helper()
			p, err := identity.GetPrincipal(ctx, user.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			user.AuthorizationVersion = p.AuthorizationVersion
			return user
		}
		read := func(t *testing.T, user userProvenance, paths ...engineplugin.EngineCatalogPath) *sourceReadRules {
			t.Helper()
			result, err := repo.readCurrentSourceRules(ctx, sourceReadRequest{TenantID: runtime.TenantID, Source: user, Targets: paths})
			if err != nil || result == nil {
				t.Fatalf("read=%+v %v", result, err)
			}
			return result
		}
		assert := func(t *testing.T, user userProvenance, reason string) {
			t.Helper()
			result := read(t, user, base.Path)
			if result.Targets[0].Reason != reason || result.Covered != (reason == "grant") {
				t.Fatalf("want=%s result=%+v", reason, result)
			}
		}
		grant := func(t *testing.T, kind string, id int64, expiry *time.Time) uuid.UUID {
			t.Helper()
			binding := base
			binding.RecipientType, binding.RecipientID = kind, id
			if expiry != nil {
				binding.ExpiryMode, binding.ExpiresAt = shared.SharingExpiryAtTime, expiry
			}
			requestID := uuid.New()
			if _, err := acceptor.AcceptFulfillment(ctx, runtime, requestID, binding); err != nil {
				t.Fatal(err)
			}
			if _, err := service.IssueFulfillmentGrant(ctx, runtime, requestID, binding); err != nil {
				t.Fatal(err)
			}
			return requestID
		}
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: runtime.TenantID, RoleKey: "custom.rule_fixture", Name: "Explicit rule commands", ScopeTypes: []string{"tenant"},
			PermissionKeys: []string{"system.engine_access_deny.create", "system.engine_access_deny.release", "system.engine_access_grant.revoke"}, ActorPrincipalID: adminID})
		if err != nil {
			t.Fatal(err)
		}
		commandUser, expiry := newUser(t, time.Hour)
		seedDelegation(t, commandUser.MembershipID, expiry.Add(-time.Second))
		if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: runtime.TenantID, MembershipID: commandUser.MembershipID, RoleIDs: []int64{role.ID, fulfillmentRoleID}, ScopeType: "tenant", ActorPrincipalID: adminID, Reason: "Explicit rule tests"}); err != nil {
			t.Fatal(err)
		}
		commandUser = refresh(t, commandUser)
		actor := Actor{TenantID: runtime.TenantID, PrincipalID: commandUser.PrincipalID, MembershipID: commandUser.MembershipID, AuthorizationVersion: commandUser.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Minute)}
		deny := func(t *testing.T, kind string, id int64, expiry *time.Time) uuid.UUID {
			t.Helper()
			input := CreateDenyInput{Actor: actor, EngineID: int64(base.Path.EngineID), DenyID: uuid.New(), CatalogPath: base.Path, RecipientType: kind, RecipientID: id, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Explicit precise restriction"}
			if expiry != nil {
				input.ExpiryMode, input.ExpiresAt = shared.SharingExpiryAtTime, expiry
			}
			if _, err := service.CreateDeny(ctx, input); err != nil {
				t.Fatal(err)
			}
			return input.DenyID
		}
		release := func(t *testing.T, id uuid.UUID) {
			t.Helper()
			if _, err := service.ReleaseDeny(ctx, ReleaseDenyInput{Actor: actor, EngineID: int64(base.Path.EngineID), DenyID: id, Reason: "Restriction no longer needed"}); err != nil {
				t.Fatal(err)
			}
		}
		revoke := func(t *testing.T, id uuid.UUID) {
			t.Helper()
			if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(base.Path.EngineID), RequestID: id, Reason: "End precise grant"}); err != nil {
				t.Fatal(err)
			}
		}
		t.Run("personal grant complete set and current identity", func(t *testing.T) {
			user, _ := newUser(t, time.Hour)
			assert(t, user, "no_grant")
			grantID := grant(t, "user", user.PrincipalID, nil)
			assert(t, user, "grant")
			d := engineplugin.TabularItemPath(base.Path.EngineID, "schema", "public", "ungranted_D")
			result := read(t, user, base.Path, d, base.Path)
			if result.Covered || len(result.Targets) != 2 || !result.Targets[0].Covered || result.Targets[1].Reason != "no_grant" {
				t.Fatalf("C+D=%+v", result)
			}
			wrong := user
			wrong.AuthorizationVersion++
			assert(t, wrong, "source_unavailable")
			wrong = user
			wrong.MembershipID = commandUser.MembershipID
			assert(t, wrong, "source_unavailable")
			crossTenant := sourceReadRequest{TenantID: runtime.TenantID + 1000, Source: user, Targets: []engineplugin.EngineCatalogPath{base.Path}}
			if result, err := repo.readCurrentSourceRules(ctx, crossTenant); err != nil || result == nil || result.Covered {
				t.Fatalf("cross tenant=%+v %v", result, err)
			}
			for _, mutation := range []struct {
				table, column     string
				id                int64
				invalid, restored any
			}{
				{"system.principals", "status", user.PrincipalID, "suspended", "active"},
				{"system.tenant_memberships", "expires_at", user.MembershipID, gorm.Expr("joined_at + interval '1 microsecond'"), nil},
				{"system.engines", "lifecycle_state", int64(base.Path.EngineID), "disabled", "active"},
			} {
				func() {
					if err := db.Table(mutation.table).Where("id=?", mutation.id).Update(mutation.column, mutation.invalid).Error; err != nil {
						t.Fatal(err)
					}
					defer func() {
						if err := db.Table(mutation.table).Where("id=?", mutation.id).Update(mutation.column, mutation.restored).Error; err != nil {
							t.Error(err)
						}
					}()
					reason := "source_unavailable"
					if mutation.table == "system.engines" {
						reason = "target_unavailable"
					}
					assert(t, refresh(t, user), reason)
				}()
			}
			user = refresh(t, user)
			assert(t, user, "grant")
			denyID := deny(t, "user", user.PrincipalID, nil)
			assert(t, user, "explicit_deny")
			// Test-only clock substitution uses the exact production SQL and
			// committed facts, never changes the server clock or immutable history.
			// Establishment is not ValidFrom: a small wall-clock rollback must
			// not ignore an already committed Deny and expose the personal Allow.
			row, err := repo.findSourceDeny(ctx, denyID)
			if err != nil {
				t.Fatal(err)
			}
			request := sourceReadRequest{TenantID: runtime.TenantID, Source: user, Targets: []engineplugin.EngineCatalogPath{base.Path}}
			paths, encoded, err := request.encode()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(currentSourceReadRulesSQL, "clock_timestamp()") != 1 {
				t.Fatal("observation must have exactly one database clock")
			}
			var rows []sourceReadRuleRow
			err = repo.readCommitted(ctx, func(tx *Repository) error {
				return tx.db.Raw(strings.Replace(currentSourceReadRulesSQL, "clock_timestamp()", "?::timestamptz", 1),
					request.TenantID, user.PrincipalID, user.MembershipID, user.AuthorizationVersion, string(encoded), row.EstablishedAt.Add(-time.Microsecond)).Scan(&rows).Error
			})
			if err != nil {
				t.Fatal(err)
			}
			rolledBackClock, err := sourceReadObservation(paths, rows)
			if err != nil || rolledBackClock == nil || rolledBackClock.Covered || rolledBackClock.Targets[0].Reason != "explicit_deny" {
				t.Fatalf("wall-clock rollback bypassed Deny=%+v %v", rolledBackClock, err)
			}
			revoke(t, grantID)
			release(t, denyID)
			assert(t, user, "no_grant") // Release is not restoration.
		})
		t.Run("group Allow personal Deny and current membership", func(t *testing.T) {
			user, _ := newUser(t, time.Hour)
			group, err := organizations.CreateProjectGroup(ctx, iam.CreateProjectGroupInput{TenantID: runtime.TenantID, ActorPrincipalID: adminID, Code: "rule_group", Name: "Rule group"})
			if err != nil {
				t.Fatal(err)
			}
			member, err := organizations.CreateProjectGroupMembership(ctx, iam.CreateProjectGroupMembershipInput{TenantID: runtime.TenantID, ProjectGroupID: group.ID, TenantMembershipID: user.MembershipID, ActorPrincipalID: adminID, RelationRole: "member"})
			if err != nil {
				t.Fatal(err)
			}
			grant(t, "project_group", group.ID, nil)
			stale := user
			user = refresh(t, user)
			assert(t, stale, "source_unavailable")
			assert(t, user, "grant")
			personal := grant(t, "user", user.PrincipalID, nil)
			revoke(t, personal)
			assert(t, user, "grant") // Another independent Allow remains.
			denyID := deny(t, "user", user.PrincipalID, nil)
			assert(t, user, "explicit_deny")
			release(t, denyID)
			assert(t, user, "grant")
			groupDeny := deny(t, "project_group", group.ID, nil)
			assert(t, user, "explicit_deny")
			release(t, groupDeny)
			if _, err := organizations.CloseProjectGroupMembership(ctx, iam.CloseOrganizationMembershipInput{TenantID: runtime.TenantID, OrganizationID: group.ID, MembershipID: member.ID, Version: member.Version, ActorPrincipalID: adminID, Reason: "End collaboration"}); err != nil {
				t.Fatal(err)
			}
			assert(t, refresh(t, user), "no_grant")
			if _, err := organizations.CreateProjectGroupMembership(ctx, iam.CreateProjectGroupMembershipInput{TenantID: runtime.TenantID, ProjectGroupID: group.ID, TenantMembershipID: user.MembershipID, ActorPrincipalID: adminID, RelationRole: "member"}); err != nil {
				t.Fatal(err)
			}
			assert(t, refresh(t, user), "grant")
			if _, err := organizations.CloseProjectGroup(ctx, iam.CloseProjectGroupInput{TenantID: runtime.TenantID, ProjectGroupID: group.ID, Version: group.Version, ActorPrincipalID: adminID, Reason: "Close recipient group"}); err != nil {
				t.Fatal(err)
			}
			assert(t, refresh(t, user), "no_grant")
		})
		t.Run("department is direct not ancestor inheritance", func(t *testing.T) {
			user, _ := newUser(t, time.Hour)
			parent, err := organizations.CreateDepartment(ctx, iam.CreateDepartmentInput{TenantID: runtime.TenantID, ActorPrincipalID: adminID, Code: "rule_parent", Name: "Parent"})
			if err != nil {
				t.Fatal(err)
			}
			child, err := organizations.CreateDepartment(ctx, iam.CreateDepartmentInput{TenantID: runtime.TenantID, ActorPrincipalID: adminID, Code: "rule_child", Name: "Child", ParentID: &parent.ID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := organizations.CreateDepartmentMembership(ctx, iam.CreateDepartmentMembershipInput{TenantID: runtime.TenantID, DepartmentID: child.ID, TenantMembershipID: user.MembershipID, ActorPrincipalID: adminID, MembershipType: "primary", RelationRole: "member"}); err != nil {
				t.Fatal(err)
			}
			grant(t, "user", user.PrincipalID, nil)
			deny(t, "department", parent.ID, nil)
			user = refresh(t, user)
			assert(t, user, "grant")
			deny(t, "department", child.ID, nil)
			assert(t, user, "explicit_deny")
			if _, err := organizations.DisableDepartment(ctx, iam.ChangeDepartmentStatusInput{TenantID: runtime.TenantID, DepartmentID: child.ID, Version: child.Version, ActorPrincipalID: adminID, Reason: "Disable recipient department"}); err != nil {
				t.Fatal(err)
			}
			assert(t, refresh(t, user), "grant")
		})
		t.Run("database expiry of Grant and Deny", func(t *testing.T) {
			waitUntil := func(expires time.Time) {
				t.Helper()
				if err := db.Exec("SELECT pg_sleep(GREATEST(0, EXTRACT(EPOCH FROM (?::timestamptz - clock_timestamp()))) + 0.01)", expires).Error; err != nil {
					t.Fatal(err)
				}
			}
			user, _ := newUser(t, time.Hour)
			now, err := repo.wallClock(ctx)
			if err != nil {
				t.Fatal(err)
			}
			expires := now.Add(900 * time.Millisecond)
			grant(t, "user", user.PrincipalID, &expires)
			assert(t, user, "grant")
			waitUntil(expires)
			assert(t, user, "no_grant")
			grant(t, "user", user.PrincipalID, nil)
			now, err = repo.wallClock(ctx)
			if err != nil {
				t.Fatal(err)
			}
			expires = now.Add(900 * time.Millisecond)
			deny(t, "user", user.PrincipalID, &expires)
			assert(t, user, "explicit_deny")
			waitUntil(expires)
			assert(t, user, "grant")
		})
		t.Run("committed read only snapshot no target lock or side effects", func(t *testing.T) {
			user, _ := newUser(t, time.Hour)
			binding := base
			binding.RecipientType, binding.RecipientID = "user", user.PrincipalID
			id := uuid.New()
			if _, err := acceptor.AcceptFulfillment(ctx, runtime, id, binding); err != nil {
				t.Fatal(err)
			}
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			path, err := shared.EncodeSharingTarget(base.Path)
			if err != nil {
				t.Fatal(err)
			}
			if err := NewRepository(tx).lockFulfillmentTarget(ctx, runtime.TenantID, path); err != nil {
				t.Fatal(err)
			}
			if err := tx.Create(&fulfillmentGrant{RequestID: id}).Error; err != nil {
				t.Fatal(err)
			}
			request := sourceReadRequest{TenantID: runtime.TenantID, Source: user, Targets: []engineplugin.EngineCatalogPath{base.Path}}
			if result, err := NewRepository(tx).readCurrentSourceRules(ctx, request); result != nil || !errors.Is(err, errFulfillmentBinding) {
				t.Fatalf("uncommitted own writes=%+v %v", result, err)
			}
			readCtx, stop := context.WithTimeout(ctx, 2*time.Second)
			defer stop()
			if result, err := repo.readCurrentSourceRules(readCtx, request); err != nil || result == nil || result.Covered {
				t.Fatalf("uncommitted independent observation=%+v %v", result, err)
			}
			if err := tx.Rollback().Error; err != nil {
				t.Fatal(err)
			}
			if _, err := service.IssueFulfillmentGrant(ctx, runtime, id, binding); err != nil {
				t.Fatal(err)
			}
			var before, after int64
			if err := db.Table("system.audit_logs").Count(&before).Error; err != nil {
				t.Fatal(err)
			}
			assert(t, user, "grant")
			if err := db.Table("system.audit_logs").Count(&after).Error; err != nil || before != after {
				t.Fatalf("read wrote audit %d -> %d %v", before, after, err)
			}
			deadCtx, stopDead := context.WithCancel(ctx)
			stopDead()
			if result, err := repo.readCurrentSourceRules(deadCtx, request); result != nil || err == nil {
				t.Fatalf("database failure became Allow=%+v %v", result, err)
			}
		})
		exerciseSourceReadCredentials(t, db, base.Path, newUser, grant, roles, runtime.TenantID, adminID, deny, revoke)
		t.Run("later handler suspension does not revoke recipient Grant", func(t *testing.T) {
			user, _ := newUser(t, time.Hour)
			binding := base
			binding.Operator = shared.SharingFulfillmentOperator{PrincipalID: commandUser.PrincipalID, MembershipID: commandUser.MembershipID, AuthorizationVersion: commandUser.AuthorizationVersion}
			binding.RecipientType, binding.RecipientID = "user", user.PrincipalID
			id := uuid.New()
			if _, err := acceptor.AcceptFulfillment(ctx, runtime, id, binding); err != nil {
				t.Fatal(err)
			}
			if _, err := service.IssueFulfillmentGrant(ctx, runtime, id, binding); err != nil {
				t.Fatal(err)
			}
			if err := db.Table("system.principals").Where("id=?", commandUser.PrincipalID).Update("status", "suspended").Error; err != nil {
				t.Fatal(err)
			}
			assert(t, user, "grant")
		})
	})
}
