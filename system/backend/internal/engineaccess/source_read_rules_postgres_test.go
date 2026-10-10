package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/authorization"
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
		// This independent exercise models a fresh bounded Runtime request, not
		// a one-minute fixture credential consumed by earlier serial exercises.
		var runtimeNow time.Time
		if err := db.WithContext(ctx).Raw("SELECT clock_timestamp()").Scan(&runtimeNow).Error; err != nil {
			t.Fatal(err)
		}
		runtime.TokenExpiresAt = runtimeNow.Add(time.Minute)
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
			PermissionKeys: []string{"system.engine_access_deny.create", "system.engine_access_deny.release", "system.engine_access_grant.revoke", "system.engine_access_grant.read"}, ActorPrincipalID: adminID})
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
		inspect := func(t *testing.T, user userProvenance, reason string, kinds ...string) {
			t.Helper()
			observation, err := service.InspectSourceGrants(ctx, actor, int64(base.Path.EngineID), user.PrincipalID, base.Path)
			if err != nil || observation == nil || observation.Reason != reason || observation.RuleCovered != (reason == "grant") || observation.ObservedAt.IsZero() || observation.AccountID != user.PrincipalID || len(observation.Sources) != len(kinds) {
				t.Fatalf("inspection=%+v want=%s kinds=%v err=%v", observation, reason, kinds, err)
			}
			listed, total, listErr := service.ListSourceGrantRelations(ctx, actor, int64(base.Path.EngineID), 1, 1,
				SourceGrantFilter{RecipientType: "user", RecipientID: user.PrincipalID})
			if listErr != nil {
				t.Fatal(listErr)
			}
			if len(kinds) > 0 {
				if total != 1 || len(listed) != 1 || listed[0].Inspection == nil || listed[0].Inspection.Reason != reason || len(listed[0].Inspection.Sources) != len(kinds) {
					t.Fatalf("unified account list=%+v total=%d error=%v", listed, total, listErr)
				}
				for _, source := range listed[0].Inspection.Sources {
					if source.RequestID == uuid.Nil || source.ApprovalMode == "" {
						t.Fatalf("missing source-specific revoke anchor: %+v", source)
					}
				}
				empty, count, err := service.ListSourceGrantRelations(ctx, actor, int64(base.Path.EngineID), 2, 1, SourceGrantFilter{RecipientType: "user", RecipientID: user.PrincipalID})
				if err != nil || count != 1 || len(empty) != 0 {
					t.Fatalf("pagination after membership expansion: %+v %d %v", empty, count, err)
				}
			}
			for _, kind := range kinds {
				found := false
				for _, source := range observation.Sources {
					if source.RecipientType == kind && source.RecipientID > 0 && source.GrantCount > 0 {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing source %s: %+v", kind, observation.Sources)
				}
			}
		}
		t.Run("inspection management boundary and canonical target", func(t *testing.T) {
			user, expiry := newUser(t, time.Hour)
			inspect(t, user, "no_grant")
			for _, target := range []engineplugin.EngineCatalogPath{{}, engineplugin.TabularItemPath(base.Path.EngineID+1, "schema", "public", "wrong_engine"), {EngineID: base.Path.EngineID, Version: base.Path.Version, Segments: base.Path.Segments[:2]}} {
				if result, err := service.InspectSourceGrants(ctx, actor, int64(base.Path.EngineID), user.PrincipalID, target); result != nil || !errors.Is(err, commonapi.ErrBadRequest) {
					t.Fatalf("bad target=%+v %v", result, err)
				}
			}
			if result, err := service.InspectSourceGrants(ctx, actor, int64(base.Path.EngineID), -1, base.Path); result != nil || !errors.Is(err, commonapi.ErrBadRequest) {
				t.Fatalf("bad account=%+v %v", result, err)
			}
			if result, err := service.InspectSourceGrants(ctx, actor, int64(base.Path.EngineID), 9223372036854775807, base.Path); result != nil || !errors.Is(err, commonapi.ErrNotFound) {
				t.Fatalf("unknown account=%+v %v", result, err)
			}
			unqualified := Actor{TenantID: runtime.TenantID, PrincipalID: user.PrincipalID, MembershipID: user.MembershipID, AuthorizationVersion: user.AuthorizationVersion, TokenExpiresAt: expiry}
			if result, err := service.InspectSourceGrants(ctx, unqualified, int64(base.Path.EngineID), commandUser.PrincipalID, base.Path); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("unqualified read=%+v %v", result, err)
			}
			if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: runtime.TenantID, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ScopeType: "tenant", ActorPrincipalID: adminID, Reason: "Read permission without management"}); err != nil {
				t.Fatal(err)
			}
			unqualified.AuthorizationVersion = refresh(t, user).AuthorizationVersion
			if result, err := service.InspectSourceGrants(ctx, unqualified, int64(base.Path.EngineID), commandUser.PrincipalID, base.Path); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("read permission alone=%+v %v", result, err)
			}
			// An existing account outside this tenant remains indistinguishable
			// from an unknown account to this management reader.
			outside := &iam.Principal{PrincipalType: iam.PrincipalTypeUser, Status: iam.PrincipalStatusActive, AuthorizationVersion: 1}
			if err := identity.Transaction(ctx, func(tx *iam.Repository) error {
				if err := tx.CreatePrincipal(ctx, outside); err != nil {
					return err
				}
				return tx.CreateUser(ctx, &iam.User{ID: outside.ID, DisplayName: "Outside selected tenant"})
			}); err != nil {
				t.Fatal(err)
			}
			if result, err := service.InspectSourceGrants(ctx, actor, int64(base.Path.EngineID), outside.ID, base.Path); result != nil || !errors.Is(err, commonapi.ErrNotFound) {
				t.Fatalf("outside account=%+v %v", result, err)
			}
		})
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
					if mutation.table != "system.engines" {
						inspect(t, user, "source_unavailable")
					}
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
			inspect(t, user, "grant", "project_group", "user")
			revoke(t, personal)
			inspect(t, user, "grant", "project_group")
			assert(t, user, "grant") // Another independent Allow remains.
			denyID := deny(t, "user", user.PrincipalID, nil)
			inspect(t, user, "explicit_deny", "project_group")
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
			inspect(t, user, "no_grant")
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
			inspect(t, user, "grant", "user")
			deny(t, "department", child.ID, nil)
			assert(t, user, "explicit_deny")
			if _, err := organizations.DisableDepartment(ctx, iam.ChangeDepartmentStatusInput{TenantID: runtime.TenantID, DepartmentID: child.ID, Version: child.Version, ActorPrincipalID: adminID, Reason: "Disable recipient department"}); err != nil {
				t.Fatal(err)
			}
			assert(t, refresh(t, user), "grant")
			inspect(t, user, "grant", "user")
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
			inspect(t, user, "grant", "user")
			waitUntil(expires)
			assert(t, user, "no_grant")
			inspect(t, user, "no_grant")
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
			if err := tx.Create(&sourceGrant{RequestID: id, ApprovalMode: approvalModeCatalog}).Error; err != nil {
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
			inspect(t, user, "grant", "user")
			if err := db.Table("system.audit_logs").Count(&after).Error; err != nil || before != after {
				t.Fatalf("inspection wrote audit %d -> %d %v", before, after, err)
			}
			deadCtx, stopDead := context.WithCancel(ctx)
			stopDead()
			if result, err := repo.readCurrentSourceRules(deadCtx, request); result != nil || err == nil {
				t.Fatalf("database failure became Allow=%+v %v", result, err)
			}
		})
		t.Run("independent origins use the same rules and withdrawal without receipts", func(t *testing.T) {
			// Storage/consumer contract only: no public independent command or
			// permission is claimed. These facts belong to this disposable fixture.
			for _, kind := range []string{"user", "department", "project_group"} {
				t.Run(kind, func(t *testing.T) {
					user, _ := newUser(t, time.Hour)
					recipientID := user.PrincipalID
					switch kind {
					case "department":
						d, err := organizations.CreateDepartment(ctx, iam.CreateDepartmentInput{TenantID: runtime.TenantID, ActorPrincipalID: adminID, Code: "independent_department", Name: "Independent department"})
						if err != nil {
							t.Fatal(err)
						}
						if _, err := organizations.CreateDepartmentMembership(ctx, iam.CreateDepartmentMembershipInput{TenantID: runtime.TenantID, DepartmentID: d.ID, TenantMembershipID: user.MembershipID, ActorPrincipalID: adminID, MembershipType: "primary", RelationRole: "member"}); err != nil {
							t.Fatal(err)
						}
						recipientID = d.ID
					case "project_group":
						g, err := organizations.CreateProjectGroup(ctx, iam.CreateProjectGroupInput{TenantID: runtime.TenantID, ActorPrincipalID: adminID, Code: "independent_group", Name: "Independent group"})
						if err != nil {
							t.Fatal(err)
						}
						if _, err := organizations.CreateProjectGroupMembership(ctx, iam.CreateProjectGroupMembershipInput{TenantID: runtime.TenantID, ProjectGroupID: g.ID, TenantMembershipID: user.MembershipID, ActorPrincipalID: adminID, RelationRole: "member"}); err != nil {
							t.Fatal(err)
						}
						recipientID = g.ID
					}
					user = refresh(t, user)
					path := engineplugin.TabularItemPath(base.Path.EngineID, "schema", "public", "independent_"+kind)
					encoded, err := shared.EncodeSharingTarget(path)
					if err != nil {
						t.Fatal(err)
					}
					actorType, actorContext := iam.PrincipalTypeUser, iam.ContextTypeTenant
					if err := repo.transaction(ctx, func(tx *Repository) error {
						_, err := tx.changeApprovalRequirement(ctx, approvalRequirementChange{TenantID: runtime.TenantID, Path: path, Mode: approvalModeIndependent, Reason: "Explicit storage fixture arrangement",
							Audit: iam.AuditMetadata{PrincipalID: &actor.PrincipalID, PrincipalType: &actorType, ContextType: &actorContext, TenantID: &runtime.TenantID}}, func(*Repository) error { return nil })
						return err
					}); err != nil {
						t.Fatal(err)
					}
					if result := read(t, user, path); result.Covered {
						t.Fatal("approval configuration alone granted access")
					}
					reason := "Explicit independent storage fixture"
					row := &sourceGrant{RequestID: uuid.New(), ApprovalMode: approvalModeIndependent,
						TenantID: runtime.TenantID, EngineID: int64(path.EngineID), CatalogPath: encoded,
						RecipientType: kind, RecipientID: recipientID, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked,
						RequirementVersion: 1, OperatorPrincipalID: actor.PrincipalID, OperatorMembershipID: actor.MembershipID,
						OperatorAuthorizationVersion: actor.AuthorizationVersion, Reason: &reason}
					bad := *row
					bad.RequirementVersion++
					if err := repo.insertSourceGrant(ctx, &bad); err == nil {
						t.Fatal("mismatched independent requirement accepted")
					}
					if err := repo.insertSourceGrant(ctx, row); err != nil {
						t.Fatal(err)
					}
					observation, err := service.InspectSourceGrants(ctx, actor, int64(path.EngineID), user.PrincipalID, path)
					if err != nil || observation == nil || !observation.RuleCovered || len(observation.Sources) != 1 || observation.Sources[0].RecipientType != kind || observation.Sources[0].RecipientID != recipientID || observation.Sources[0].GrantCount != 1 || observation.Sources[0].ExpiryMode != shared.SharingExpiryUntilRevoked || observation.Sources[0].ExpiresAt != nil {
						t.Fatalf("independent inspection=%+v %v", observation, err)
					}
					var receipts int64
					if err := db.Model(&fulfillmentOutcome{}).Where("request_id = ?", row.RequestID).Count(&receipts).Error; err != nil || receipts != 0 {
						t.Fatalf("independent grant fabricated receipt: %d %v", receipts, err)
					}
					if row.CatalogRequestID != nil || row.GrantedAt.IsZero() {
						t.Fatalf("invalid independent projection: %+v", row)
					}
					if _, err := repo.findFulfillmentGrant(ctx, row.RequestID); !errors.Is(err, gorm.ErrRecordNotFound) {
						t.Fatalf("independent issuance was exposed as Catalog fulfillment history: %v", err)
					}
					if result := read(t, user, path); !result.Covered {
						t.Fatalf("canonical independent Allow missing: %+v", result)
					}
					outsider, _ := newUser(t, time.Hour)
					if read(t, outsider, path).Covered {
						t.Fatal("independent grant leaked to unrelated user")
					}
					revoke(t, row.RequestID)
					if result := read(t, user, path); result.Covered || result.Targets[0].Reason != "no_grant" {
						t.Fatalf("independent withdrawal failed: %+v", result)
					}
				})
			}
		})
		exerciseSourceReadCredentials(t, db, base.Path, newUser, grant, roles, runtime.TenantID, adminID, deny, revoke)
		exerciseManagerProfileAuthorization(t, db, base.Path, newUser, grant, roles, runtime.TenantID, adminID, deny, revoke)
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

