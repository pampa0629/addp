package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/addp/common/client"
	"net/http"
	"strings"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Included in the existing file-composition PostgreSQL gate: real Grant
// commands and current User credentials, with only Meta identity as a fixture.
func exerciseMetadataOnlyGrants(t *testing.T, db *gorm.DB, tenantID, adminID int64, roles *iam.TenantRoleService, newUser func(*testing.T, time.Duration) (userProvenance, time.Time)) {
	t.Run("metadata identity never grants content", func(t *testing.T) {
		ctx := t.Context()
		issuerRole, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.generic_metadata_issuer", Name: "Metadata issuer", PermissionKeys: []string{"system.engine_access_delegation.create", "system.engine_access_grant.create", "system.engine_access_grant.read", "system.engine_access_grant.revoke"}, ActorPrincipalID: adminID})
		if err != nil {
			t.Fatal(err)
		}
		readerRole, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.generic_metadata_reader", Name: "Metadata viewer", PermissionKeys: []string{MetaCatalogReadPermission, MetaLineageReadPermission, ManagerPreviewReadPermission, ManagerSearchPermission}, ActorPrincipalID: adminID})
		if err != nil {
			t.Fatal(err)
		}
		qualify := func(roleID int64) userProvenance {
			user, _ := newUser(t, time.Hour)
			if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID, RoleIDs: []int64{roleID}, ActorPrincipalID: adminID, Reason: "Explicit metadata fixture"}); err != nil {
				t.Fatal(err)
			}
			principal, err := iam.NewRepository(db).GetPrincipal(ctx, user.PrincipalID)
			if err != nil {
				t.Fatal(err)
			}
			user.AuthorizationVersion = principal.AuthorizationVersion
			return user
		}
		operator := qualify(issuerRole.ID)
		actor := Actor{TenantID: tenantID, PrincipalID: operator.PrincipalID, MembershipID: operator.MembershipID, AuthorizationVersion: operator.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Hour)}
		for _, kind := range []string{"nfs", "ftp", "minio", "ftp-whitespace", "minio-whitespace", "nfs-whole", "minio-whole", "postgresql", "mysql", "mongodb", "elasticsearch", "redis", "neo4j", "kafka"} {
			t.Run(kind, func(t *testing.T) {
				tenant := uint(tenantID)
				engineType := strings.TrimSuffix(strings.TrimSuffix(kind, "-whole"), "-whitespace")
				engine := models.Engine{Name: "Metadata identity " + kind, EngineType: engineType, TenantID: &tenant, Version: 1, LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{"host": "metadata-" + kind + ".invalid"}, IdentityKey: models.JSONString(`{"host":"metadata-` + kind + `.invalid"}`)}
				if err := db.Table("system.engines").Create(&engine).Error; err != nil {
					t.Fatal(err)
				}
				target := plugin.FileItemPath(engine.ID, "outputs/nodes.gpkg")
				switch kind {
				case "mysql":
					target = plugin.TabularItemPath(engine.ID, "database", "a.b", "c")
				case "mongodb":
					target = plugin.EngineCatalogBranchLeafPath(plugin.DynamicSchemaCatalogModel(), engine.ID, "database", "a.b", "collection", "collection", "c")
				case "postgresql":
					target = plugin.TabularItemPath(engine.ID, "schema", "a.b", "view.c")
					target.Segments[len(target.Segments)-1].Kind = "view"
				case "minio":
					target = plugin.ObjectItemPath(engine.ID, "outputs", "nodes.gpkg")
				case "elasticsearch", "redis", "neo4j", "kafka":
					root, leaf := "service", "index"
					if kind == "redis" {
						root, leaf = "server", "keyspace"
					}
					if kind == "neo4j" {
						root, leaf = "server", "graph"
					}
					if kind == "kafka" {
						leaf = "topic"
					}
					target = plugin.EngineCatalogPath{Version: plugin.EngineCatalogPathVersion, EngineID: engine.ID, Segments: []plugin.EngineCatalogSegment{{Term: root, Kind: root, Name: ""}, {Term: leaf, Kind: leaf, Name: "a.b / 空格"}}}
				}
				if kind == "neo4j" {
					target.Segments = append(target.Segments[:1], plugin.EngineCatalogSegment{Term: "database", Kind: "namespace", Name: "neo4j"}, target.Segments[1])
				}
				if strings.HasSuffix(kind, "-whitespace") {
					target = plugin.FileItemPath(engine.ID, "outputs/ ")
					if engineType == "minio" {
						target = plugin.ObjectItemPath(engine.ID, "outputs", " ")
					}
					if target.Segments[len(target.Segments)-1].Name != " " {
						t.Fatal("physical whitespace identity changed")
					}
				}
				layout := "single"
				if strings.HasSuffix(kind, "-whole") {
					layout = "whole"
					target = plugin.FileDirectoryPath(engine.ID, "outputs/scene")
					if engineType == "minio" {
						target = plugin.ObjectDirectoryPath(engine.ID, "outputs", "scene")
					}
				}
				itemType, fullName, err := authorizationfacts.MetadataTargetIdentity(target)
				if err != nil {
					t.Fatal(err)
				}
				facts := authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: engine.ID, ItemType: itemType, FullName: fullName, ItemFingerprint: commonmodels.GenerateItemFingerprint(engine.ID, fullName), ObservedAt: time.Now(), BasisState: authorizationfacts.AuthorizationBasisComplete, Boundary: &authorizationfacts.AuthorizationBoundary{Layout: layout, MetadataTarget: &authorizationfacts.MetadataBoundary{Target: target, Layout: layout, DataType: "container", Format: "geopackage", CompositionDigest: strings.Repeat("a", 64)}}}
				native := kind == "mysql" || kind == "mongodb"
				metaRaw, _ := json.Marshal(facts.Boundary.MetadataTarget)
				pathRaw, _ := json.Marshal(target)
				var sqlValid bool
				if err := db.Raw("SELECT system.valid_source_metadata_target(?::jsonb,?::jsonb,?,?)", string(metaRaw), string(pathRaw), engine.ID, facts.ItemFingerprint).Scan(&sqlValid).Error; err != nil || !sqlValid {
					t.Fatal("valid SQL metadata identity rejected", err)
				}
				for _, key := range []string{"layout", "data_type", "format", "composition_digest"} {
					var malformed map[string]any
					if err := json.Unmarshal(metaRaw, &malformed); err != nil {
						t.Fatal(err)
					}
					malformed[key] = nil
					invalidRaw, _ := json.Marshal(malformed)
					sqlValid = true
					if err := db.Raw("SELECT system.valid_source_metadata_target(?::jsonb,?::jsonb,?,?)", string(invalidRaw), string(pathRaw), engine.ID, facts.ItemFingerprint).Scan(&sqlValid).Error; err != nil || sqlValid {
						t.Fatal("SQL accepted null metadata identity", key, err)
					}
				}
				if native {
					facts.Boundary = &authorizationfacts.AuthorizationBoundary{Layout: "single", NativeTarget: target}
				}
				hash, err := facts.ComputeBoundaryHash()
				if err != nil {
					t.Fatal(err)
				}
				facts.BoundaryHash = &hash
				missingTarget := target
				missingTarget.Segments = append([]plugin.EngineCatalogSegment(nil), target.Segments...)
				missingTarget.Segments[len(missingTarget.Segments)-1].Name = "missing"
				missingFP, _ := authorizationfacts.PrimaryCandidateFingerprint(missingTarget)
				offline := false
				verifierCalls := 0
				repo := NewRepository(db)
				service := NewService(repo, func(*models.Engine) bool { return true }).WithAuthorizationFactsReader(bindingFactsReaderFunc(func(c context.Context, wanted uint, fp string) (*authorizationfacts.AuthorizationFacts, error) {
					if fp == missingFP {
						return nil, &client.TenantAPIError{StatusCode: http.StatusNotFound}
					}
					if wanted != tenant || fp != facts.ItemFingerprint {
						t.Fatal("wrong trusted Meta scope")
					}
					if offline {
						return nil, errors.New("private Meta failure")
					}
					copy := facts
					return &copy, nil
				})).WithIndependentTargetVerifier(independentVerifierFunc(func(context.Context, int64, plugin.EngineCatalogPath) (int64, error) {
					verifierCalls++
					return 0, ErrIndependentGrantSourceUnavailable
				}))
				reader := qualify(readerRole.ID)
				credential := metadataFixtureUserToken(t, db, reader)
				request := shared.SourceMetadataCheckRequest{Targets: []plugin.EngineCatalogPath{target}}
				check := func(want bool) {
					t.Helper()
					for _, fn := range []func(context.Context, string, shared.SourceMetadataCheckRequest) (*shared.SourceMetadataCheck, error){service.CheckMetaMetadataRead, service.CheckMetaLineageRead} {
						result, err := fn(ctx, credential, request)
						if err != nil || !result.Matches(request) || result.Targets[0].Visible != want {
							t.Fatalf("visible=%v result=%+v err=%v", want, result, err)
						}
					}
				}
				check(false)
				command := CreateIndependentGrantInput{Actor: actor, EngineID: int64(engine.ID), RequestID: uuid.New(), CatalogPath: target, ItemFingerprint: facts.ItemFingerprint, ExpectedBoundaryHash: hash, RecipientType: "user", RecipientID: reader.PrincipalID, Action: shared.SourceAccessMetadataRead, ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "View metadata only"}
				grant, err := service.CreateIndependentGrant(ctx, command)
				if err != nil {
					t.Fatal(err)
				}
				if ((!native && len(grant.MetadataTarget) == 0) || (native && len(grant.MetadataTarget) != 0)) || len(grant.FileComposition) > 0 || grant.Action != "metadata_read" || verifierCalls != 0 {
					t.Fatal("metadata Grant lost its exclusive boundary")
				}
				check(true)
				configureSearchObservationFixture(t, service)
				searchRequest := shared.SourceSearchCheckRequest{Targets: request.Targets, QueryNonce: "0123456789abcdef0123456789abcdef"}
				search, err := service.CheckManagerSearchRead(ctx, credential, searchRequest)
				if err != nil || !search.Matches(searchRequest) || !search.Targets[0].Visible || search.Targets[0].ContentReadable {
					t.Fatalf("metadata-only Grant reached content search: %+v %v", search, err)
				}
				mixed := shared.SourceMetadataCheckRequest{Targets: []plugin.EngineCatalogPath{missingTarget, target}}
				mixedResult, err := service.CheckMetaLineageRead(ctx, credential, mixed)
				if err != nil || !mixedResult.Matches(mixed) || mixedResult.Targets[0].Visible || !mixedResult.Targets[1].Visible {
					t.Fatal("missing target poisoned a visible target or disclosed existence", err)
				}
				absent := shared.SourceMetadataCheckRequest{Targets: []plugin.EngineCatalogPath{missingTarget}}
				missingResult, err := service.CheckMetaLineageRead(ctx, credential, absent)
				if err != nil || !missingResult.Matches(absent) || missingResult.Targets[0].Visible {
					t.Fatal("missing target was not independently invisible", err)
				}
				rows, total, err := repo.listAccountGrantRelations(ctx, tenantID, int64(engine.ID), reader.PrincipalID, 1, 100, SourceGrantFilter{})
				if err != nil || total != 1 || len(rows) != 1 || rows[0].Inspection == nil || !rows[0].Inspection.RuleCovered {
					t.Fatalf("metadata relation: %+v %d %v", rows, total, err)
				}
				binding, err := facts.MetadataBinding()
				if err != nil {
					t.Fatal(err)
				}
				if !native {
					if _, err := encodeBoundSourceTargets(request.Targets, []authorizationfacts.Binding{binding}, "read"); err == nil {
						t.Fatal("metadata identity encoded as content authorization")
					}
					if _, err := service.resolveReadBindings(ctx, tenantID, request.Targets); !errors.Is(err, ErrAuthorizationBoundaryChanged) {
						t.Fatal("content proof accepted metadata", err)
					}
				}
				if result, err := service.CheckManagerPreviewRead(ctx, credential, SourceReadCheckRequest{Targets: request.Targets}); result != nil || err == nil {
					t.Fatal("metadata Grant allowed preview", err)
				}
				readCommand := command
				readCommand.RequestID = uuid.New()
				readCommand.Action = "read"
				if _, err := service.CreateIndependentGrant(ctx, readCommand); err == nil {
					t.Fatal("unsupported content profile issued a read Grant")
				}
				if !native {
					stored, err := repo.findSourceGrant(ctx, grant.RequestID)
					if err != nil {
						t.Fatal(err)
					}
					invalid := *stored
					invalid.RequestID = uuid.New()
					invalid.Action = "read"
					if err := db.Create(&invalid).Error; err == nil {
						t.Fatal("SQL accepted metadata boundary for read")
					}
					original := *facts.Boundary.MetadataTarget
					changed := original
					changed.CompositionDigest = strings.Repeat("b", 64)
					facts.Boundary = &authorizationfacts.AuthorizationBoundary{Layout: layout, MetadataTarget: &changed}
					next, err := facts.ComputeBoundaryHash()
					if err != nil {
						t.Fatal(err)
					}
					facts.BoundaryHash = &next
					check(false)
					facts.Boundary = &authorizationfacts.AuthorizationBoundary{Layout: layout, MetadataTarget: &original}
					facts.BoundaryHash = &hash
					check(true)
				}
				offline = true
				if result, err := service.CheckMetaLineageRead(ctx, credential, request); result != nil || !errors.Is(err, ErrAuthorizationFactsUnavailable) {
					t.Fatal("Meta outage reused Allow", err)
				}
				offline = false
				if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(engine.ID), RequestID: grant.RequestID, Reason: "Withdraw metadata"}); err != nil {
					t.Fatal(err)
				}
				check(false)
				if kind == "nfs" {
					var expires time.Time
					if err := db.Raw("SELECT clock_timestamp()+interval '2 seconds'").Scan(&expires).Error; err != nil {
						t.Fatal(err)
					}
					command.RequestID = uuid.New()
					command.ExpiryMode = shared.SharingExpiryAtTime
					command.ExpiresAt = &expires
					if _, err := service.CreateIndependentGrant(ctx, command); err != nil {
						t.Fatal(err)
					}
					check(true)
					if err := db.Exec("SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM (?::timestamptz-clock_timestamp())))+0.03)", expires).Error; err != nil {
						t.Fatal(err)
					}
					check(false)
				}
			})
		}
	})
}

