package engineaccess

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	engineplugin "github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	systemauthorization "github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Runs inside the existing formal Grant fixture and IAM PostgreSQL owner gate.
// Real browser sessions use production IAM services; no source DB is accessed.
func exerciseSourceReadCredentials(t *testing.T, db *gorm.DB, path engineplugin.EngineCatalogPath,
	newUser func(*testing.T, time.Duration) (userProvenance, time.Time),
	grant func(*testing.T, string, int64, *time.Time) uuid.UUID,
	roles *iam.TenantRoleService, tenantID, adminID int64,
	revoke func(*testing.T, uuid.UUID),
) {
	t.Run("trusted current User credential and precise rules", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		identity := iam.NewRepository(db)
		repo := NewRepository(db)
		issue := func(t *testing.T, user userProvenance, ttl time.Duration) (*iam.IssuedBrowserSession, *iam.TokenFamilyService) {
			t.Helper()
			tokens, err := iam.NewTokenFamilyService(identity, iam.BrowserSessionConfig{AccessTokenTTL: ttl, ResourceTicketOwners: []string{"manager"}}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			selection, err := iam.NewContextSelectionService(identity, tokens)
			if err != nil {
				t.Fatal(err)
			}
			result, err := selection.BeginContextSelection(ctx, iam.BeginContextSelectionInput{PrincipalID: user.PrincipalID,
				Authentication: iam.SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: iam.AssuranceLevelAAL1, AuthenticatedAt: time.Now().UTC().Add(-time.Second)}})
			if err != nil {
				t.Fatalf("issue current User session: %v", err)
			}
			session := result.Session
			if result.Challenge != nil {
				session, err = selection.ConsumeContextSelection(ctx, iam.ConsumeContextSelectionInput{SelectionTicket: result.Challenge.SelectionTicket,
					Choice: iam.ContextSelectionChoice{Type: iam.ContextTypeTenant, TenantMembershipID: &user.MembershipID}})
				if err != nil {
					t.Fatal(err)
				}
			}
			if session == nil {
				t.Fatal("expected real Tenant User session")
			}
			return session, tokens
		}

		t.Run("Develop workflow result current User grants and withdrawal", func(t *testing.T) {
			service := NewService(repo, nil).WithAuthorizationFactsReader(testMetaFacts{paths: []engineplugin.EngineCatalogPath{path}})
			user, _ := newUser(t, time.Hour)
			role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.workflow_result", Name: "Workflow result fixture", PermissionKeys: []string{"develop.task.read", "develop.data_read.execute"}, ActorPrincipalID: adminID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: adminID, Reason: "Workflow result fixture"}); err != nil {
				t.Fatal(err)
			}
			session, _ := issue(t, user, time.Minute)
			request := SourceReadCheckRequest{Targets: []engineplugin.EngineCatalogPath{path}}
			check := func(want error) {
				t.Helper()
				result, err := service.CheckDevelopWorkflowResultRead(authenticatedManagerReadRequest(ctx, service, session.AccessToken), session.AccessToken, request)
				if want == nil {
					if err != nil || result == nil {
						t.Fatal(result, err)
					}
				} else if result != nil || !errors.Is(err, want) {
					t.Fatal(result, err, want)
				}
			}
			check(commonapi.ErrForbidden)
			id := grant(t, "user", user.PrincipalID, nil)
			check(nil)
			revoke(t, id)
			check(commonapi.ErrForbidden)
		})
		t.Run("refresh scope uses current function precise grants and withdrawal", func(t *testing.T) {
			user, _ := newUser(t, time.Hour)
			query := shared.MetadataRefreshQuery{Kind: "node", Target: engineplugin.TabularNamespacePath(path.EngineID, "schema", path.Segments[1].Name)}
			sourceID := grant(t, "user", user.PrincipalID, nil)
			service := NewService(repo, nil).WithAuthorizationFactsReader(testMetaFacts{paths: []engineplugin.EngineCatalogPath{path}})
			ownerCalls := 0
			withdrawDuringIO := false
			service.WithNodeIdentityReader(nodeIdentityReaderFunc(func(_ context.Context, tenant uint, q authorizationfacts.NodeIdentityQuery) (*authorizationfacts.NodeIdentity, error) {
				ownerCalls++
				if withdrawDuringIO {
					revoke(t, sourceID)
					withdrawDuringIO = false
				}
				return &authorizationfacts.NodeIdentity{TenantID: tenant, Target: q.Target, Exists: true}, nil
			}))
			session, _ := issue(t, user, time.Minute)
			if result, err := service.GetMetadataRefreshScope(ctx, session.AccessToken, query); result != nil || !errors.Is(err, commonapi.ErrForbidden) || ownerCalls != 0 {
				t.Fatalf("missing scan function result=%+v calls=%d err=%v", result, ownerCalls, err)
			}
			role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.metadata_refresh", Name: "Metadata refresh fixture", PermissionKeys: []string{metadataRefreshPermission}, ActorPrincipalID: adminID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: adminID, Reason: "Explicit refresh fixture"}); err != nil {
				t.Fatal(err)
			}
			session, _ = issue(t, user, time.Minute)
			result, err := service.GetMetadataRefreshScope(ctx, session.AccessToken, query)
			if err != nil || result == nil || result.Validate() != nil || len(result.Ranges) != 1 || result.Ranges[0].Node != nil || result.Ranges[0].Item == nil {
				t.Fatalf("precise grants became a parent scan: %+v %v", result, err)
			}
			itemQuery := shared.MetadataRefreshQuery{Kind: "item", Target: path}
			exact, err := service.GetMetadataRefreshScope(ctx, session.AccessToken, itemQuery)
			if err != nil || exact == nil || exact.Validate() != nil || exact.Ranges[0].Item.ItemFingerprint != result.Ranges[0].Item.ItemFingerprint {
				t.Fatalf("exact scope=%+v err=%v", exact, err)
			}
			checkFrozenAfterWithdrawal := exerciseFrozenRefreshRuntime(t, db, service, result)
			withdrawDuringIO = true
			if result, err := service.GetMetadataRefreshScope(ctx, session.AccessToken, query); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("revoked during owner IO admitted %+v %v", result, err)
			}
			checkFrozenAfterWithdrawal()
			var now time.Time
			if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
				t.Fatal(err)
			}
			expiry := now.Add(time.Second)
			grant(t, "user", user.PrincipalID, &expiry)
			if _, err := service.GetMetadataRefreshScope(ctx, session.AccessToken, query); err != nil {
				t.Fatal(err)
			}
			time.Sleep(time.Until(expiry) + 20*time.Millisecond)
			if result, err := service.GetMetadataRefreshScope(ctx, session.AccessToken, query); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("expired scope reused %+v %v", result, err)
			}
		})
		t.Run("metadata candidates use current User and no source details", func(t *testing.T) {
			other := engineplugin.TabularItemPath(path.EngineID, "schema", "public", "ungranted_metadata")
			service := NewService(repo, nil).WithAuthorizationFactsReader(testMetaFacts{paths: []engineplugin.EngineCatalogPath{path, other}})
			user, _ := newUser(t, time.Hour)
			session, _ := issue(t, user, time.Minute)
			request := shared.SourceMetadataCheckRequest{Targets: []engineplugin.EngineCatalogPath{path, other}}
			if result, err := service.CheckMetaMetadataRead(ctx, session.AccessToken, request); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("missing metadata function: %+v %v", result, err)
			}
			role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.metadata_query", Name: "Metadata observation fixture", PermissionKeys: []string{MetaCatalogReadPermission}, ActorPrincipalID: adminID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: adminID, Reason: "Metadata observation fixture"}); err != nil {
				t.Fatal(err)
			}
			session, _ = issue(t, user, time.Minute)
			delegation, err := iam.NewDelegationService(identity, systemauthorization.ToolAuthorizationCatalog{}, iam.DelegationServiceConfig{})
			if err != nil {
				t.Fatal(err)
			}
			credentials := []string{session.AccessToken}
			for _, scope := range []string{"resource.children.list", "resource.ancestors.get"} {
				delegated, err := delegation.IssueDelegatedAccessToken(ctx, iam.IssueDelegatedAccessTokenInput{SourceAccessToken: session.AccessToken, Audience: "meta", Scopes: []string{scope}, AgentRunID: uuid.NewString(), ToolCallID: uuid.NewString()})
				if err != nil || delegated == nil {
					t.Fatalf("issue actual Meta delegate: %v", err)
				}
				credentials = append(credentials, delegated.AccessToken)
			}
			check := func(visible bool) {
				t.Helper()
				var before, after int64
				if err := db.Table("system.audit_logs").Count(&before).Error; err != nil {
					t.Fatal(err)
				}
				for _, credential := range credentials {
					result, err := service.CheckMetaMetadataRead(ctx, credential, request)
					if err != nil || result == nil || result.ObservedAt.IsZero() || len(result.Targets) != 2 || result.Targets[0].Visible != visible || result.Targets[1].Visible {
						t.Fatalf("metadata candidate visibility=%+v err=%v", result, err)
					}
					if strings.HasPrefix(credential, "addp_dat_") {
						if result, err := service.CheckMetaLineageRead(ctx, credential, request); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
							t.Fatalf("Meta delegate crossed into lineage: %+v %v", result, err)
						}

						if result, err := service.CheckManagerPreviewRead(authenticatedManagerReadRequest(ctx, service, credential), credential, SourceReadCheckRequest{Targets: []engineplugin.EngineCatalogPath{path}}); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
							t.Fatalf("Meta delegate crossed into Manager: %v", err)
						}
					}
				}
				if err := db.Table("system.audit_logs").Count(&after).Error; err != nil || before != after {
					t.Fatalf("observation changed audits %d/%d: %v", before, after, err)
				}
			}
			check(false)
			id := grant(t, "user", user.PrincipalID, nil)
			check(true)
			if result, err := service.CheckManagerPreviewRead(authenticatedManagerReadRequest(ctx, service, session.AccessToken), session.AccessToken, SourceReadCheckRequest{Targets: []engineplugin.EngineCatalogPath{path}}); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("metadata function became preview authority: %+v %v", result, err)
			}
			revoke(t, id)
			check(false)
			if result, err := service.CheckMetaMetadataRead(ctx, session.ResourceAccessTickets["manager"], request); result != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("ticket became metadata credential: %+v %v", result, err)
			}
		})
		t.Run("Manager search uses current source rules and exact delegation", func(t *testing.T) {
			other := engineplugin.TabularItemPath(path.EngineID, "schema", "public", "ungranted_search")
			service := NewService(repo, nil).WithAuthorizationFactsReader(testMetaFacts{paths: []engineplugin.EngineCatalogPath{path, other}})
			configureSearchObservationFixture(t, service)
			user, _ := newUser(t, time.Hour)
			role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.manager_search", Name: "Manager search fixture", PermissionKeys: []string{ManagerSearchPermission, MetaCatalogReadPermission}, ActorPrincipalID: adminID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: adminID, Reason: "Manager search fixture"}); err != nil {
				t.Fatal(err)
			}
			session, _ := issue(t, user, time.Minute)
			delegation, err := iam.NewDelegationService(identity, systemauthorization.ToolAuthorizationCatalog{}, iam.DelegationServiceConfig{})
			if err != nil {
				t.Fatal(err)
			}
			search, err := delegation.IssueDelegatedAccessToken(ctx, iam.IssueDelegatedAccessTokenInput{SourceAccessToken: session.AccessToken, Audience: "manager", Scopes: []string{"data.search"}, AgentRunID: uuid.NewString(), ToolCallID: uuid.NewString()})
			if err != nil {
				t.Fatal(err)
			}
			meta, err := delegation.IssueDelegatedAccessToken(ctx, iam.IssueDelegatedAccessTokenInput{SourceAccessToken: session.AccessToken, Audience: "meta", Scopes: []string{"resource.children.list"}, AgentRunID: uuid.NewString(), ToolCallID: uuid.NewString()})
			if err != nil {
				t.Fatal(err)
			}
			request := shared.SourceMetadataCheckRequest{Targets: []engineplugin.EngineCatalogPath{path, other}}
			searchRequest := shared.SourceSearchCheckRequest{Targets: request.Targets, QueryNonce: "0123456789abcdef0123456789abcdef"}
			if result, err := service.CheckManagerSearchRead(ctx, meta.AccessToken, searchRequest); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("Meta delegation reached search: %+v %v", result, err)
			}
			if result, err := service.CheckMetaMetadataRead(ctx, search.AccessToken, request); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("search delegation reached Meta: %+v %v", result, err)
			}
			check := func(want bool) {
				t.Helper()
				for _, credential := range []string{session.AccessToken, search.AccessToken} {
					observedCtx, timing := ObserveManagerSearchTiming(ctx)
					result, err := service.CheckManagerSearchRead(observedCtx, credential, searchRequest)
					if err != nil || !result.Matches(searchRequest) || result.Targets[0].Visible != want || result.Targets[0].ContentReadable != want || result.Targets[1].Visible || result.Targets[1].ContentReadable {
						t.Fatalf("want readable=%v: %+v %v", want, result, err)
					}
					if timing.candidates != 2 || timing.wholeCandidates != 0 || timing.engineQueries != 1 ||
						timing.ruleItems != 4 || timing.ruleQueries != 1 || timing.ruleQuery <= 0 ||
						timing.observedCredential <= 0 || timing.ruleObservation < timing.ruleQuery+timing.observedCredential ||
						timing.metaFacts <= 0 || timing.metadataBinding < timing.metaFacts {
						t.Fatalf("unexpected table search timing: %+v", timing)
					}
				}
			}
			check(false)
			id := grant(t, "user", user.PrincipalID, nil)
			check(true)
			empty := shared.SourceSearchCheckRequest{QueryNonce: searchRequest.QueryNonce, Targets: []engineplugin.EngineCatalogPath{}}
			begun, err := service.CheckManagerSearchRead(ctx, session.AccessToken, empty)
			if err != nil || !begun.Matches(empty) {
				t.Fatalf("empty observation failed: %v", err)
			}
			stable := searchRequest
			stable.ExpectedObservationTag = &begun.ObservationTag
			if result, err := service.CheckManagerSearchRead(ctx, session.AccessToken, stable); err != nil || !result.Matches(stable) {
				t.Fatalf("stable continuation rejected: %v", err)
			}
			rollback := db.Begin()
			if rollback.Error != nil {
				t.Fatal(rollback.Error)
			}
			if err := rollback.Exec("UPDATE system.engines SET version=version+1 WHERE id=?", path.EngineID).Error; err != nil {
				rollback.Rollback()
				t.Fatal(err)
			}
			if err := rollback.Rollback().Error; err != nil {
				t.Fatal(err)
			}
			if _, err := service.CheckManagerSearchRead(ctx, session.AccessToken, stable); err != nil {
				t.Fatalf("rolled back basis change invalidated search: %v", err)
			}

			revoke(t, id)
			if result, err := service.CheckManagerSearchRead(ctx, session.AccessToken, stable); result != nil || !errors.Is(err, ErrSearchContextChanged) {
				t.Fatalf("withdrawal reused old ranking: %+v %v", result, err)
			}

			check(false)

			var dbNow time.Time
			if err := db.Raw("SELECT clock_timestamp()").Scan(&dbNow).Error; err != nil {
				t.Fatal(err)
			}
			deadline := dbNow.Add(time.Second)
			grant(t, "user", user.PrincipalID, &deadline)
			begun, err = service.CheckManagerSearchRead(ctx, session.AccessToken, empty)
			if err != nil {
				t.Fatal(err)
			}
			stable.ExpectedObservationTag = &begun.ObservationTag
			if err := db.Exec("SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM (?::timestamptz-clock_timestamp()))+0.02))", deadline).Error; err != nil {
				t.Fatal(err)
			}
			if result, err := service.CheckManagerSearchRead(ctx, session.AccessToken, stable); result != nil || !errors.Is(err, ErrSearchContextChanged) {
				t.Fatalf("natural expiry reused old ranking: %+v %v", result, err)
			}
			service.searchRevisionReader = searchChangingRevisionFixture{calls: new(int)}
			if result, err := service.CheckManagerSearchRead(ctx, session.AccessToken, empty); result != nil || !errors.Is(err, ErrSearchContextChanged) {
				t.Fatalf("Meta changed between observations: %+v %v", result, err)
			}
			configureSearchObservationFixture(t, service)

			tenant := uint(tenantID)
			nfs := models.Engine{Name: "Manager search whole fixture", EngineType: "nfs", TenantID: &tenant,
				Version: 1, LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{"host": "manager-search.invalid"},
				IdentityKey: models.JSONString(`{"host":"manager-search.invalid"}`)}
			if err := db.WithContext(ctx).Table("system.engines").Create(&nfs).Error; err != nil {
				t.Fatal(err)
			}
			var wholeTargets []engineplugin.EngineCatalogPath
			factsByKey := map[string]*authorizationfacts.AuthorizationFacts{}
			for _, dir := range []string{"test/a.gdb", "test/b.gdb"} {
				root := engineplugin.FileDirectoryPath(nfs.ID, dir)
				wholeTargets = append(wholeTargets, root)
				facts := authorizationfacts.AuthorizationFacts{
					SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: nfs.ID,
					ItemType: "file", FullName: root.StringPath(), ItemFingerprint: commonmodels.GenerateItemFingerprint(nfs.ID, root.StringPath()),
					BasisState: authorizationfacts.AuthorizationBasisComplete, ObservedAt: time.Now(),
					Boundary: &authorizationfacts.AuthorizationBoundary{Layout: "whole",
						Scope: &authorizationfacts.ScopeBoundary{Format: "filegdb", RuleVersion: 1, Root: root}},
				}
				hash, err := facts.ComputeBoundaryHash()
				if err != nil {
					t.Fatal(err)
				}
				facts.BoundaryHash = &hash
				factsByKey[facts.ItemFingerprint] = &facts
			}
			service.WithAuthorizationFactsReader(metadataBatchReaderFunc(func(_ context.Context, _ uint, keys []string) (*authorizationfacts.AuthorizationFactsBatch, error) {
				batch := &authorizationfacts.AuthorizationFactsBatch{}
				for _, key := range keys {
					batch.Data = append(batch.Data, authorizationfacts.AuthorizationFactsResult{ItemFingerprint: key, Facts: factsByKey[key]})
				}
				return batch, nil
			}))
			for _, tc := range []struct {
				targets                           []engineplugin.EngineCatalogPath
				whole, engineQueries, ruleQueries int
			}{
				{targets: wholeTargets, whole: 2, engineQueries: 3, ruleQueries: 1},
				{targets: []engineplugin.EngineCatalogPath{path}},
			} {
				req := shared.SourceSearchCheckRequest{Targets: tc.targets, QueryNonce: "0123456789abcdef0123456789abcdef"}
				observedCtx, timing := ObserveManagerSearchTiming(ctx)
				result, err := service.CheckManagerSearchRead(observedCtx, session.AccessToken, req)
				if err != nil || !result.Matches(req) || len(result.Targets) != len(tc.targets) {
					t.Fatalf("invalid search observation: %v", err)
				}
				for _, target := range result.Targets {
					if target.Visible || target.ContentReadable {
						t.Fatal("an ungranted or unproven candidate became visible")
					}
				}
				if timing.candidates != len(tc.targets) || timing.wholeCandidates != tc.whole || timing.engineQueries != tc.engineQueries ||
					timing.ruleItems != tc.whole*2 || timing.ruleQueries != tc.ruleQueries || (timing.ruleQuery > 0) != (tc.ruleQueries > 0) ||
					timing.observedCredential <= 0 || timing.ruleObservation < timing.observedCredential {
					t.Fatalf("unexpected whole or missing search timing: %+v", timing)
				}
			}
		})
		t.Run("lineage uses only its function and current data rules", func(t *testing.T) {
			service := NewService(repo, nil).WithAuthorizationFactsReader(testMetaFacts{paths: []engineplugin.EngineCatalogPath{path}})
			user, _ := newUser(t, time.Hour)
			session, _ := issue(t, user, time.Minute)
			request := shared.SourceMetadataCheckRequest{Targets: []engineplugin.EngineCatalogPath{path}}
			if result, err := service.CheckMetaLineageRead(ctx, session.AccessToken, request); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("missing function: %+v %v", result, err)
			}
			role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.lineage_query", Name: "Lineage observation fixture", PermissionKeys: []string{MetaLineageReadPermission}, ActorPrincipalID: adminID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: adminID, Reason: "Lineage observation fixture"}); err != nil {
				t.Fatal(err)
			}
			session, _ = issue(t, user, time.Minute)
			check := func(visible bool) {
				t.Helper()
				result, err := service.CheckMetaLineageRead(ctx, session.AccessToken, request)
				if err != nil || !result.Matches(request) || result.Targets[0].Visible != visible {
					t.Fatalf("lineage visibility: %+v %v", result, err)
				}
			}
			check(false)
			id := grant(t, "user", user.PrincipalID, nil)
			check(true)
			if result, err := service.CheckMetaMetadataRead(ctx, session.AccessToken, request); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("lineage became catalog permission: %+v %v", result, err)
			}
			revoke(t, id)
			check(false)
		})
		t.Run("formal preview consumer intersects function permission and source rules", func(t *testing.T) {
			service := NewService(repo, nil).WithAuthorizationFactsReader(testMetaFacts{paths: []engineplugin.EngineCatalogPath{path, engineplugin.TabularItemPath(path.EngineID, "schema", "public", "formal_preview_ungranted_D")}})
			request := SourceReadCheckRequest{Targets: []engineplugin.EngineCatalogPath{path}}
			user, _ := newUser(t, time.Hour)
			grantID := grant(t, "user", user.PrincipalID, nil)
			session, _ := issue(t, user, time.Minute)
			if result, err := service.CheckManagerPreviewRead(authenticatedManagerReadRequest(ctx, service, session.AccessToken), session.AccessToken, request); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("Grant without function Permission accepted: %+v %v", result, err)
			}
			if result, err := service.CheckManagerProfileResultRead(authenticatedManagerReadRequest(ctx, service, session.AccessToken), session.AccessToken, request); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("profile Grant without function accepted: %+v %v", result, err)
			}
			role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.preview_check", Name: "Explicit preview access",
				PermissionKeys: []string{ManagerPreviewReadPermission}, ActorPrincipalID: adminID})
			if err != nil {
				t.Fatal(err)
			}
			assign := func(user userProvenance) *iam.ManagedTenantRoleAssignment {
				t.Helper()
				assignments, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID,
					RoleIDs: []int64{role.ID}, ActorPrincipalID: adminID, Reason: "Explicit preview consumer fixture"})
				if err != nil || len(assignments) != 1 {
					t.Fatalf("assign=%+v %v", assignments, err)
				}
				return &assignments[0]
			}
			assignment := assign(user)
			session, _ = issue(t, user, time.Minute)
			delegation, err := iam.NewDelegationService(identity, systemauthorization.ToolAuthorizationCatalog{}, iam.DelegationServiceConfig{})
			if err != nil {
				t.Fatal(err)
			}
			delegate := func(session *iam.IssuedBrowserSession) string {
				t.Helper()
				issued, err := delegation.IssueDelegatedAccessToken(ctx, iam.IssueDelegatedAccessTokenInput{
					SourceAccessToken: session.AccessToken, Audience: "manager", Scopes: []string{"data.preview"},
					AgentRunID: uuid.NewString(), ToolCallID: uuid.NewString(),
				})
				if err != nil || issued == nil {
					t.Fatalf("issue real preview delegation: %v", err)
				}
				return issued.AccessToken
			}
			delegated := delegate(session)
			credentials := []string{session.AccessToken, delegated}
			checkProfile := func(credential string, targets []engineplugin.EngineCatalogPath, want error) {
				t.Helper()
				result, err := service.CheckManagerProfileResultRead(authenticatedManagerReadRequest(ctx, service, credential), credential, SourceReadCheckRequest{Targets: targets})
				if want == nil {
					if err != nil || result == nil || result.ObservedAt.IsZero() {
						t.Fatalf("profile result source coverage=%+v %v", result, err)
					}
				} else if result != nil || !errors.Is(err, want) {
					t.Fatalf("profile result accepted=%+v %v want=%v", result, err, want)
				}
			}
			for _, consumer := range []string{"preview", "profile"} {
				for _, change := range []string{"unchanged", "grant withdrawal", "Grant expiry", "credential expiry", "logout", "function withdrawal", "department exit", "project group exit"} {
					t.Run(consumer+" rechecks after Meta "+change, func(t *testing.T) {
						readerUser, _ := newUser(t, time.Hour)
						readerAssignment := assign(readerUser)
						mutate := func() {}
						recipientType, recipientID := "user", readerUser.PrincipalID
						if change == "department exit" || change == "project group exit" {
							organizations := iam.NewOrganizationService(identity, testDatabaseClock(t, db))
							code := "request_reuse_" + strings.ReplaceAll(uuid.NewString(), "-", "")
							var member *iam.ManagedOrganizationMembership
							if change == "department exit" {
								department, err := organizations.CreateDepartment(ctx, iam.CreateDepartmentInput{TenantID: tenantID, ActorPrincipalID: adminID, Code: code, Name: code})
								if err != nil {
									t.Fatal(err)
								}
								member, err = organizations.CreateDepartmentMembership(ctx, iam.CreateDepartmentMembershipInput{TenantID: tenantID, DepartmentID: department.ID, TenantMembershipID: readerUser.MembershipID, ActorPrincipalID: adminID, MembershipType: "primary", RelationRole: "member"})
								if err != nil {
									t.Fatal(err)
								}
								recipientType, recipientID = "department", department.ID
								mutate = func() {
									_, err := organizations.CloseDepartmentMembership(ctx, iam.CloseOrganizationMembershipInput{TenantID: tenantID, OrganizationID: department.ID, MembershipID: member.ID, Version: member.Version, ActorPrincipalID: adminID, Reason: "Exit during Meta facts IO"})
									if err != nil {
										t.Fatal(err)
									}
								}
							} else {
								group, err := organizations.CreateProjectGroup(ctx, iam.CreateProjectGroupInput{TenantID: tenantID, ActorPrincipalID: adminID, Code: code, Name: code})
								if err != nil {
									t.Fatal(err)
								}
								member, err = organizations.CreateProjectGroupMembership(ctx, iam.CreateProjectGroupMembershipInput{TenantID: tenantID, ProjectGroupID: group.ID, TenantMembershipID: readerUser.MembershipID, ActorPrincipalID: adminID, RelationRole: "member"})
								if err != nil {
									t.Fatal(err)
								}
								recipientType, recipientID = "project_group", group.ID
								mutate = func() {
									_, err := organizations.CloseProjectGroupMembership(ctx, iam.CloseOrganizationMembershipInput{TenantID: tenantID, OrganizationID: group.ID, MembershipID: member.ID, Version: member.Version, ActorPrincipalID: adminID, Reason: "Exit during Meta facts IO"})
									if err != nil {
										t.Fatal(err)
									}
								}
							}
						}
						var expires *time.Time
						if change == "Grant expiry" {
							value := time.Now().UTC().Add(350 * time.Millisecond)
							expires = &value
						}
						id := grant(t, recipientType, recipientID, expires)
						ttl := time.Minute
						if change == "credential expiry" {
							ttl = 350 * time.Millisecond
						}
						readerSession, readerTokens := issue(t, readerUser, ttl)
						want := error(nil)
						switch change {
						case "grant withdrawal":
							want, mutate = commonapi.ErrForbidden, func() { revoke(t, id) }
						case "Grant expiry", "credential expiry":
							until := readerSession.AccessTokenExpiresAt
							want = commonapi.ErrUnauthorized
							if expires != nil {
								until, want = *expires, commonapi.ErrForbidden
							}
							mutate = func() {
								if err := db.WithContext(ctx).Exec("SELECT pg_sleep(GREATEST(0, EXTRACT(EPOCH FROM (?::timestamptz - clock_timestamp()))) + 0.02)", until).Error; err != nil {
									t.Fatal(err)
								}
							}
						case "logout":
							want = commonapi.ErrUnauthorized
							mutate = func() {
								logout, err := iam.NewLogoutService(identity, readerTokens)
								if err != nil {
									t.Fatal(err)
								}
								if err := logout.LogoutBrowserSession(ctx, iam.LogoutBrowserSessionInput{AccessToken: readerSession.AccessToken, RefreshToken: readerSession.RefreshToken}); err != nil {
									t.Fatal(err)
								}
							}
						case "function withdrawal":
							want = commonapi.ErrUnauthorized
							mutate = func() {
								if _, err := roles.RevokeAssignment(ctx, iam.RevokeTenantRoleAssignmentInput{TenantID: tenantID, AssignmentID: readerAssignment.ID, ActorPrincipalID: adminID, Reason: "Withdraw during Meta IO"}); err != nil {
									t.Fatal(err)
								}
							}
						case "department exit", "project group exit":
							want = commonapi.ErrUnauthorized
						}
						resolutions, factsCalls := 0, 0
						observed := iam.WithAuthContextTiming(ctx, func(slog.Value) { resolutions++ })
						entry := authenticatedManagerReadRequest(observed, service, readerSession.AccessToken)
						consumerService := NewService(repo, nil).WithAuthorizationFactsReader(bindingFactsReaderFunc(func(_ context.Context, tenant uint, fingerprint string) (*authorizationfacts.AuthorizationFacts, error) {
							factsCalls++
							facts := testBindingFacts(path)
							if resolutions != 1 || int64(tenant) != tenantID || fingerprint != facts.ItemFingerprint {
								t.Fatal("Meta preparation repeated IAM resolution or changed the trusted target")
							}
							mutate()
							return &facts, nil
						}))
						check := consumerService.CheckManagerPreviewRead
						if consumer == "profile" {
							check = consumerService.CheckManagerProfileResultRead
						}
						result, err := check(entry, readerSession.AccessToken, request)
						if factsCalls != 1 || (want == nil && (err != nil || result == nil || resolutions != 2)) || (want != nil && (result != nil || !errors.Is(err, want))) {
							t.Fatalf("Meta return final check: result=%+v err=%v want=%v resolutions=%d calls=%d", result, err, want, resolutions, factsCalls)
						}
					})
				}
			}
			var before, after int64
			if err := db.Table("system.audit_logs").Count(&before).Error; err != nil {
				t.Fatal(err)
			}
			for _, credential := range credentials {
				result, err := service.CheckManagerPreviewRead(authenticatedManagerReadRequest(ctx, service, credential), credential, request)
				if err != nil || result == nil || result.ObservedAt.IsZero() {
					t.Fatalf("function + source intersection=%+v %v", result, err)
				}
			}
			trustedReader := service.authorizationFacts
			for _, state := range []string{"missing", "offline", "nil", "incomplete", "needs_refresh", "unsupported", "changed_fingerprint", "changed_native_namespace", "invalid_boundary_hash"} {
				t.Run("existing Grant still requires current Meta facts "+state, func(t *testing.T) {
					defer service.WithAuthorizationFactsReader(trustedReader)
					calls := 0
					want := ErrAuthorizationBoundaryChanged
					if state == "offline" || state == "nil" || state == "changed_fingerprint" || state == "invalid_boundary_hash" {
						want = ErrAuthorizationFactsUnavailable
					}
					service.WithAuthorizationFactsReader(bindingFactsReaderFunc(func(_ context.Context, tenant uint, fingerprint string) (*authorizationfacts.AuthorizationFacts, error) {
						calls++
						facts := testBindingFacts(path)
						if int64(tenant) != tenantID || fingerprint != facts.ItemFingerprint {
							t.Fatal("facts request lost the trusted tenant or exact target")
						}
						switch state {
						case "missing":
							return nil, &client.TenantAPIError{StatusCode: http.StatusNotFound}
						case "offline":
							return nil, errors.New("Meta facts unavailable")
						case "nil":
							return nil, nil
						case "changed_fingerprint":
							other := testBindingFacts(engineplugin.TabularItemPath(path.EngineID, "schema", path.Segments[1].Name, "different_preview_target"))
							return &other, nil
						case "changed_native_namespace":
							// Same Engine, dotted name and fingerprint, but a different
							// typed boundary. A valid DTO is not proof of PG ownership.
							other := testBindingFacts(engineplugin.TabularItemPath(path.EngineID, "database", path.Segments[1].Name, path.Segments[2].Name))
							if other.Validate() != nil || other.ItemFingerprint != facts.ItemFingerprint || *other.BoundaryHash == *facts.BoundaryHash {
								t.Fatal("fixture must retain identity with a valid different boundary")
							}
							return &other, nil
						case "invalid_boundary_hash":
							hash := strings.Repeat("a", 64)
							facts.BoundaryHash = &hash
							return &facts, nil
						default:
							facts.BasisState = authorizationfacts.AuthorizationBasisState(state)
							facts.Boundary, facts.BoundaryHash = nil, nil
							return &facts, nil
						}
					}))
					for _, credential := range credentials {
						if result, err := service.CheckManagerPreviewRead(authenticatedManagerReadRequest(ctx, service, credential), credential, request); result != nil || !errors.Is(err, want) {
							t.Fatalf("stored Grant bypassed Meta facts: result=%+v error=%v want=%v", result, err, want)
						}
					}
					if result, err := service.CheckManagerProfileResultRead(authenticatedManagerReadRequest(ctx, service, session.AccessToken), session.AccessToken, request); result != nil || !errors.Is(err, want) {
						t.Fatalf("stored profile Grant bypassed Meta facts: result=%+v error=%v want=%v", result, err, want)
					}
					if calls != len(credentials)+1 {
						t.Fatalf("every check must re-read current Meta facts: calls=%d", calls)
					}
				})
				// The same credential and stored Grant recover only after trusted
				// facts recover; no new grant or cached Allow is manufactured.
				if result, err := service.CheckManagerPreviewRead(authenticatedManagerReadRequest(ctx, service, session.AccessToken), session.AccessToken, request); err != nil || result == nil {
					t.Fatalf("recovered facts failed to restore the original check: %+v %v", result, err)
				}
			}
			d := engineplugin.TabularItemPath(path.EngineID, "schema", "public", "formal_preview_ungranted_D")
			checkProfile(session.AccessToken, request.Targets, nil)
			checkProfile(session.AccessToken, []engineplugin.EngineCatalogPath{path, d}, commonapi.ErrForbidden)
			checkProfile(delegated, request.Targets, commonapi.ErrUnauthorized)
			checkProfile(session.ResourceAccessTickets["manager"], request.Targets, commonapi.ErrUnauthorized)
			for _, credential := range credentials {
				if result, err := service.CheckManagerPreviewRead(authenticatedManagerReadRequest(ctx, service, credential), credential, SourceReadCheckRequest{Targets: []engineplugin.EngineCatalogPath{path, d}}); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
					t.Fatalf("partial C+D accepted=%+v %v", result, err)
				}
			}
			if result, err := repo.testReadCurrentUserSourceRules(ctx, delegated, request.Targets); result != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("delegate downgraded into ordinary User reader=%+v %v", result, err)
			}
			if result, err := service.CheckManagerPreviewRead(authenticatedManagerReadRequest(ctx, service, session.ResourceAccessTickets["manager"]), session.ResourceAccessTickets["manager"], request); result != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("ticket accepted=%+v %v", result, err)
			}
			if err := db.Table("system.audit_logs").Count(&after).Error; err != nil || before != after {
				t.Fatalf("read check wrote audits=%d/%d %v", before, after, err)
			}
			revoke(t, grantID)
			checkProfile(session.AccessToken, request.Targets, commonapi.ErrForbidden)
			for _, credential := range credentials {
				if result, err := service.CheckManagerPreviewRead(authenticatedManagerReadRequest(ctx, service, credential), credential, request); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
					t.Fatalf("revoked Grant accepted=%+v %v", result, err)
				}
			}
			grant(t, "user", user.PrincipalID, nil)
			checkProfile(session.AccessToken, request.Targets, nil)
			for _, credential := range credentials {
				if result, err := service.CheckManagerPreviewRead(authenticatedManagerReadRequest(ctx, service, credential), credential, request); err != nil || result == nil {
					t.Fatalf("new explicit Grant not observed=%+v %v", result, err)
				}
			}
			if _, err := roles.RevokeAssignment(ctx, iam.RevokeTenantRoleAssignmentInput{TenantID: tenantID, AssignmentID: assignment.ID, ActorPrincipalID: adminID, Reason: "End preview fixture"}); err != nil {
				t.Fatal(err)
			}
			checkProfile(session.AccessToken, request.Targets, commonapi.ErrUnauthorized)
			for _, credential := range credentials {
				if result, err := service.CheckManagerPreviewRead(authenticatedManagerReadRequest(ctx, service, credential), credential, request); result != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
					t.Fatalf("old authorization credential accepted=%+v %v", result, err)
				}
			}
			session, _ = issue(t, user, time.Minute)
			checkProfile(session.AccessToken, request.Targets, commonapi.ErrForbidden)
			if result, err := service.CheckManagerPreviewRead(authenticatedManagerReadRequest(ctx, service, session.AccessToken), session.AccessToken, request); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("revoked function Permission accepted=%+v %v", result, err)
			}
			ungranted, _ := newUser(t, time.Hour)
			assign(ungranted)
			session, _ = issue(t, ungranted, time.Minute)
			checkProfile(session.AccessToken, request.Targets, commonapi.ErrForbidden)
			for _, credential := range []string{session.AccessToken, delegate(session)} {
				if result, err := service.CheckManagerPreviewRead(authenticatedManagerReadRequest(ctx, service, credential), credential, request); result != nil || !errors.Is(err, commonapi.ErrForbidden) {
					t.Fatalf("function without Grant accepted=%+v %v", result, err)
				}
			}
			familyUser, _ := newUser(t, time.Hour)
			assign(familyUser)
			grant(t, "user", familyUser.PrincipalID, nil)
			familySession, tokens := issue(t, familyUser, time.Minute)
			familyDelegate := delegate(familySession)
			if _, err := service.CheckManagerPreviewRead(authenticatedManagerReadRequest(ctx, service, familyDelegate), familyDelegate, request); err != nil {
				t.Fatal(err)
			}
			logout, err := iam.NewLogoutService(identity, tokens)
			if err != nil {
				t.Fatal(err)
			}
			if err := logout.LogoutBrowserSession(ctx, iam.LogoutBrowserSessionInput{AccessToken: familySession.AccessToken, RefreshToken: familySession.RefreshToken}); err != nil {
				t.Fatal(err)
			}
			if result, err := service.CheckManagerPreviewRead(authenticatedManagerReadRequest(ctx, service, familyDelegate), familyDelegate, request); result != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("revoked source family delegate accepted=%+v %v", result, err)
			}
			checkProfile(familySession.AccessToken, request.Targets, commonapi.ErrUnauthorized)
		})
		t.Run("identity derived only from credential and no observation writes", func(t *testing.T) {
			user, _ := newUser(t, time.Hour)
			grant(t, "user", user.PrincipalID, nil)
			session, tokens := issue(t, user, time.Minute)
			var before, after int64
			if err := db.Table("system.audit_logs").Count(&before).Error; err != nil {
				t.Fatal(err)
			}
			result, err := repo.testReadCurrentUserSourceRules(ctx, session.AccessToken, []engineplugin.EngineCatalogPath{path})
			if err != nil || result == nil || !result.Covered {
				t.Fatalf("real credential=%+v %v", result, err)
			}
			if err := db.Table("system.audit_logs").Count(&after).Error; err != nil || before != after {
				t.Fatalf("observation audit=%d/%d %v", before, after, err)
			}
			d := engineplugin.TabularItemPath(path.EngineID, "schema", "public", "credential_ungranted_D")
			result, err = repo.testReadCurrentUserSourceRules(ctx, session.AccessToken, []engineplugin.EngineCatalogPath{path, d})
			if err != nil || result == nil || result.Covered || !result.Targets[0].Covered || result.Targets[1].Reason != "no_grant" {
				t.Fatalf("credential C+D=%+v %v", result, err)
			}
			for _, token := range []string{"", "addp_at_unknown", "Bearer " + session.AccessToken, session.ResourceAccessTickets["manager"], "addp_dat_wrong", "addp_rt_wrong"} {
				result, err := repo.testReadCurrentUserSourceRules(ctx, token, []engineplugin.EngineCatalogPath{path})
				if result != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
					t.Fatal("invalid/non-User credential must return nil and unauthorized")
				}
			}
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			result, err = NewRepository(tx).testReadCurrentUserSourceRules(ctx, session.AccessToken, []engineplugin.EngineCatalogPath{path})
			if result != nil || !errors.Is(err, errFulfillmentBinding) {
				t.Fatalf("caller transaction accepted=%+v %v", result, err)
			}
			if err := tx.Rollback().Error; err != nil {
				t.Fatal(err)
			}
			logout, err := iam.NewLogoutService(identity, tokens)
			if err != nil {
				t.Fatal(err)
			}
			if err := logout.LogoutBrowserSession(ctx, iam.LogoutBrowserSessionInput{AccessToken: session.AccessToken, RefreshToken: session.RefreshToken}); err != nil {
				t.Fatal(err)
			}
			result, err = repo.testReadCurrentUserSourceRules(ctx, session.AccessToken, []engineplugin.EngineCatalogPath{path})
			if result != nil || !errors.Is(err, commonapi.ErrUnauthorized) {
				t.Fatalf("revoked Family accepted=%+v %v", result, err)
			}
		})
		t.Run("credential and rules use one committed read only snapshot", func(t *testing.T) {
			user, _ := newUser(t, time.Hour)
			session, _ := issue(t, user, time.Minute)
			callback := "test:source-credential-snapshot"
			called := false
			if err := db.Callback().Row().Before("gorm:row").Register(callback, func(tx *gorm.DB) {
				if called || !strings.Contains(tx.Statement.SQL.String(), "WITH input AS MATERIALIZED") {
					return
				}
				called = true
				var isolation, readOnly string
				if err := tx.Statement.ConnPool.QueryRowContext(ctx, "SELECT current_setting('transaction_isolation'), current_setting('transaction_read_only')").Scan(&isolation, &readOnly); err != nil {
					tx.AddError(err)
					return
				}
				if isolation != "repeatable read" || readOnly != "on" {
					tx.AddError(errors.New("credential snapshot is not repeatable read and read only"))
					return
				}
				// An independent formal writer commits after IAM credential loading.
				// This observation must not mix its earlier credential with a newer Grant.
				grant(t, "user", user.PrincipalID, nil)
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Callback().Row().Remove(callback) })
			result, err := repo.testReadCurrentUserSourceRules(ctx, session.AccessToken, []engineplugin.EngineCatalogPath{path})
			if err != nil || result == nil || result.Covered || !called || result.Targets[0].Reason != "no_grant" {
				t.Fatalf("mixed credential/rule snapshot=%+v %v called=%t", result, err, called)
			}
			if err := db.Callback().Row().Remove(callback); err != nil {
				t.Fatal(err)
			}
			result, err = repo.testReadCurrentUserSourceRules(ctx, session.AccessToken, []engineplugin.EngineCatalogPath{path})
			if err != nil || result == nil || !result.Covered {
				t.Fatalf("later committed Grant=%+v %v", result, err)
			}
		})
		t.Run("database wall clock expires credential after IAM projection", func(t *testing.T) {
			user, _ := newUser(t, time.Hour)
			grant(t, "user", user.PrincipalID, nil)
			session, _ := issue(t, user, 350*time.Millisecond)
			callback := "test:source-credential-expiry"
			called := false
			if err := db.Callback().Row().Before("gorm:row").Register(callback, func(tx *gorm.DB) {
				if called || !strings.Contains(tx.Statement.SQL.String(), "WITH input AS MATERIALIZED") {
					return
				}
				called = true
				_, err := tx.Statement.ConnPool.ExecContext(ctx, "SELECT pg_sleep(GREATEST(0, EXTRACT(EPOCH FROM ($1::timestamptz - clock_timestamp()))) + 0.02)", session.AccessTokenExpiresAt)
				if err != nil {
					tx.AddError(err)
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Row().Remove(callback)
			result, err := repo.testReadCurrentUserSourceRules(ctx, session.AccessToken, []engineplugin.EngineCatalogPath{path})
			if result != nil || !errors.Is(err, commonapi.ErrUnauthorized) || !called {
				t.Fatalf("expired projected credential accepted=%+v %v called=%t", result, err, called)
			}
		})
	})
}
