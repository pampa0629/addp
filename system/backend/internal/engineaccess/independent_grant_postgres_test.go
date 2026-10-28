package engineaccess

import (
	"context"
	"errors"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
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

func (f independentVerifierFunc) VerifyIndependentReadTarget(c context.Context, id int64, p engineplugin.EngineCatalogPath) (int64, error) {
	return f(c, id, p)
}

// Runs inside the standard disposable PostgreSQL gate. Public commands create
// the approval basis, Grant and revocation; the fake Provider reads no source.
func exerciseIndependentGrantCommands(t *testing.T, db *gorm.DB, tenantID int64, base engineplugin.EngineCatalogPath, roles *iam.TenantRoleService, adminID int64,
	newUser func(*testing.T, time.Duration) (userProvenance, time.Time), seedDelegation func(*testing.T, int64, time.Time) *Delegation) {
	t.Run("independent grant formal commands", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.independent_grants", Name: "Independent grant fixture", PermissionKeys: []string{"system.engine_access_grant.create", "system.engine_access_grant.read", "system.engine_access_grant.revoke"}, ActorPrincipalID: adminID})
		if err != nil {
			t.Fatal(err)
		}
		qualify := func(t *testing.T, delegate bool) Actor {
			user, expires := newUser(t, time.Hour)
			if delegate {
				seedDelegation(t, user.MembershipID, expires.Add(-time.Second))
			}
			if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: adminID, Reason: "Explicit fixture permission"}); err != nil {
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
		reader := testMetaFacts{paths: []engineplugin.EngineCatalogPath{path}}
		for _, name := range []string{"unauthorized_initialization", "atomic_first_authorization", "missing_approval", "filter_%户外", "filter_X户外", "record_states", "concurrent_relation", "legacy_relation"} {
			reader.paths = append(reader.paths, engineplugin.TabularItemPath(base.EngineID, "schema", "public", name))
		}
		// Source inspection must not hold any IAM lock. NOWAIT proves that an
		// independent connection can lock the operator during Provider IO.
		calls := 0
		service := NewService(NewRepository(db), func(*models.Engine) bool { return true }).WithAuthorizationFactsReader(reader).WithIndependentTargetVerifier(independentVerifierFunc(func(c context.Context, tenant int64, p engineplugin.EngineCatalogPath) (int64, error) {
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
		basis, err := installFixturePermit(ctx, NewRepository(db), tenantID, adminID, path)
		if err != nil {
			t.Fatal(err)
		}
		receiver, _ := newUser(t, time.Hour)
		input := CreateIndependentGrantInput{ItemFingerprint: factstest.Binding(path).ItemFingerprint, ExpectedBoundaryHash: factstest.Binding(path).BoundaryHash, Actor: actor, EngineID: int64(base.EngineID), RequestID: uuid.New(), CatalogPath: path, DirectPermissionVersion: basis.Version, DirectPermissionSource: "explicit", RecipientType: "user", RecipientID: receiver.PrincipalID, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Explicit standalone read access"}
		t.Run("personal department and project grants withdraw independently", func(t *testing.T) {
			user, _ := newUser(t, time.Hour)
			organizations := iam.NewOrganizationService(iam.NewRepository(db), testDatabaseClock(t, db))
			department, err := organizations.CreateDepartment(ctx, iam.CreateDepartmentInput{TenantID: tenantID, ActorPrincipalID: adminID, Code: "combined_sources_department", Name: "Combined sources department"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := organizations.CreateDepartmentMembership(ctx, iam.CreateDepartmentMembershipInput{TenantID: tenantID, DepartmentID: department.ID, TenantMembershipID: user.MembershipID, ActorPrincipalID: adminID, MembershipType: "primary", RelationRole: "member"}); err != nil {
				t.Fatal(err)
			}
			group, err := organizations.CreateProjectGroup(ctx, iam.CreateProjectGroupInput{TenantID: tenantID, ActorPrincipalID: adminID, Code: "combined_sources_group", Name: "Combined sources group"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := organizations.CreateProjectGroupMembership(ctx, iam.CreateProjectGroupMembershipInput{TenantID: tenantID, ProjectGroupID: group.ID, TenantMembershipID: user.MembershipID, ActorPrincipalID: adminID, RelationRole: "member"}); err != nil {
				t.Fatal(err)
			}
			principal, err := iam.NewRepository(db).GetPrincipal(ctx, user.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			user.AuthorizationVersion = principal.AuthorizationVersion
			ids := make([]uuid.UUID, 0, 3)
			for _, recipient := range []struct {
				kind string
				id   int64
			}{{"user", user.PrincipalID}, {"department", department.ID}, {"project_group", group.ID}} {
				command := input
				command.RequestID, command.RecipientType, command.RecipientID = uuid.New(), recipient.kind, recipient.id
				if _, err := service.CreateIndependentGrant(ctx, command); err != nil {
					t.Fatal(err)
				}
				ids = append(ids, command.RequestID)
			}
			for remaining := 3; remaining >= 0; remaining-- {
				observation, err := service.InspectSourceGrants(ctx, actor, int64(base.EngineID), user.PrincipalID, path)
				if err != nil || observation == nil || len(observation.Sources) != remaining || observation.RuleCovered != (remaining > 0) {
					t.Fatalf("remaining=%d inspection=%+v err=%v", remaining, observation, err)
				}
				rules, err := NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{TenantID: tenantID, Source: user, Targets: []engineplugin.EngineCatalogPath{path}, Bindings: testReadBindings(path)})
				if err != nil || rules == nil || rules.Covered != (remaining > 0) {
					t.Fatalf("remaining=%d actual rules=%+v err=%v", remaining, rules, err)
				}
				if remaining > 0 {
					if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(base.EngineID), RequestID: ids[3-remaining], Reason: "Withdraw only this source"}); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
		t.Run("administrator handles without granting itself access", func(t *testing.T) {
			administrator := qualify(t, false)
			adminRole, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.engine_grant_administrator", Name: "Authorization administrator fixture", PermissionKeys: []string{"system.engine_access_delegation.create"}, ActorPrincipalID: adminID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: administrator.MembershipID, RoleIDs: []int64{adminRole.ID}, ActorPrincipalID: adminID, Reason: "Explicit administrator qualification"}); err != nil {
				t.Fatal(err)
			}
			p, err := iam.NewRepository(db).GetPrincipal(ctx, administrator.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			administrator.AuthorizationVersion = p.AuthorizationVersion
			copy := input
			copy.Actor, copy.RequestID = administrator, uuid.New()
			if _, err := service.CreateIndependentGrant(ctx, copy); err != nil {
				t.Fatalf("administrator requires self-delegation: %v", err)
			}
			var delegations int64
			if err := db.Model(&Delegation{}).Where("tenant_membership_id = ?", administrator.MembershipID).Count(&delegations).Error; err != nil || delegations != 0 {
				t.Fatalf("manufactured self-delegation: %d %v", delegations, err)
			}
			result, err := NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{TenantID: tenantID, Source: userProvenance{PrincipalID: administrator.PrincipalID, MembershipID: administrator.MembershipID, AuthorizationVersion: administrator.AuthorizationVersion}, Targets: []engineplugin.EngineCatalogPath{path}, Bindings: testReadBindings(path)})
			if err != nil || result == nil || result.Targets[0].Reason != "no_grant" {
				t.Fatalf("administrator automatically gained read access: %+v %v", result, err)
			}
			// Leave the outer read-before-issuance assertion independent of this fixture.
			if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: administrator, EngineID: copy.EngineID, RequestID: copy.RequestID, Reason: "Fixture completed"}); err != nil {
				t.Fatal(err)
			}
		})
		calls = 0
		assertRead := func(want string) {
			t.Helper()
			result, err := NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{TenantID: tenantID, Source: receiver, Targets: []engineplugin.EngineCatalogPath{path}, Bindings: testReadBindings(path)})
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
		t.Run("Grant cannot create its own permit", func(t *testing.T) {
			first := input
			first.RequestID = uuid.New()
			first.CatalogPath = engineplugin.TabularItemPath(base.EngineID, "schema", "public", "unauthorized_initialization")
			first.ItemFingerprint, first.ExpectedBoundaryHash = testReadBindings(first.CatalogPath)[0].ItemFingerprint, testReadBindings(first.CatalogPath)[0].BoundaryHash
			if _, err := service.CreateIndependentGrant(ctx, first); !errors.Is(err, ErrIndependentGrantBasis) {
				t.Fatal(err)
			}
			if _, err := NewRepository(db).directPermission(ctx, tenantID, first.EngineID, first.ItemFingerprint); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("Grant created permit: %v", err)
			}
		})
		for _, mutate := range []func(*CreateIndependentGrantInput){func(i *CreateIndependentGrantInput) { i.DirectPermissionVersion++ }, func(i *CreateIndependentGrantInput) {
			i.CatalogPath = engineplugin.TabularItemPath(base.EngineID, "schema", "public", "missing_approval")
			i.ItemFingerprint, i.ExpectedBoundaryHash = testReadBindings(i.CatalogPath)[0].ItemFingerprint, testReadBindings(i.CatalogPath)[0].BoundaryHash
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
		t.Run("another collection cannot reuse table approval", func(t *testing.T) {
			collection := engineplugin.EngineCatalogPath{Version: engineplugin.EngineCatalogPathVersion, EngineID: base.EngineID,
				Segments: []engineplugin.EngineCatalogSegment{{Term: "server", Kind: "server"}, {Term: "database", Kind: "namespace", Name: "Outdoor"}, {Term: "collection", Kind: "collection", Name: "Persons"}}}
			command := input
			command.RequestID, command.CatalogPath = uuid.New(), collection
			if _, err := service.CreateIndependentGrant(ctx, command); !errors.Is(err, commonapi.ErrBadRequest) {
				t.Fatalf("collection reused unrelated table approval: %v", err)
			}
		})
		t.Run("database single tables and collections bind formal grants", func(t *testing.T) {
			for _, target := range []engineplugin.EngineCatalogPath{
				engineplugin.TabularItemPath(base.EngineID, "database", "business", "customers"),
				engineplugin.EngineCatalogBranchLeafPath(engineplugin.DynamicSchemaCatalogModel(), base.EngineID, "database", "Outdoor", "collection", "collection", "Persons"),
			} {
				binding := factstest.Binding(target)
				typedService := NewService(NewRepository(db), func(*models.Engine) bool { return true }).WithAuthorizationFactsReader(testMetaFacts{paths: []engineplugin.EngineCatalogPath{target}}).WithIndependentTargetVerifier(service.independentTarget)
				command := input
				command.RequestID, command.CatalogPath = uuid.New(), target
				command.ItemFingerprint, command.ExpectedBoundaryHash = binding.ItemFingerprint, binding.BoundaryHash
				if _, err := installFixturePermit(ctx, NewRepository(db), tenantID, adminID, target); err != nil {
					t.Fatal(err)
				}
				command.DirectPermissionVersion = 1
				row, err := typedService.CreateIndependentGrant(ctx, command)
				if err != nil || row == nil || row.ItemFingerprint != binding.ItemFingerprint || row.BoundaryHash != binding.BoundaryHash {
					t.Fatalf("typed grant=%+v err=%v", row, err)
				}
				result, err := NewRepository(db).readCurrentSourceRules(ctx, sourceReadRequest{TenantID: tenantID, Source: receiver, Targets: []engineplugin.EngineCatalogPath{target}, Bindings: testReadBindings(target)})
				if err != nil || result == nil || result.Targets[0].Reason != "grant" {
					t.Fatalf("typed read denied after exact grant: %+v %v", result, err)
				}
				changed := command
				changed.RequestID = uuid.New()
				changed.CatalogPath.Segments = append([]engineplugin.EngineCatalogSegment(nil), target.Segments...)
				changed.CatalogPath.Segments[2].Name = "unapproved"
				if _, err := typedService.CreateIndependentGrant(ctx, changed); !errors.Is(err, commonapi.ErrBadRequest) {
					t.Fatalf("changed target reused approved binding: %v", err)
				}
			}
		})
		t.Run("table and recipient filters precede pagination and preserve history", func(t *testing.T) {
			other, _ := newUser(t, time.Hour)
			commands := []CreateIndependentGrantInput{}
			for _, table := range []string{"filter_%户外", "filter_X户外"} {
				command := input
				command.RequestID, command.CatalogPath = uuid.New(), engineplugin.TabularItemPath(base.EngineID, "schema", "public", table)
				command.ItemFingerprint, command.ExpectedBoundaryHash = testReadBindings(command.CatalogPath)[0].ItemFingerprint, testReadBindings(command.CatalogPath)[0].BoundaryHash
				command.DirectPermissionVersion = 1
				if _, err := installFixturePermit(ctx, NewRepository(db), tenantID, adminID, command.CatalogPath); err != nil {
					t.Fatal(err)
				}
				if _, err := service.CreateIndependentGrant(ctx, command); err != nil {
					t.Fatal(err)
				}
				commands = append(commands, command)
			}
			command := commands[0]
			command.RequestID, command.RecipientID = uuid.New(), other.PrincipalID
			if _, err := service.CreateIndependentGrant(ctx, command); err != nil {
				t.Fatal(err)
			}
			commands = append(commands, command)
			organizations := iam.NewOrganizationService(iam.NewRepository(db), nil)
			department, err := organizations.CreateDepartment(ctx, iam.CreateDepartmentInput{TenantID: tenantID, ActorPrincipalID: adminID, Code: "filter_department", Name: "Filter department"})
			if err != nil {
				t.Fatal(err)
			}
			group, err := organizations.CreateProjectGroup(ctx, iam.CreateProjectGroupInput{TenantID: tenantID, ActorPrincipalID: adminID, Code: "filter_group", Name: "Filter group"})
			if err != nil {
				t.Fatal(err)
			}
			for kind, id := range map[string]int64{"department": department.ID, "project_group": group.ID} {
				organization := commands[0]
				organization.RequestID, organization.RecipientType, organization.RecipientID = uuid.New(), kind, id
				if _, err := service.CreateIndependentGrant(ctx, organization); err != nil {
					t.Fatal(err)
				}
				commands = append(commands, organization)
			}
			for _, test := range []struct {
				filter SourceGrantFilter
				count  int64
			}{
				{SourceGrantFilter{TableSearch: "FILTER_"}, 5},
				{SourceGrantFilter{CatalogPath: &commands[0].CatalogPath}, 4},
				{SourceGrantFilter{CatalogPath: &commands[1].CatalogPath}, 1},
				{SourceGrantFilter{CatalogPath: &commands[0].CatalogPath, RecipientType: "user", RecipientID: receiver.PrincipalID}, 1},
				{SourceGrantFilter{CatalogPath: &commands[0].CatalogPath, RecipientType: "department", RecipientID: department.ID}, 1},
				{SourceGrantFilter{TableSearch: "%户外"}, 4},
				{SourceGrantFilter{TableSearch: "filter_", RecipientType: "user", RecipientID: receiver.PrincipalID}, 2},
				{SourceGrantFilter{TableSearch: "%户外", RecipientType: "user", RecipientID: receiver.PrincipalID}, 1},
				{SourceGrantFilter{RecipientType: "user", RecipientID: other.PrincipalID}, 1},
				{SourceGrantFilter{TableSearch: "filter_", RecipientType: "user"}, 3},
				{SourceGrantFilter{TableSearch: "%户外", RecipientType: "department", RecipientID: department.ID}, 1},
				{SourceGrantFilter{TableSearch: "filter_", RecipientType: "department"}, 1},
				{SourceGrantFilter{TableSearch: "%户外", RecipientType: "project_group", RecipientID: group.ID}, 1},
				{SourceGrantFilter{TableSearch: "filter_", RecipientType: "project_group"}, 1},
				{SourceGrantFilter{TableSearch: "filter_X", RecipientType: "department", RecipientID: department.ID}, 0},
				{SourceGrantFilter{TableSearch: "not_present"}, 0},
			} {
				for page := 1; page <= int(test.count)+1; page++ {
					current, total, err := service.ListSourceGrantRelations(ctx, actor, input.EngineID, page, 1, test.filter)
					history, historicalTotal, historyErr := service.ListSourceGrants(ctx, actor, input.EngineID, page, 1, test.filter)
					wantLength := 0
					if int64(page) <= test.count {
						wantLength = 1
					}
					if err != nil || historyErr != nil || total != test.count || historicalTotal != test.count || len(current) != wantLength || len(history) != wantLength {
						t.Fatalf("filter=%+v page=%d current=%+v total=%d history=%+v total=%d errors=%v/%v", test.filter, page, current, total, history, historicalTotal, err, historyErr)
					}
				}
			}
			for _, command := range commands {
				if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: input.EngineID, RequestID: command.RequestID, Reason: "Filter fixture finished"}); err != nil {
					t.Fatal(err)
				}
			}
			filter := SourceGrantFilter{CatalogPath: &commands[0].CatalogPath, RecipientType: "user", RecipientID: receiver.PrincipalID}
			current, total, err := service.ListSourceGrantRelations(ctx, actor, input.EngineID, 1, 1, filter)
			history, historicalTotal, historyErr := service.ListSourceGrants(ctx, actor, input.EngineID, 1, 1, filter)
			if err != nil || historyErr != nil || total != 0 || len(current) != 0 || historicalTotal != 1 || len(history) != 1 || history[0].Revocation == nil {
				t.Fatalf("withdrawn filtered relation did not preserve history: %v %v %d/%d %+v", err, historyErr, total, historicalTotal, history)
			}
			for kind, id := range map[string]int64{"department": department.ID, "project_group": group.ID} {
				filter := SourceGrantFilter{TableSearch: "filter_", RecipientType: kind, RecipientID: id}
				current, total, err := service.ListSourceGrantRelations(ctx, actor, input.EngineID, 1, 1, filter)
				history, historicalTotal, historyErr := service.ListSourceGrants(ctx, actor, input.EngineID, 1, 1, filter)
				if err != nil || historyErr != nil || total != 0 || len(current) != 0 || historicalTotal != 1 || len(history) != 1 || history[0].Revocation == nil {
					t.Fatalf("withdrawn %s filter: current=%d history=%d %v/%v", kind, total, historicalTotal, err, historyErr)
				}
			}
		})
		t.Run("record states combine with exact target before pagination and revocation precedes expiry", func(t *testing.T) {
			command := input
			command.RequestID, command.CatalogPath = uuid.New(), engineplugin.TabularItemPath(base.EngineID, "schema", "public", "record_states")
			command.ItemFingerprint, command.ExpectedBoundaryHash = testReadBindings(command.CatalogPath)[0].ItemFingerprint, testReadBindings(command.CatalogPath)[0].BoundaryHash
			command.DirectPermissionVersion = 1
			if _, err := installFixturePermit(ctx, NewRepository(db), tenantID, adminID, command.CatalogPath); err != nil {
				t.Fatal(err)
			}
			// Issuance and state filtering use the database wall clock. Keep the
			// fixture's expiry and wait on that same clock, including VM drift.
			expires := testDatabaseClock(t, db)().Add(5 * time.Second)
			command.ExpiryMode, command.ExpiresAt = shared.SharingExpiryAtTime, &expires
			if _, err := service.CreateIndependentGrant(ctx, command); err != nil {
				t.Fatal(err)
			}
			other, _ := newUser(t, time.Hour)
			revoked := command
			revoked.RequestID, revoked.RecipientID = uuid.New(), other.PrincipalID
			if _, err := service.CreateIndependentGrant(ctx, revoked); err != nil {
				t.Fatal(err)
			}
			if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: command.EngineID, RequestID: revoked.RequestID, Reason: "State fixture"}); err != nil {
				t.Fatal(err)
			}
			check := func(state string, recipient int64, count int64) {
				t.Helper()
				filter := SourceGrantFilter{CatalogPath: &command.CatalogPath, RecipientType: "user", RecipientID: recipient, State: state}
				for page := 1; page <= int(count)+1; page++ {
					rows, total, err := service.ListSourceGrants(ctx, actor, command.EngineID, page, 1, filter)
					want := 0
					if int64(page) <= count {
						want = 1
					}
					if err != nil || total != count || len(rows) != want {
						t.Fatalf("state=%s page=%d total=%d rows=%+v err=%v", state, page, total, rows, err)
					}
				}
			}
			check("active", receiver.PrincipalID, 1)
			check("revoked", other.PrincipalID, 1)
			check("expired", receiver.PrincipalID, 0)
			if err := db.WithContext(ctx).Exec("SELECT pg_sleep(GREATEST(0, EXTRACT(EPOCH FROM (?::timestamptz-clock_timestamp())))+0.05)", expires).Error; err != nil {
				t.Fatal(err)
			}
			check("active", receiver.PrincipalID, 0)
			check("expired", receiver.PrincipalID, 1)
			check("expired", other.PrincipalID, 0)
			check("revoked", other.PrincipalID, 1)
			check("", 0, 2)
			if _, _, err := service.ListSourceGrantRelations(ctx, actor, command.EngineID, 1, 1, SourceGrantFilter{State: "active"}); err == nil {
				t.Fatal("current relations accepted a record state")
			}
		})
		t.Run("relation rejects another command even with changed validity", func(t *testing.T) {
			duplicate := input
			duplicate.RequestID = uuid.New()
			if _, err := service.CreateIndependentGrant(ctx, duplicate); !errors.Is(err, ErrGrantRelationExists) {
				t.Fatalf("duplicate=%v", err)
			}
			expires := time.Now().Add(time.Hour)
			duplicate.RequestID, duplicate.ExpiryMode, duplicate.ExpiresAt = uuid.New(), shared.SharingExpiryAtTime, &expires
			if _, err := service.CreateIndependentGrant(ctx, duplicate); !errors.Is(err, ErrGrantRelationExists) {
				t.Fatalf("silent validity change=%v", err)
			}
			relations, _, err := service.ListSourceGrantRelations(ctx, actor, input.EngineID, 1, 100, SourceGrantFilter{})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, relation := range relations {
				if relation.RequestID != nil && *relation.RequestID == input.RequestID {
					found = relation.GrantCount == 1
				}
			}
			if !found {
				t.Fatalf("current relation=%+v", relations)
			}
		})
		t.Run("concurrent new commands issue only one relation", func(t *testing.T) {
			path := engineplugin.TabularItemPath(base.EngineID, "schema", "public", "concurrent_relation")
			basis, err := installFixturePermit(ctx, NewRepository(db), tenantID, adminID, path)
			if err != nil {
				t.Fatal(err)
			}
			concurrent := NewService(NewRepository(db), func(*models.Engine) bool { return true }).WithAuthorizationFactsReader(reader).WithIndependentTargetVerifier(independentVerifierFunc(func(c context.Context, tenant int64, p engineplugin.EngineCatalogPath) (int64, error) {
				e, err := NewRepository(db).engine(c, tenant, int64(p.EngineID), false)
				if err != nil {
					return 0, err
				}
				return e.Version, nil
			}))
			type outcome struct {
				row *SourceGrantView
				err error
			}
			results := make(chan outcome, 2)
			for i := 0; i < 2; i++ {
				command := input
				command.RequestID, command.CatalogPath, command.DirectPermissionVersion = uuid.New(), path, basis.Version
				command.ItemFingerprint, command.ExpectedBoundaryHash = testReadBindings(path)[0].ItemFingerprint, testReadBindings(path)[0].BoundaryHash
				go func() { row, err := concurrent.CreateIndependentGrant(ctx, command); results <- outcome{row, err} }()
			}
			var issued *SourceGrantView
			duplicates := 0
			var failures []error
			issuedCount := 0
			for i := 0; i < 2; i++ {
				result := <-results
				if result.err == nil {
					issuedCount++
					issued = result.row
				} else if errors.Is(result.err, ErrGrantRelationExists) {
					duplicates++
				} else {
					failures = append(failures, result.err)
				}
			}
			if issued == nil || issuedCount != 1 || duplicates != 1 || len(failures) != 0 {
				t.Fatalf("concurrency did not converge: issued=%d duplicate=%d errors=%v", issuedCount, duplicates, failures)
			}
			if _, err := concurrent.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: input.EngineID, RequestID: issued.RequestID, Reason: "Close concurrency fixture"}); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("legacy duplicates withdraw together and old retries leave new grants intact", func(t *testing.T) {
			original := input
			original.CatalogPath = engineplugin.TabularItemPath(base.EngineID, "schema", "public", "legacy_relation")
			original.ItemFingerprint, original.ExpectedBoundaryHash = testReadBindings(original.CatalogPath)[0].ItemFingerprint, testReadBindings(original.CatalogPath)[0].BoundaryHash
			original.RequestID, original.DirectPermissionVersion = uuid.New(), 1
			if _, err := installFixturePermit(ctx, NewRepository(db), tenantID, adminID, original.CatalogPath); err != nil {
				t.Fatal(err)
			}
			first, err := service.CreateIndependentGrant(ctx, original)
			if err != nil {
				t.Fatal(err)
			}
			legacy := original
			legacy.RequestID = uuid.New()
			row, err := prepareIndependentGrant(legacy)
			if err != nil {
				t.Fatal(err)
			}
			// Simulate a pre-202 duplicate only in the standard isolated fixture.
			// The production command must reject it; never disable immutable guards.
			if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec("ALTER TABLE system.engine_access_grants DISABLE TRIGGER trg_source_grant_relation").Error; err != nil {
					return err
				}
				if err := NewRepository(tx).insertSourceGrant(ctx, row); err != nil {
					return err
				}
				return restoreGrantRelationFixtureTrigger(tx)
			}); err != nil {
				t.Fatal(err)
			}
			relations, _, err := service.ListSourceGrantRelations(ctx, actor, input.EngineID, 1, 100, SourceGrantFilter{})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, relation := range relations {
				if relation.RequestID != nil && *relation.RequestID == row.RequestID {
					found = relation.GrantCount == 2
				}
			}
			if !found {
				t.Fatalf("legacy grouping=%+v", relations)
			}
			filter := SourceGrantFilter{TableSearch: "legacy_relation", RecipientType: "user", RecipientID: receiver.PrincipalID}
			filtered, relationTotal, err := service.ListSourceGrantRelations(ctx, actor, input.EngineID, 1, 1, filter)
			history, historyTotal, historyErr := service.ListSourceGrants(ctx, actor, input.EngineID, 2, 1, filter)
			if err != nil || historyErr != nil || relationTotal != 1 || len(filtered) != 1 || filtered[0].GrantCount != 2 || historyTotal != 2 || len(history) != 1 {
				t.Fatalf("filtered legacy grouping/history: %+v %d/%d %v/%v", filtered, relationTotal, historyTotal, err, historyErr)
			}
			withdraw := RevokeGrantInput{Actor: actor, EngineID: input.EngineID, RequestID: first.RequestID, Reason: "Withdraw complete legacy relation"}
			result, err := service.RevokeGrant(ctx, withdraw)
			if err != nil || len(result.RevokedRequestIDs) != 2 {
				t.Fatalf("withdraw=%+v %v", result, err)
			}
			for _, id := range []uuid.UUID{first.RequestID, row.RequestID} {
				if _, err := service.repository.findGrantRevocation(ctx, id); err != nil {
					t.Fatal(err)
				}
			}
			fresh := legacy
			fresh.RequestID = uuid.New()
			if _, err := service.CreateIndependentGrant(ctx, fresh); err != nil {
				t.Fatal(err)
			}
			if retry, err := service.RevokeGrant(ctx, withdraw); err != nil || !retry.RevokedAt.Equal(result.RevokedAt) {
				t.Fatalf("old retry=%+v %v", retry, err)
			}
			if _, err := service.repository.findGrantRevocation(ctx, fresh.RequestID); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("old retry touched new grant: %v", err)
			}
			current, _, err := service.ListSourceGrantRelations(ctx, actor, input.EngineID, 1, 100, SourceGrantFilter{})
			if err != nil {
				t.Fatal(err)
			}
			found = false
			for _, relation := range current {
				if relation.RequestID != nil && *relation.RequestID == fresh.RequestID {
					found = relation.GrantCount == 1
				}
			}
			if !found {
				t.Fatalf("new relation=%+v", current)
			}
			withdraw.RequestID = fresh.RequestID
			if _, err := service.RevokeGrant(ctx, withdraw); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("permit changes preserve issued read access", func(t *testing.T) {
			version := basis.Version
			for _, allowed := range []bool{false, true} {
				command := fixturePermitInput(ctx, NewRepository(db), tenantID, adminID, path, allowed, version)
				err := NewRepository(db).transaction(ctx, func(tx *Repository) error {
					row, err := fixtureSetPermit(ctx, tx, command, func(*Repository) error { return nil })
					if err == nil {
						version = row.Version
					}
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				assertRead("grant")
			}
		})

		t.Run("deployment default issuance and explicit override", func(t *testing.T) {
			version := fixtureDisabledCatalogDeclaration(t, ctx, db)
			target := engineplugin.TabularItemPath(base.EngineID, "schema", "public", "deployment_default")
			binding := factstest.Binding(target)
			repo := NewRepository(db)
			svc := NewService(repo, func(*models.Engine) bool { return true }).WithAuthorizationFactsReader(testMetaFacts{paths: []engineplugin.EngineCatalogPath{target}}).WithIndependentTargetVerifier(service.independentTarget)
			command := input
			command.RequestID = uuid.New()
			command.CatalogPath = target
			command.ItemFingerprint = binding.ItemFingerprint
			command.ExpectedBoundaryHash = binding.BoundaryHash
			command.DirectPermissionSource = "deployment_default"
			command.DirectPermissionVersion = version
			t.Cleanup(func() {
				cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
				defer done()
				if _, err := repo.findSourceGrant(cleanup, command.RequestID); errors.Is(err, gorm.ErrRecordNotFound) {
					return
				} else if err != nil {
					t.Error(err)
					return
				}
				if _, err := svc.RevokeGrant(cleanup, RevokeGrantInput{Actor: actor, EngineID: command.EngineID, RequestID: command.RequestID, Reason: "Complete isolated deployment-default verification"}); err != nil {
					t.Error(err)
				}
			})
			granted, err := svc.CreateIndependentGrant(ctx, command)
			if err != nil || granted == nil || granted.DirectPermissionSource != "deployment_default" {
				t.Fatal("disabled declaration did not allow direct issuance", err)
			}
			var settings int64
			if err := db.Table("system.engine_access_direct_permissions").Where("item_fingerprint=?", binding.ItemFingerprint).Count(&settings).Error; err != nil || settings != 0 {
				t.Fatal("default materialized an implicit setting", settings, err)
			}
			if err := db.Exec(`UPDATE system.module_definitions SET enabled=true,version=version+1 WHERE module_name='catalog'`).Error; err != nil {
				t.Fatal(err)
			}
			fresh := command
			fresh.RequestID = uuid.New()
			if result, err := svc.CreateIndependentGrant(ctx, fresh); result != nil || !errors.Is(err, ErrIndependentGrantBasis) {
				t.Fatal("enabled Catalog allowed a stale default", err)
			}
			read, err := repo.readCurrentSourceRules(ctx, sourceReadRequest{TenantID: tenantID, Source: receiver, Targets: []engineplugin.EngineCatalogPath{target}, Bindings: testReadBindings(target)})
			if err != nil || read == nil || !read.Covered {
				t.Fatal("deployment change revoked an existing Grant", err)
			}
			replay, err := svc.CreateIndependentGrant(ctx, command)
			if err != nil || replay == nil || !replay.GrantedAt.Equal(granted.GrantedAt) {
				t.Fatal("deployment change rewrote issuance history", err)
			}
			// Explicit denial dominates a disabled declaration, without creating
			// an implicit grant or preventing business-approved issuance.
			if err := db.Exec(`UPDATE system.module_definitions SET enabled=false,version=version+1 WHERE module_name='catalog'`).Error; err != nil {
				t.Fatal(err)
			}
			permitInput := fixturePermitInput(ctx, repo, tenantID, adminID, target, false, 0)
			if err := repo.transaction(ctx, func(tx *Repository) error {
				_, err := fixtureSetPermit(ctx, tx, permitInput, func(*Repository) error { return nil })
				return err
			}); err != nil {
				t.Fatal(err)
			}
			fresh.DirectPermissionSource = "explicit"
			fresh.DirectPermissionVersion = 1
			if result, err := svc.CreateIndependentGrant(ctx, fresh); result != nil || !errors.Is(err, ErrIndependentGrantBasis) {
				t.Fatal("deployment default overrode explicit deny", err)
			}
		})

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
		rows, total, err := service.ListSourceGrants(ctx, actor, input.EngineID, 1, 100, SourceGrantFilter{})
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
		exerciseDirectPermissionIssuanceOrdering(t, db, actor, receiver, input, adminID)
	})
}