func metadataFixtureUserToken(t *testing.T, db *gorm.DB, user userProvenance) string {
	t.Helper()
	ctx := t.Context()
	repo := iam.NewRepository(db)
	tokens, err := iam.NewTokenFamilyService(repo, iam.BrowserSessionConfig{AccessTokenTTL: time.Hour, ResourceTicketOwners: []string{"manager"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := iam.NewContextSelectionService(repo, tokens)
	if err != nil {
		t.Fatal(err)
	}
	var now time.Time
	if err := db.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
		t.Fatal(err)
	}
	login, err := selection.BeginContextSelection(ctx, iam.BeginContextSelectionInput{PrincipalID: user.PrincipalID, Authentication: iam.SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: iam.AssuranceLevelAAL1, AuthenticatedAt: now}})
	if err != nil {
		t.Fatal(err)
	}
	session := login.Session
	if login.Challenge != nil {
		session, err = selection.ConsumeContextSelection(ctx, iam.ConsumeContextSelectionInput{SelectionTicket: login.Challenge.SelectionTicket, Choice: iam.ContextSelectionChoice{Type: iam.ContextTypeTenant, TenantMembershipID: &user.MembershipID}})
	}
	if err != nil || session == nil {
		t.Fatal("User session missing", err)
	}
	return session.AccessToken
}