// Test-only exact observer compares the unified management list with the shared
// rule predicates; production exposes only the current-list query.
// The selected account is an object of management, never a borrowed caller.
// IAM resolution, membership expansion, rules and sources share one statement.
const sourceGrantInspectionSQL = `WITH input AS MATERIALIZED (
 SELECT m.tenant_id, p.id AS principal_id, m.id AS membership_id,
 p.authorization_version, ?::jsonb AS targets, clock_timestamp() AS observed_at
 FROM system.tenant_memberships m
 JOIN system.principals p ON p.id = m.principal_id AND p.principal_type = 'user'
 JOIN system.users u ON u.id = p.id
 WHERE m.tenant_id = ? AND p.id = ?
)` + sourceReadRuleCTEs + inspectionSourceCTE + `
 SELECT rules.*, COALESCE(s.sources, '[]'::jsonb) AS sources FROM rules
 LEFT JOIN source_sets s USING (position)`

func (s *Service) InspectSourceGrants(ctx context.Context, actor Actor, engineID, accountID int64, path engineplugin.EngineCatalogPath) (*SourceGrantInspection, error) {
	paths, batch, err := encodeSourceReadTargets([]engineplugin.EngineCatalogPath{path})
	if err != nil || accountID <= 0 || int64(path.EngineID) != engineID {
		return nil, commonapi.ErrBadRequest
	}
	var result *SourceGrantInspection
	err = s.withEngineManagementScope(ctx, actor, engineID, authorization.PermissionSystemEngineAccessGrantRead, false,
		func(tx *Repository, check func() error) error {
			var rows []struct {
				Position   int64
				ObservedAt time.Time
				Reason     string
				Sources    json.RawMessage
			}
			if err := tx.db.WithContext(ctx).Raw(sourceGrantInspectionSQL, string(batch), actor.TenantID, accountID).Scan(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 0 {
				return commonapi.ErrNotFound
			}
			observation, err := sourceReadObservation(paths, []sourceReadRuleRow{{Position: rows[0].Position, ObservedAt: rows[0].ObservedAt, Reason: rows[0].Reason}})
			if err != nil || len(rows) != 1 {
				return errSourceReadRules
			}
			result = &SourceGrantInspection{AccountID: accountID, CatalogPath: paths[0], ObservedAt: observation.ObservedAt,
				RuleCovered: observation.Covered, Reason: observation.Targets[0].Reason, Sources: make([]SourceGrantInspectionSource, 0)}
			if err := json.Unmarshal(rows[0].Sources, &result.Sources); err != nil {
				return err
			}
			return check()
		})
	if err != nil {
		return nil, err
	}
	return result, nil
}
