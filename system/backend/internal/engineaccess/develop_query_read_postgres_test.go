package engineaccess

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func exerciseDevelopQueryReadGrants(t *testing.T, db *gorm.DB, path plugin.EngineCatalogPath, tenant, admin int64, roles *iam.TenantRoleService,
	newUser func(*testing.T, time.Duration) (userProvenance, time.Time), grant func(*testing.T, string, int64, *time.Time) uuid.UUID, revoke func(*testing.T, uuid.UUID), actor Actor, credential func(*testing.T, userProvenance) string) {
	t.Run("Notebook ordinary user PostgreSQL grants", func(t *testing.T) {
		ctx := t.Context()
		engine := db.Table("system.engines").Where("id=?", path.EngineID)
		var initialStatus string
		if err := engine.Select("connection_status").Scan(&initialStatus).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.engines").Where("id=?", path.EngineID).Update("connection_status", "online").Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := db.Table("system.engines").Where("id=?", path.EngineID).Update("connection_status", initialStatus).Error; err != nil {
				t.Error(err)
			}
		})
		user, _ := newUser(t, time.Hour)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenant, RoleKey: "custom.notebook_source_read", Name: "Notebook source reader", PermissionKeys: []string{"develop.task.read", "develop.task.execute", "develop.notebook.update", "develop.data_read.execute"}, ActorPrincipalID: admin})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenant, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: admin, Reason: "Notebook source fixture"}); err != nil {
			t.Fatal(err)
		}
		sessions, err := iam.NewNotebookSessionAuthorizationService(iam.NewRepository(db))
		if err != nil {
			t.Fatal(err)
		}
		id := uuid.New()
		trace := "notebook-grants"
		issued, err := sessions.Issue(ctx, iam.IssueNotebookSessionAuthorizationInput{SourceAccessToken: credential(t, user), SessionID: id, TaskID: 41, ExpiresIn: time.Minute, Audit: iam.AuditMetadata{RequestID: &trace}})
		if err != nil {
			t.Fatal(err)
		}
		var serviceID int64
		if err = db.Table("system.service_principals").Select("id").Where("name=?", "addp-develop").Scan(&serviceID).Error; err != nil {
			t.Fatal(err)
		}
		typ, contextType := iam.PrincipalTypeServicePrincipal, iam.ContextTypeTenant
		audit := iam.AuditMetadata{RequestID: &trace, PrincipalID: &serviceID, PrincipalType: &typ, ContextType: &contextType, TenantID: &tenant}
		input := iam.AuthorizeNotebookEngineCatalogInput{SessionID: id, AuthorizationID: issued.ID, ServicePrincipalID: serviceID, ServiceClientID: "addp-develop", TenantID: tenant, Audit: audit}
		session, err := sessions.ReviewNavigation(ctx, input, false)
		if err != nil || session.CanBrowseOtherEngines {
			t.Fatalf("unexpected management access: %+v %v", session, err)
		}
		derived, err := sessions.DeriveExecutionEngineAccess(ctx, iam.DeriveNotebookExecutionEngineAccessInput{SessionID: id, AuthorizationID: issued.ID, EngineID: int64(path.EngineID), ExecutionID: uuid.New(), ServicePrincipalID: serviceID, ServiceClientID: "addp-develop", TenantID: tenant, ExpiresIn: time.Minute, Audit: audit})
		if err != nil {
			t.Fatal(err)
		}
		var version int64
		if err = db.Table("system.engines").Select("version").Where("id=?", path.EngineID).Scan(&version).Error; err != nil {
			t.Fatal(err)
		}
		other := plugin.TabularItemPath(path.EngineID, "schema", "public", "ungranted_notebook")
		svc := NewService(NewRepository(db), nil).WithAuthorizationFactsReader(testMetaFacts{paths: []plugin.EngineCatalogPath{path, other}})
		request := client.DevelopQueryReadRequest{NotebookSessionID: id.String(), AuthorizationID: strconv.FormatInt(derived.AuthorizationID, 10), ExecutionID: derived.ExecutionID.String(), EngineID: strconv.Itoa(int(path.EngineID)), EngineVersion: strconv.FormatInt(version, 10)}
		check := func(want bool, paths ...plugin.EngineCatalogPath) {
			t.Helper()
			request.ReadSet, err = plugin.NewQueryReadSet(paths...)
			if err != nil {
				t.Fatal(err)
			}
			result, err := svc.CheckDevelopQueryRead(ctx, tenant, request)
			if want && (err != nil || result == nil) || !want && (!errors.Is(err, commonapi.ErrForbidden) || result != nil) {
				t.Fatalf("expected source access %t, got %+v %v", want, result, err)
			}
		}
		visibility := func(want bool) {
			t.Helper()
			targets := []plugin.EngineCatalogPath{path, other}
			visible, err := svc.CheckNotebookTableVisibility(ctx, session, targets)
			if err != nil || len(visible) != 2 || visible[0] != want || visible[1] {
				t.Fatal("table visibility", visible, err)
			}
			q := shared.SourceNavigationCheckRequest{Targets: []plugin.EngineCatalogPath{plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), path.EngineID)}}
			v, err := svc.CheckNotebookNavigation(ctx, session, q)
			if err != nil || !v.MatchesNavigation(q) || v.Targets[0].Visible != want {
				t.Fatal("engine visibility", v, err)
			}
		}
		visibility(false)
		check(false, path)
		g := grant(t, "user", user.PrincipalID, nil)
		visibility(true)
		check(true, path)
		check(false, other)
		check(false, path, other)
		request.NotebookSessionID = uuid.NewString()
		check(false, path)
		request.NotebookSessionID = id.String()
		revoke(t, g)
		visibility(false)
		check(false, path)
		t.Run("existing MongoDB reader without data Grant", func(t *testing.T) {
			tenantID := uint(tenant)
			mongo := models.Engine{Name: "Notebook MongoDB access fixture", EngineType: "mongodb", TenantID: &tenantID,
				LifecycleState: "active", ConnectionStatus: "online", ConnectionInfo: models.ConnectionInfo{}, IdentityKey: models.JSONString(`{"host":"notebook.invalid","database":"fixture"}`)}
			if err := db.Table("system.engines").Create(&mongo).Error; err != nil {
				t.Fatal(err)
			}
			accessInput := iam.DeriveNotebookExecutionEngineAccessInput{SessionID: id, AuthorizationID: issued.ID, EngineID: int64(mongo.ID), ExecutionID: uuid.New(), ServicePrincipalID: serviceID, ServiceClientID: "addp-develop", TenantID: tenant, ExpiresIn: time.Minute, Audit: audit}
			if _, err := sessions.DeriveExecutionEngineAccess(ctx, accessInput); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("unexpected ordinary MongoDB derivation: %v", err)
			}
			reader, _ := newUser(t, time.Hour)
			readerRole, err := iam.NewRepository(db).GetActiveBuiltinRoleByKey(ctx, "tenant.engine_management_reader")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenant, MembershipID: reader.MembershipID, RoleIDs: []int64{role.ID, readerRole.ID}, ActorPrincipalID: admin, Reason: "Existing Notebook account"}); err != nil {
				t.Fatal(err)
			}
			readerID := uuid.New()
			original, err := sessions.Issue(ctx, iam.IssueNotebookSessionAuthorizationInput{SourceAccessToken: credential(t, reader), SessionID: readerID, TaskID: 42, ExpiresIn: time.Minute, Audit: iam.AuditMetadata{RequestID: &trace}})
			if err != nil {
				t.Fatal(err)
			}
			input.SessionID, input.AuthorizationID = readerID, original.ID
			if scope, err := sessions.Authorize(ctx, input); err != nil || !scope.CanBrowseOtherEngines {
				t.Fatalf("MongoDB catalog unavailable: %v", err)
			}
			accessInput.SessionID, accessInput.AuthorizationID = readerID, original.ID
			access, err := sessions.DeriveExecutionEngineAccess(ctx, accessInput)
			if err != nil {
				t.Fatal(err)
			}
			executions, err := iam.NewExecutionAuthorizationService(iam.NewRepository(db))
			if err != nil {
				t.Fatal(err)
			}
			_, err = executions.AuthorizeEngineAccess(ctx, iam.AuthorizeExecutionEngineAccessInput{AuthorizationID: access.AuthorizationID, ExecutionID: access.ExecutionID, EngineID: int64(mongo.ID), RequiredEffects: []string{"read"}, ServicePrincipalID: serviceID, ServiceClientID: "addp-develop", TenantID: tenant, Audit: audit})
			if err != nil {
				t.Fatalf("MongoDB read execution access: %v", err)
			}
		})
	})
	t.Run("Develop query current Grants and final Meta recheck", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		user, _ := newUser(t, time.Hour)
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenant, RoleKey: "custom.query_source_read", Name: "Query source read fixture", PermissionKeys: []string{"develop.task.execute", "develop.data_read.execute"}, ActorPrincipalID: admin})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenant, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: admin, Reason: "Query source fixture"}); err != nil {
			t.Fatal(err)
		}
		token := credential(t, user)
		executions, err := iam.NewExecutionAuthorizationService(iam.NewRepository(db))
		if err != nil {
			t.Fatal(err)
		}
		requestID := "develop-query-source"
		issued, err := executions.Issue(ctx, iam.IssueExecutionAuthorizationInput{SourceAccessToken: token, Audience: "develop", ExecutionID: uuid.New(), Accesses: []iam.ExecutionEngineAccessScope{{EngineID: int64(path.EngineID), Effects: []string{"read"}}}, ExpiresIn: time.Minute, Audit: iam.AuditMetadata{RequestID: &requestID}})
		if err != nil {
			t.Fatal(err)
		}
		var version int64
		if err := db.Table("system.engines").Select("version").Where("id=?", path.EngineID).Scan(&version).Error; err != nil {
			t.Fatal(err)
		}
		set, _ := plugin.NewQueryReadSet(path)
		request := client.DevelopQueryReadRequest{AuthorizationID: strconv.FormatInt(issued.ID, 10), ExecutionID: issued.ExecutionID.String(), EngineID: strconv.FormatUint(uint64(path.EngineID), 10), EngineVersion: strconv.FormatInt(version, 10), ReadSet: set, SampleCredential: token}
		metaCalls := 0
		var afterMeta func()
		service := NewService(NewRepository(db), nil).WithAuthorizationFactsReader(bindingFactsReaderFunc(func(_ context.Context, actualTenant uint, _ string) (*authorizationfacts.AuthorizationFacts, error) {
			metaCalls++
			if actualTenant != uint(tenant) {
				t.Fatal("wrong Meta tenant")
			}
			if afterMeta != nil {
				afterMeta()
			}
			facts := testBindingFacts(path)
			return &facts, nil
		}))
		check := func(allowed bool) {
			t.Helper()
			result, err := service.CheckDevelopQueryRead(ctx, tenant, request)
			if allowed {
				if err != nil || result == nil || !result.ExpiresAt.After(result.ObservedAt) {
					t.Fatalf("source observation=%+v err=%v", result, err)
				}
			} else if result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("uncovered source admitted: %+v %v", result, err)
			}
		}
		check(false)
		id := grant(t, "user", user.PrincipalID, nil)
		check(true)
		t.Run("pool edits preserve only valid source versions", func(t *testing.T) {
			engine := db.Table("system.engines").Where("id=?", path.EngineID)
			if err := engine.Updates(map[string]any{"version": version + 2, "pool_only_since_version": version}).Error; err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := engine.Updates(map[string]any{"version": version, "pool_only_since_version": nil}).Error; err != nil {
					t.Error(err)
				}
				request.EngineVersion = strconv.FormatInt(version, 10)
			}()
			for _, v := range []int64{version, version + 1, version + 2, version + 3} {
				request.EngineVersion = strconv.FormatInt(v, 10)
				check(v <= version+2)
			}
			request.EngineVersion = strconv.FormatInt(version, 10)
			if err := engine.Updates(map[string]any{"version": version + 3, "pool_only_since_version": nil}).Error; err != nil {
				t.Fatal(err)
			}
			check(false)
		})
		revoke(t, id)
		check(false)
		id = grant(t, "user", user.PrincipalID, nil)
		afterMeta = func() { revoke(t, id) }
		check(false) // Meta preparation must not preserve the earlier Grant.
		afterMeta = nil
		before := metaCalls
		request.ReadSet, _ = plugin.NewQueryReadSet()
		check(true)
		if metaCalls != before {
			t.Fatal("proven empty source enumerated Meta")
		}
		for _, engineType := range []string{"mysql", "tidb", "oceanbase", "starrocks"} {
			t.Run(engineType+" database paths and current Grants", func(t *testing.T) {
				tenantID := uint(tenant)
				nativeEngine := models.Engine{Name: "Develop " + engineType + " source authorization fixture", EngineType: engineType, TenantID: &tenantID, Version: 1, LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{}, IdentityKey: models.JSONString(`{"host":"develop-` + engineType + `-grant.invalid","database":"fixture"}`)}
				if err := db.WithContext(ctx).Table("system.engines").Create(&nativeEngine).Error; err != nil {
					t.Fatal(err)
				}
				issued, err := executions.Issue(ctx, iam.IssueExecutionAuthorizationInput{SourceAccessToken: token, Audience: "develop", ExecutionID: uuid.New(), Accesses: []iam.ExecutionEngineAccessScope{{EngineID: int64(nativeEngine.ID), Effects: []string{"read"}}}, ExpiresIn: time.Minute, Audit: iam.AuditMetadata{RequestID: &requestID}})
				if err != nil {
					t.Fatal(err)
				}
				first := plugin.TabularItemPath(nativeEngine.ID, "database", "fixture", "authorized")
				second := plugin.TabularItemPath(nativeEngine.ID, "database", "fixture", "ungranted")
				service := NewService(NewRepository(db), nil).WithAuthorizationFactsReader(testMetaFacts{paths: []plugin.EngineCatalogPath{first, second}})
				request := client.DevelopQueryReadRequest{AuthorizationID: strconv.FormatInt(issued.ID, 10), ExecutionID: issued.ExecutionID.String(), EngineID: strconv.FormatUint(uint64(nativeEngine.ID), 10), EngineVersion: strconv.FormatInt(nativeEngine.Version, 10), SampleCredential: token}
				check := func(allowed bool, paths ...plugin.EngineCatalogPath) {
					t.Helper()
					var err error
					request.ReadSet, err = plugin.NewQueryReadSet(paths...)
					if err != nil {
						t.Fatal(err)
					}
					observation, err := service.CheckDevelopQueryRead(ctx, tenant, request)
					if allowed {
						if err != nil || observation == nil || !observation.ExpiresAt.After(observation.ObservedAt) {
							t.Fatalf("qualified observation=%+v err=%v", observation, err)
						}
					} else if observation != nil || !errors.Is(err, commonapi.ErrForbidden) {
						t.Fatalf("uncovered observation=%+v err=%v", observation, err)
					}
				}
				check(true) // A proven empty source still needs the actual engine and EA.
				check(false, first)
				binding := testReadBindings(first)[0]
				input := CreateIndependentGrantInput{Actor: actor, EngineID: int64(nativeEngine.ID), RequestID: uuid.New(), CatalogPath: first, ItemFingerprint: binding.ItemFingerprint, ExpectedBoundaryHash: binding.BoundaryHash, RecipientType: "user", RecipientID: user.PrincipalID, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Native query source fixture"}
				grant, err := prepareIndependentGrant(input)
				if err != nil {
					t.Fatal(err)
				}
				if err := NewRepository(db).insertSourceGrant(ctx, grant); err != nil {
					t.Fatal(err)
				}
				check(true, first)
				check(false, first, second)
				check(false, plugin.TabularItemPath(nativeEngine.ID, "schema", "fixture", "authorized"))
				row := GrantRevocation{RequestID: grant.RequestID, RevokedByPrincipalID: actor.PrincipalID, RevokedByMembershipID: actor.MembershipID, Reason: "Withdraw native source fixture", RevokedRequestIDs: []uuid.UUID{grant.RequestID}}
				if err := db.WithContext(ctx).Create(&row).Error; err != nil {
					t.Fatal(err)
				}
				check(false, first)
				expiry := time.Now().UTC().Add(time.Second)
				input.RequestID, input.ExpiryMode, input.ExpiresAt = uuid.New(), shared.SharingExpiryAtTime, &expiry
				temporary, err := prepareIndependentGrant(input)
				if err != nil {
					t.Fatal(err)
				}
				if err := NewRepository(db).insertSourceGrant(ctx, temporary); err != nil {
					t.Fatal(err)
				}
				check(true, first)
				time.Sleep(time.Until(expiry))
				check(false, first)
			})
		}
	})
}
