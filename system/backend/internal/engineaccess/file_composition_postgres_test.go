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
	"github.com/addp/common/execution"
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
		exerciseMetadataOnlyGrants(t, db, tenantID, adminID, roles, newUser)
		t.Run("STL single-primary SQL contract", func(t *testing.T) { exerciseSTLCompositionSQL(t, db) })
		t.Run("glTF multi SQL contract", func(t *testing.T) { exerciseGLTFCompositionSQL(t, db) })
		t.Run("TIFF PAM SQL contract", func(t *testing.T) { exerciseTIFFCompositionSQL(t, db) })
		for _, fixture := range []struct{ kind, format string }{{"ftp", "3ds"}, {"nfs", "3ds"}, {"ftp", "dae"}, {"nfs", "dae"}, {"minio", "3ds"}, {"minio", "dae"}, {"ftp", "obj"}, {"nfs", "obj"}, {"minio", "obj"}, {"ftp", "stl"}, {"nfs", "stl"}, {"minio", "stl"}, {"ftp", "glb"}, {"nfs", "glb"}, {"minio", "glb"}, {"ftp", "gltf"}, {"nfs", "gltf"}, {"minio", "gltf"}, {"ftp", "fbx"}, {"nfs", "fbx"}, {"minio", "fbx"}, {"nfs", "filegdb"}, {"nfs", "envi"}, {"minio", "envi"}, {"nfs", "geopackage"}, {"ftp", "geopackage"}, {"nfs", "tiff"}, {"minio", "tiff"}, {"minio", "geojson"}} {
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
				if format == "geopackage" {
					primary = targetFor("scene.gpkg")
				}
				if format == "tiff" {
					primary = targetFor("scene.tif")
				}
				if format == "envi" {
					primary = targetFor("scene.img")
				}
				if format == "filegdb" {
					primary = plugin.FileDirectoryPath(engine.ID, "models/scene.gdb")
				}
				factsFor := func(texture string) authorizationfacts.AuthorizationFacts {
					if format == "geopackage" {
						return *developGeoPackageFacts(t, primary)
					}
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
					if format == "envi" {
						refs[0].Role = "raster"
						refs[1].Role = "header"
						name := "scene.hdr"
						if texture == "B.PNG" {
							name = "scene.img.hdr"
						}
						refs[1].Target = targetFor(name)
					}
					layout := ""
					if format == "geojson" {
						refs = refs[:1]
						refs[0].Role = "data"
					}
					if format == "tiff" {
						refs = refs[:1]
						refs[0].Role = "image"
						layout = "single"
						if texture == "B.PNG" {
							layout = "multi"
							refs = append(refs, authorizationfacts.FileCompositionRef{Target: targetFor("scene.tif.aux.xml"), Role: "auxiliary_metadata"})
						}
					}
					composition, err := authorizationfacts.NewScannedFileCompositionBoundary(format, layout, refs)
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
					PermissionKeys: []string{"system.engine_access_delegation.create", "system.engine_access_grant.create", "system.engine_access_grant.read", "system.engine_access_grant.revoke"}, ActorPrincipalID: adminID})
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
				readerPermissions := []string{ManagerPreviewReadPermission, "manager.derived_artifact.create"}
				if format == "geojson" {
					readerPermissions = append(readerPermissions, "develop.task.execute", "develop.data_read.execute")
				}
				readRole, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.file_grant_reader_" + kind + "_" + format, Name: "File reader", PermissionKeys: readerPermissions, ActorPrincipalID: adminID})
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
				if format == "geopackage" {
					exerciseGeoPackageGrant(t, db, service, ctx, actor, reader, kind, primary, &current)
					return
				}
				request := SourceReadCheckRequest{Targets: []plugin.EngineCatalogPath{primary}}
				if current.Boundary.FileComposition != nil {
					request.ExpectedSource = &client.ManagerFileExpectedSource{EngineVersion: "1", FileComposition: *current.Boundary.FileComposition.Clone()}
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
					ItemFingerprint: current.ItemFingerprint, ExpectedBoundaryHash: *current.BoundaryHash,
					RecipientType: "user", RecipientID: reader.PrincipalID, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Approve complete A composition"}
				grantA, err := service.CreateIndependentGrant(ctx, input)
				if err != nil {
					t.Fatal(err)
				}
				var stored authorizationfacts.FileCompositionBoundary
				expectedRefs := 2
				if format == "gltf" {
					expectedRefs = 3
				}
				if format == "stl" || format == "glb" || format == "fbx" || format == "tiff" || format == "geojson" {
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
				if format == "geojson" {
					exerciseWorkflowGeoJSONAccess(t, db, service, tenantID, reader, current)
					return
				}
				if format != "glb" && format != "filegdb" && format != "tiff" {
					exerciseManagerFileAuthorization(t, db, service, tenantID, reader, session.AccessToken, actor, engine.ID, format+".file-composition.invalid", current)
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
				currentRequest := SourceReadCheckRequest{Targets: []plugin.EngineCatalogPath{primary}, ExpectedSource: &client.ManagerFileExpectedSource{EngineVersion: "1", FileComposition: *current.Boundary.FileComposition.Clone()}}
				read(currentRequest, false) // old A Grant alone does not cover B.
				read(request, false)
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
				read(currentRequest, true)
				if format == "tiff" {
					exerciseManagerFileAuthorization(t, db, service, tenantID, reader, session.AccessToken, actor, engine.ID, format+".file-composition.invalid", current)
				}
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

func exerciseGeoPackageGrant(t *testing.T, db *gorm.DB, service *Service, ctx context.Context, actor Actor, reader userProvenance,
	kind string, target plugin.EngineCatalogPath, current *authorizationfacts.AuthorizationFacts) {
	t.Helper()
	input := CreateIndependentGrantInput{Actor: actor, EngineID: int64(target.EngineID), RequestID: uuid.New(), CatalogPath: target,
		ItemFingerprint: current.ItemFingerprint, ExpectedBoundaryHash: *current.BoundaryHash,
		RecipientType: "user", RecipientID: reader.PrincipalID, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Approve complete GeoPackage"}
	if kind != "nfs" {
		if result, err := service.CreateIndependentGrant(ctx, input); result != nil || !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("non-NFS GeoPackage Grant accepted or misclassified: %v %v", result, err)
		}
		return
	}

	principal, err := iam.NewRepository(db).GetPrincipal(ctx, reader.PrincipalID)
	if err != nil {
		t.Fatal(err)
	}
	auth := &iam.ExecutionAuthorization{Audience: execution.AudienceDevelop, SourceType: "user", TenantID: actor.TenantID,
		ActorPrincipalID: reader.PrincipalID, TenantMembershipID: reader.MembershipID, IssuedAuthorizationVersion: principal.AuthorizationVersion}
	intent, err := execution.NewDevelopFileIntent([]byte(`{"fixture":true}`), 1, 45, []execution.DevelopFileSourceIntent{{NodeID: "load", Target: target}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	check := func(want error) {
		t.Helper()
		prepared, err := service.PrepareDevelopFileRead(ctx, auth, *intent)
		if err == nil {
			if len(prepared.Scope.Sources) != 1 || prepared.Scope.Sources[0].BoundaryHash != input.ExpectedBoundaryHash {
				t.Fatal("unexpected sealed source")
			}
			sealed := *auth
			sealed.DevelopFileScope = prepared.Scope.Clone()
			err = db.Transaction(func(tx *gorm.DB) error {
				_, err := prepared.Verify(ctx, tx, &sealed, nil)
				return err
			})
		}
		if !errors.Is(err, want) {
			t.Fatalf("Develop read qualification: got %v, want %v", err, want)
		}
	}
	check(commonapi.ErrForbidden)
	grant, err := service.CreateIndependentGrant(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	var stored authorizationfacts.FileCompositionBoundary
	if err := json.Unmarshal(grant.FileComposition, &stored); err != nil || stored.Validate(target.EngineID) != nil || stored.Format != "geopackage" {
		t.Fatal("GeoPackage composition was not persisted", err)
	}
	check(nil)
	original := *current
	current.BasisState, current.Boundary, current.BoundaryHash = authorizationfacts.AuthorizationBasisIncomplete, nil, nil
	check(ErrAuthorizationBoundaryChanged)
	*current = original
	if _, err := service.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(target.EngineID), RequestID: grant.RequestID, Reason: "Withdraw GeoPackage"}); err != nil {
		t.Fatal(err)
	}
	check(commonapi.ErrForbidden)

	ref := stored.Refs[0]
	for _, invalid := range []struct {
		name string
		path plugin.EngineCatalogPath
		refs []authorizationfacts.FileCompositionRef
	}{
		{"object", plugin.ObjectItemPath(target.EngineID, "files", "scene.gpkg"), []authorizationfacts.FileCompositionRef{{Target: plugin.ObjectItemPath(target.EngineID, "files", "scene.gpkg"), Role: "container", Required: true, Primary: true}}},
		{"wrong role", target, []authorizationfacts.FileCompositionRef{{Target: target, Role: "model", Required: true, Primary: true}}},
		{"optional", target, []authorizationfacts.FileCompositionRef{{Target: target, Role: "container", Primary: true}}},
		{"not primary", target, []authorizationfacts.FileCompositionRef{{Target: target, Role: "container", Required: true}}},
		{"different target", target, []authorizationfacts.FileCompositionRef{{Target: plugin.FileItemPath(target.EngineID, "models/other.gpkg"), Role: "container", Required: true, Primary: true}}},
		{"sidecar", target, []authorizationfacts.FileCompositionRef{ref, {Target: plugin.FileItemPath(target.EngineID, "models/scene.gpkg-wal"), Role: "journal", Required: true}}},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			stored.Refs = invalid.refs
			raw, _ := json.Marshal(stored)
			pathJSON, _ := json.Marshal(invalid.path)
			var valid bool
			if err := db.Raw("SELECT system.valid_source_file_composition(?::jsonb, ?::jsonb, ?) IS TRUE", string(raw), string(pathJSON), target.EngineID).Scan(&valid).Error; err != nil || valid {
				t.Fatalf("malformed GeoPackage boundary accepted: valid=%v err=%v", valid, err)
			}
		})
	}
}
