package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	systemauthorization "github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Formal System commands and real User credentials; only the Meta fact owner
// and metadata-only Provider are fixtures. This is not a real FTP content T4.
func exerciseFileCompositionGrants(t *testing.T, db *gorm.DB, tenantID, adminID int64,
	roles *iam.TenantRoleService, newUser func(*testing.T, time.Duration) (userProvenance, time.Time)) {
	t.Run("file composition source grants", func(t *testing.T) {
		t.Run("STL single-primary SQL contract", func(t *testing.T) { exerciseSTLCompositionSQL(t, db) })
		t.Run("glTF multi SQL contract", func(t *testing.T) { exerciseGLTFCompositionSQL(t, db) })
		for _, fixture := range []struct{ kind, format string }{{"ftp", "3ds"}, {"nfs", "3ds"}, {"ftp", "dae"}, {"nfs", "dae"}, {"minio", "3ds"}, {"minio", "dae"}, {"ftp", "obj"}, {"nfs", "obj"}, {"minio", "obj"}, {"ftp", "stl"}, {"nfs", "stl"}, {"minio", "stl"}, {"ftp", "glb"}, {"nfs", "glb"}, {"minio", "glb"}, {"ftp", "gltf"}, {"nfs", "gltf"}, {"minio", "gltf"}, {"ftp", "fbx"}, {"nfs", "fbx"}, {"minio", "fbx"}, {"nfs", "filegdb"}} {
			kind, format := fixture.kind, fixture.format
			t.Run(kind+"/"+format, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
				defer cancel()
				tenant := uint(tenantID)
				engine := models.Engine{Name: "File composition fixture " + format, EngineType: kind, TenantID: &tenant, Version: 1,
					LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{"host": format + ".file-composition.invalid"}, IdentityKey: models.JSONString(`{"host":"` + format + `.file-composition.invalid"}`)}
				if err := db.Table("system.engines").Create(&engine).Error; err != nil {
					t.Fatal(err)
				}
				targetFor := func(name string) plugin.EngineCatalogPath {
					if kind == "minio" {
						return plugin.ObjectItemPath(engine.ID, "models", name)
					}
					return plugin.FileItemPath(engine.ID, "models/"+name)
				}
				itemType := "file"
				if kind == "minio" {
					itemType = "object"
				}
				primary := targetFor("scene." + format)
				if format == "filegdb" {
					primary = plugin.FileDirectoryPath(engine.ID, "models/scene.gdb")
				}
				factsFor := func(texture string) authorizationfacts.AuthorizationFacts {
					refs := []authorizationfacts.FileCompositionRef{
						{Target: primary, Role: "model", Required: true, Primary: true},
						{Target: targetFor(texture), Role: "texture", Required: true},
					}
					if format == "stl" || format == "glb" || format == "fbx" {
						refs = refs[:1]
					}
					if format == "obj" {
						refs = append(refs, authorizationfacts.FileCompositionRef{Target: targetFor("model.mtl"), Role: "material_library", Required: true})
					}
					if format == "gltf" {
						refs[0].Role = "manifest"
						refs[1].Role = "image"
						refs = append(refs, authorizationfacts.FileCompositionRef{Target: targetFor("buffers/mesh.bin"), Role: "buffer", Required: true})
					}
					if format == "filegdb" {
						facts := authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: engine.ID, ItemType: "file", FullName: primary.StringPath(), ItemFingerprint: commonmodels.GenerateItemFingerprint(engine.ID, primary.StringPath()), BasisState: authorizationfacts.AuthorizationBasisComplete, ObservedAt: time.Now(), Boundary: &authorizationfacts.AuthorizationBoundary{Layout: "whole", Scope: &authorizationfacts.ScopeBoundary{Format: "filegdb", RuleVersion: 1, Root: primary}}}
						hash, err := facts.ComputeBoundaryHash()
						if err != nil {
							t.Fatal(err)
						}
						facts.BoundaryHash = &hash
						return facts
					}
					composition, err := authorizationfacts.NewFileCompositionBoundary(format, refs)
					if err != nil {
						t.Fatal(err)
					}
					facts := authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1,
						EngineID: engine.ID, ItemType: itemType, FullName: primary.StringPath(), ItemFingerprint: commonmodels.GenerateItemFingerprint(engine.ID, primary.StringPath()),
						BasisState: authorizationfacts.AuthorizationBasisComplete, ObservedAt: time.Now(), Boundary: &authorizationfacts.AuthorizationBoundary{Layout: composition.Layout(), FileComposition: composition}}
					hash, err := facts.ComputeBoundaryHash()
					if err != nil {
						t.Fatal(err)
					}
					facts.BoundaryHash = &hash
					return facts
				}
				current := factsFor("A.PNG")
				original := current
				offline := false
				repo := NewRepository(db)
				service := NewService(repo, func(engine *models.Engine) bool { return engine.EngineType == kind }).WithAuthorizationFactsReader(bindingFactsReaderFunc(func(_ context.Context, wantedTenant uint, fp string) (*authorizationfacts.AuthorizationFacts, error) {
					if wantedTenant != tenant || fp != current.ItemFingerprint {
						t.Fatal("wrong trusted Meta lookup")
					}
					if offline {
						return nil, errors.New("fixture Meta offline")
					}
					copy := current
					return &copy, nil
				})).WithIndependentTargetVerifier(independentVerifierFunc(func(c context.Context, wanted int64, target plugin.EngineCatalogPath) (int64, error) {
					if wanted != tenantID || target.StringPath() != primary.StringPath() {
						t.Fatal("wrong exact Provider target")
					}
					actual, err := repo.engine(c, tenantID, int64(engine.ID), false)
					if err != nil {
						return 0, err
					}
					return actual.Version, nil
				}))
				role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.file_grant_issuer_" + kind + "_" + format, Name: "File grant issuer",
					PermissionKeys: []string{"system.engine_access_delegation.create", "system.engine_access_grant.create", "system.engine_access_grant.read", "system.engine_access_grant.revoke", "system.engine_access_deny.create", "system.engine_access_deny.release"}, ActorPrincipalID: adminID})
				if err != nil {
					t.Fatal(err)
				}
				operator, _ := newUser(t, time.Hour)
				if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: operator.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: adminID, Reason: "Explicit file issuer fixture"}); err != nil {
					t.Fatal(err)
				}
				principal, err := iam.NewRepository(db).GetPrincipal(ctx, operator.PrincipalID)
				if err != nil {
					t.Fatal(err)
				}
				actor := Actor{TenantID: tenantID, PrincipalID: operator.PrincipalID, MembershipID: operator.MembershipID, AuthorizationVersion: principal.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Hour)}
				reader, _ := newUser(t, time.Hour)
				readRole, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.file_grant_reader_" + kind + "_" + format, Name: "File reader", PermissionKeys: []string{ManagerPreviewReadPermission, "manager.derived_artifact.create"}, ActorPrincipalID: adminID})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: reader.MembershipID, RoleIDs: []int64{readRole.ID}, ActorPrincipalID: adminID, Reason: "Read without issuance"}); err != nil {
					t.Fatal(err)
				}
				identity := iam.NewRepository(db)
				tokens, err := iam.NewTokenFamilyService(identity, iam.BrowserSessionConfig{AccessTokenTTL: time.Hour, ResourceTicketOwners: []string{"manager"}}, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				selection, err := iam.NewContextSelectionService(identity, tokens)
				if err != nil {
					t.Fatal(err)
				}
				var authenticatedAt time.Time
				if err := db.WithContext(ctx).Raw("SELECT clock_timestamp()").Scan(&authenticatedAt).Error; err != nil {
					t.Fatal(err)
				}
				login, err := selection.BeginContextSelection(ctx, iam.BeginContextSelectionInput{PrincipalID: reader.PrincipalID,
					Authentication: iam.SessionAuthentication{Methods: []string{"password"}, AssuranceLevel: iam.AssuranceLevelAAL1, AuthenticatedAt: authenticatedAt}})
				if err != nil {
					t.Fatal(err)
				}
				session := login.Session
				if login.Challenge != nil {
					session, err = selection.ConsumeContextSelection(ctx, iam.ConsumeContextSelectionInput{SelectionTicket: login.Challenge.SelectionTicket, Choice: iam.ContextSelectionChoice{Type: iam.ContextTypeTenant, TenantMembershipID: &reader.MembershipID}})
				}
				if err != nil || session == nil {
					t.Fatal("real User session missing", err)
				}
				request := SourceReadCheckRequest{Targets: []plugin.EngineCatalogPath{primary}}
				if current.Boundary.FileComposition != nil {
					request.ExpectedSource = &client.ManagerModel3DExpectedSource{EngineVersion: "1", FileComposition: *current.Boundary.FileComposition.Clone()}
				}
				read := func(request SourceReadCheckRequest, want bool) {
					t.Helper()
					result, err := service.CheckManagerPreviewRead(ctx, session.AccessToken, request)
					if want && (err != nil || result == nil) {
						t.Fatalf("read denied: %v", err)
					}
					if !want && (result != nil || !errors.Is(err, commonapi.ErrForbidden)) {
						t.Fatalf("unexpected rejection/result: %v %+v", err, result)
					}
				}
				read(request, false)
				input := CreateIndependentGrantInput{Actor: actor, EngineID: int64(engine.ID), RequestID: uuid.New(), CatalogPath: primary,
					ItemFingerprint: current.ItemFingerprint, ExpectedBoundaryHash: *current.BoundaryHash, DirectPermissionVersion: 1, DirectPermissionSource: "explicit",
					RecipientType: "user", RecipientID: reader.PrincipalID, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Approve complete A composition"}
				binding, err := current.ApprovedBinding()
				if err != nil {
					t.Fatal(err)
				}
				if err := repo.transaction(ctx, func(tx *Repository) error {
					permitInput := fixturePermitBindingInput(ctx, tx, tenantID, adminID, primary, binding, true, 0)
					_, err := fixtureSetPermit(ctx, tx, permitInput, func(*Repository) error { return nil })
					return err
				}); err != nil {
					t.Fatal(err)
				}
				grantA, err := service.CreateIndependentGrant(ctx, input)
				if err != nil {
					t.Fatal(err)
				}
				var stored authorizationfacts.FileCompositionBoundary
				expectedRefs := 2
				if format == "gltf" {
					expectedRefs = 3
				}
				if format == "stl" || format == "glb" || format == "fbx" {
					expectedRefs = 1
				}
				if format == "obj" {
					expectedRefs = 3
				}
				if format == "filegdb" {
					var scope authorizationfacts.ScopeBoundary
					if json.Unmarshal(grantA.ScopeBoundary, &scope) != nil || scope.Root.StringPath() != primary.StringPath() || len(grantA.FileComposition) > 0 {
						t.Fatal("whole boundary not persisted exclusively")
					}
				} else if json.Unmarshal(grantA.FileComposition, &stored) != nil || len(stored.Refs) != expectedRefs {
					t.Fatal("approved composition not persisted")
				}
				read(request, true)
				if format != "glb" && format != "filegdb" {
					exerciseManagerModel3DAuthorization(t, db, service, tenantID, reader, session.AccessToken, actor, engine.ID, format+".file-composition.invalid", current)
				}
				// A primary-only row cannot become a full composition Grant.
				bare, err := prepareIndependentGrant(input)
				if err != nil {
					t.Fatal(err)
				}
				bare.RequestID = uuid.New()
				if err := repo.insertSourceGrant(ctx, bare); !errors.Is(err, commonapi.ErrBadRequest) {
					t.Fatal("primary-only upgrade accepted", err)
				}
				if format == "stl" || format == "glb" || format == "fbx" || format == "filegdb" {
					current.BasisState, current.Boundary, current.BoundaryHash = authorizationfacts.AuthorizationBasisIncomplete, nil, nil
					if result, err := service.CheckManagerPreviewRead(ctx, session.AccessToken, request); result != nil || !errors.Is(err, commonapi.ErrConflict) {
						t.Fatal("incomplete STL facts did not fail closed", err)
					}
					current = original
					read(request, true)
					denyID := uuid.New()
					if _, err := service.CreateDeny(ctx, CreateDenyInput{Actor: actor, EngineID: int64(engine.ID), DenyID: denyID, CatalogPath: primary, ItemFingerprint: original.ItemFingerprint,
						RecipientType: "user", RecipientID: reader.PrincipalID, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Deny exact STL"}); err != nil {
						t.Fatal(err)
					}
					read(request, false)
					if _, err := service.ReleaseDeny(ctx, ReleaseDenyInput{Actor: actor, EngineID: int64(engine.ID), DenyID: denyID, Reason: "Release STL denial"}); err != nil {
						t.Fatal(err)
					}
					read(request, true)
					if err := db.Table("system.engines").Where("id=?", engine.ID).Update("version", 2).Error; err != nil {
						t.Fatal(err)
					}
					if request.ExpectedSource != nil {
						read(request, false)
						request.ExpectedSource.EngineVersion = "2"
					}
					read(request, true)
					offline = true
					if result, err := service.CheckManagerPreviewRead(ctx, session.AccessToken, request); result != nil || !errors.Is(err, ErrAuthorizationFactsUnavailable) {
						t.Fatal("Meta outage allowed STL", err)
					}
					offline = false
					if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(engine.ID), RequestID: grantA.RequestID, Reason: "Withdraw STL"}); err != nil {
						t.Fatal(err)
					}
					read(request, false)
					if format == "filegdb" {
						var expires time.Time
						if err := db.Raw("SELECT clock_timestamp()+interval '2 seconds'").Scan(&expires).Error; err != nil {
							t.Fatal(err)
						}
						input.RequestID, input.ExpiryMode, input.ExpiresAt = uuid.New(), shared.SharingExpiryAtTime, &expires
						if _, err := service.CreateIndependentGrant(ctx, input); err != nil {
							t.Fatal("expiring whole grant", err)
						}
						read(request, true)
						if err := db.Exec("SELECT pg_sleep(GREATEST(0, EXTRACT(EPOCH FROM (?::timestamptz-clock_timestamp())))+0.03)", expires).Error; err != nil {
							t.Fatal(err)
						}
						read(request, false)
					}
					return
				}
				current = factsFor("B.PNG")
				currentRequest := SourceReadCheckRequest{Targets: []plugin.EngineCatalogPath{primary}, ExpectedSource: &client.ManagerModel3DExpectedSource{EngineVersion: "1", FileComposition: *current.Boundary.FileComposition.Clone()}}
				read(currentRequest, false) // old A Grant alone does not cover B, even without a Deny.
				read(request, false)
				current = original
				denyID := uuid.New()
				if _, err := service.CreateDeny(ctx, CreateDenyInput{Actor: actor, EngineID: int64(engine.ID), DenyID: denyID, CatalogPath: primary, ItemFingerprint: original.ItemFingerprint,
					RecipientType: "user", RecipientID: reader.PrincipalID, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Persistent same-item denial"}); err != nil {
					t.Fatal(err)
				}
				current = factsFor("B.PNG")
				read(request, false)
				if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(engine.ID), RequestID: grantA.RequestID, Reason: "Withdraw obsolete A"}); err != nil {
					t.Fatal(err)
				}
				read(currentRequest, false)
				input.RequestID, input.ExpectedBoundaryHash, input.Reason = uuid.New(), *current.BoundaryHash, "Explicitly approve B composition"
				grantB, err := service.CreateIndependentGrant(ctx, input)
				if err != nil {
					t.Fatal(err)
				}
				read(currentRequest, false) // old A's Deny still constrains B's new Grant.
				if _, err := service.ReleaseDeny(ctx, ReleaseDenyInput{Actor: actor, EngineID: int64(engine.ID), DenyID: denyID, Reason: "Explicitly release same-item denial"}); err != nil {
					t.Fatal(err)
				}
				read(currentRequest, true)
				read(request, false) // B's active Grant must not authorize A's cached GLB.
				delegation, err := iam.NewDelegationService(identity, systemauthorization.ToolAuthorizationCatalog{}, iam.DelegationServiceConfig{})
				if err != nil {
					t.Fatal(err)
				}
				delegate, err := delegation.IssueDelegatedAccessToken(ctx, iam.IssueDelegatedAccessTokenInput{SourceAccessToken: session.AccessToken, Audience: "manager", Scopes: []string{"data.preview"}, AgentRunID: uuid.NewString(), ToolCallID: uuid.NewString()})
				if err != nil {
					t.Fatal(err)
				}
				if result, err := service.CheckManagerPreviewRead(ctx, delegate.AccessToken, currentRequest); result != nil || (!errors.Is(err, commonapi.ErrUnauthorized) && !errors.Is(err, commonapi.ErrForbidden)) {
					t.Fatal("raw delegated managed-result read accepted", err)
				}
				if err := db.Table("system.engines").Where("id=?", engine.ID).Update("version", 2).Error; err != nil {
					t.Fatal(err)
				}
				read(currentRequest, false)
				currentRequest.ExpectedSource.EngineVersion = "2"
				read(currentRequest, true)
				current.ObservedAt = time.Now() // re-observation is not a changed boundary.
				read(currentRequest, true)
				offline = true
				if result, err := service.CheckManagerPreviewRead(ctx, session.AccessToken, currentRequest); result != nil || !errors.Is(err, ErrAuthorizationFactsUnavailable) {
					t.Fatal("Meta outage allowed cached composition", err)
				}
				offline = false
				if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(engine.ID), RequestID: grantB.RequestID, Reason: "Withdraw B"}); err != nil {
					t.Fatal(err)
				}
				read(currentRequest, false)
				prior, err := repo.findSourceGrant(ctx, grantA.RequestID)
				if err != nil || !equalJSON(prior.FileComposition, grantA.FileComposition) || prior.BoundaryHash != *original.BoundaryHash {
					t.Fatal("old approved composition was overwritten", err)
				}
			})
		}
	})
}
