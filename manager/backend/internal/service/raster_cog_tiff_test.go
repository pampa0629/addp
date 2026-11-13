package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	ftpPlugin "github.com/addp/common/engine/plugins/ftp"
	"github.com/addp/common/engine/workflowaccess"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

type tiffFactsFixture struct {
	rasterSnapshotFixture
	facts    *authorizationfacts.AuthorizationFacts
	queries  int
	factsErr error
}

func (f *tiffFactsFixture) ReadAuthorizationFacts(_ context.Context, tenant uint, fingerprints []string) (*authorizationfacts.AuthorizationFactsBatch, error) {
	f.queries++
	if f.factsErr != nil {
		return nil, f.factsErr
	}
	if tenant != 7 || !reflect.DeepEqual(fingerprints, []string{f.item.Fingerprint}) {
		return nil, ErrFileSourceAuthorizationRequired
	}
	return &authorizationfacts.AuthorizationFactsBatch{Data: []authorizationfacts.AuthorizationFactsResult{{ItemFingerprint: f.item.Fingerprint, Facts: f.facts}}}, nil
}

func tiffCOGFixture(t *testing.T, engineType string, pam bool, extraRoles ...string) (*tiffFactsFixture, *commonModels.Engine, commonModels.JSONMap, map[string][]byte) {
	t.Helper()
	tenant := uint(7)
	main := "images/原名 %.tif"
	target := plugin.FileItemPath(26, main)
	if engineType == "minio" {
		target = plugin.ObjectItemPath(26, "images", "原名 %.tif")
	}
	refs := []authorizationfacts.FileCompositionRef{{Target: target, Role: "image", Primary: true, Required: true}}
	files := map[string][]byte{main: []byte("TIFF exact frozen bytes")}
	itemRefs := []any{map[string]any{"path": main, "role": "main", "primary": true, "required": true}}
	layout := "single"
	if pam {
		layout = "multi"
		aux := plugin.FileItemPath(26, main+".aux.xml")
		if engineType == "minio" {
			aux = plugin.ObjectItemPath(26, "images", "原名 %.tif.aux.xml")
		}
		refs = append(refs, authorizationfacts.FileCompositionRef{Target: aux, Role: "auxiliary_metadata"})
		files[main+".aux.xml"] = []byte("<PAMDataset/>")
		itemRefs = append(itemRefs, map[string]any{"path": main + ".aux.xml", "role": "auxiliary_metadata", "required": false, "primary": false})
	}
	for _, role := range extraRoles {
		layout = "multi"
		name := map[string]string{"world_file": "images/原名 %.tfw", "header": "images/原名 %.hdr", "crs": "images/原名 %.prj", "overview": main + ".ovr", "mask": main + ".msk"}[role]
		target := plugin.FileItemPath(26, name)
		if engineType == "minio" {
			target = plugin.ObjectItemPath(26, "images", strings.TrimPrefix(name, "images/"))
		}
		refs = append(refs, authorizationfacts.FileCompositionRef{Target: target, Role: role})
		files[name] = []byte("exact " + role)
		itemRefs = append(itemRefs, map[string]any{"path": name, "role": role, "required": false, "primary": false})
	}
	versions := []dataitem.ContentVersion{}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if raw, ok := files[name]; ok {
			digest := sha256.Sum256(raw)
			versions = append(versions, dataitem.ContentVersion{Path: name, SizeBytes: int64(len(raw)), SHA256: hex.EncodeToString(digest[:])})
		}
	}
	composition, err := authorizationfacts.NewScannedFileCompositionBoundary("tiff", layout, refs)
	if err != nil {
		t.Fatal(err)
	}
	item := &commonModels.MetaItem{ID: 100, TenantID: tenant, EngineID: 26, Name: "原名 %.tif", ItemType: "file", FullName: main, Fingerprint: commonModels.GenerateItemFingerprint(26, main), Attributes: commonModels.JSONMap{
		"item":         map[string]interface{}{"format": "tiff", "data_type": "media", "layout": layout, "refs": itemRefs},
		"storage":      map[string]interface{}{"physical_path": main, "content_versions": versions},
		"type_info":    map[string]interface{}{"media": map[string]interface{}{"width": 256, "height": 128, "band_count": 3, "size_bytes": len(files[main])}},
		"format_info":  map[string]interface{}{"tiff": map[string]interface{}{"profile": "geotiff"}},
		"capabilities": map[string]interface{}{"spatial": map[string]interface{}{"srid": 4326, "extent": []float64{110, 20, 120, 30}}},
	}}
	if engineType == "minio" {
		item.ItemType = "object"
	}
	facts := &authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: 26, ItemType: item.ItemType, FullName: main, ItemFingerprint: item.Fingerprint, BasisState: authorizationfacts.AuthorizationBasisComplete, ObservedAt: time.Now().UTC(), Boundary: &authorizationfacts.AuthorizationBoundary{Layout: layout, FileComposition: composition}}
	hash, err := facts.ComputeBoundaryHash()
	if err != nil {
		t.Fatal(err)
	}
	facts.BoundaryHash = &hash
	locator := "addp://engine/26/path/images/原名%20%25.tif?type=file&item_id=100"
	cfg := commonModels.JSONMap{"target": commonModels.JSONMap{"source_engine_id": 26, "item_id": 100, "item_fingerprint": item.Fingerprint, "locator": locator}, "raster": commonModels.JSONMap{"source_format": "tiff", "source_profile": "geotiff", "source_size_bytes": len(files[main]), "content_versions": versions, "width": 256, "height": 128, "band_count": 3, "source_srid": 4326, "extent_srid": 4326, "extent": []float64{110, 20, 120, 30}}}
	engine := &commonModels.Engine{ID: 26, TenantID: &tenant, EngineType: engineType, Version: 5, LifecycleState: "active", ConnectionStatus: "online", ConnectionInfo: commonModels.ConnectionInfo{"password": "current-consumption-connection"}}
	return &tiffFactsFixture{rasterSnapshotFixture: rasterSnapshotFixture{item: item}, facts: facts}, engine, cfg, files
}

func freezeTIFFFixture(t *testing.T, meta *tiffFactsFixture, engine *commonModels.Engine, config commonModels.JSONMap) (*RasterCOGTaskService, *commonExecution.TaskExecution) {
	t.Helper()
	svc := NewRasterCOGTaskService(nil)
	svc.SetSourceAuthorizationClients(meta, &glbSourceEngineFixture{engine: engine}, &commonClient.SystemExecutionAuthorizationClient{}, &commonClient.SystemServiceClient{})
	raw, _ := json.Marshal(config)
	execution := &commonExecution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Source: "manager", TriggerType: "manual"}
	ctx := commonExecution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "31", nil))
	managed, err := svc.freezeRasterSourceExecution(ctx, &repository.FileTaskSnapshot{TaskID: 11, TenantID: 7, Version: 3, Config: raw}, execution, "addp_at_human")
	if err != nil || !managed {
		t.Fatal("exact TIFF facts did not freeze", err)
	}
	return svc, execution
}

func TestTIFFCOGFreezesFormalMetaCompositionAndEveryContentVersion(t *testing.T) {
	for _, pam := range []bool{false, true} {
		meta, engine, config, _ := tiffCOGFixture(t, "nfs", pam)
		_, execution := freezeTIFFFixture(t, meta, engine, config)
		raw, _ := json.Marshal(execution.ExecutionConfig)
		scope, err := commonExecution.NewManagerFileReadScope(raw, "11", "3", "5", meta.facts.Boundary.FileComposition)
		if err != nil || scope.Purpose() != commonExecution.TaskTypeRasterCOGGeneration || meta.queries != 1 {
			t.Fatal("formal composition was not consumed", err)
		}
		versions := meta.item.Attributes["storage"].(map[string]interface{})["content_versions"].([]dataitem.ContentVersion)
		changed := append([]dataitem.ContentVersion(nil), versions...)
		changed[len(changed)-1].SHA256 = strings.Repeat("c", 64)
		object := execution.ExecutionConfig.Clone()
		object["raster"].(commonModels.JSONMap)["content_versions"] = changed
		modified, _ := json.Marshal(object)
		other, err := commonExecution.NewManagerFileReadScope(modified, "11", "3", "5", meta.facts.Boundary.FileComposition)
		if err != nil || other.ConfigDigest == scope.ConfigDigest {
			t.Fatal("component version escaped execution digest", err)
		}
		cfg, err := readRasterCOGExecutionConfig(object, "manager", 7)
		if err != nil || validateRasterSourceConfig(meta.item, cfg.Raster) == nil {
			t.Fatal("changed component version accepted")
		}
	}
}

func TestTIFFCacheRequiresCompleteCurrentComponentVersions(t *testing.T) {
	meta, _, _, _ := tiffCOGFixture(t, "nfs", true)
	versions, err := dataitem.TIFFContentVersionsFromAttributes(meta.item.Attributes)
	if err != nil {
		t.Fatal(err)
	}
	current := &RasterQuickViewSource{Format: "tiff", ContentVersions: versions}
	result := &models.RasterCOG{Status: models.RasterCOGStatusReady, Metadata: commonModels.JSONMap{"source_version": commonModels.JSONMap{"content_versions": versions}}}
	if !rasterCOGFactsMatch(result, QuickViewIdentity{}, current, 0) {
		t.Fatal("same source invalidated")
	}
	for _, fault := range []string{"PAM changed", "PAM removed", "missing versions", "main changed"} {
		t.Run(fault, func(t *testing.T) {
			changed := append([]dataitem.ContentVersion(nil), versions...)
			switch fault {
			case "PAM changed":
				changed[1].SHA256 = strings.Repeat("a", 64)
			case "PAM removed":
				changed = changed[:1]
			case "missing versions":
				changed = nil
			case "main changed":
				changed[0].SHA256 = strings.Repeat("b", 64)
			}
			if rasterCOGFactsMatch(result, QuickViewIdentity{}, &RasterQuickViewSource{Format: "tiff", ContentVersions: changed}, 0) {
				t.Fatal("stale COG reused")
			}
		})
	}
}

func TestTIFFCacheIncludesNonPAMAuxiliaryVersions(t *testing.T) {
	meta, _, _, _ := tiffCOGFixture(t, "nfs", true, "world_file", "header")
	versions, err := dataitem.TIFFContentVersionsFromAttributes(meta.item.Attributes)
	if err != nil {
		t.Fatal(err)
	}
	result := &models.RasterCOG{Status: models.RasterCOGStatusReady, Metadata: commonModels.JSONMap{"source_version": commonModels.JSONMap{"content_versions": versions}}}
	if !rasterCOGFactsMatch(result, QuickViewIdentity{}, &RasterQuickViewSource{Format: "tiff", ContentVersions: versions}, 0) {
		t.Fatal("unchanged four-member version rejected")
	}
	for i, version := range versions {
		if !strings.HasSuffix(version.Path, ".hdr") && !strings.HasSuffix(version.Path, ".tfw") {
			continue
		}
		changed := append([]dataitem.ContentVersion(nil), versions...)
		changed[i].SHA256 = strings.Repeat("e", 64)
		if rasterCOGFactsMatch(result, QuickViewIdentity{}, &RasterQuickViewSource{Format: "tiff", ContentVersions: changed}, 0) {
			t.Fatal("non-PAM content edit reused stale COG", version.Path)
		}
		removed := append(append([]dataitem.ContentVersion(nil), versions[:i]...), versions[i+1:]...)
		if rasterCOGFactsMatch(result, QuickViewIdentity{}, &RasterQuickViewSource{Format: "tiff", ContentVersions: removed}, 0) {
			t.Fatal("removed registered member reused stale COG", version.Path)
		}
	}
}

func TestTIFFPAMRequiresCOGEvenInsideClientBudget(t *testing.T) {
	meta, _, _, _ := tiffCOGFixture(t, "nfs", true)
	raster := RasterQuickViewSourceFromAttributes(meta.item.Attributes, "addp://engine/26/path/images/scene.tif?type=file", 26)
	if raster == nil || rasterUnavailableReason(raster, 50*1024*1024, 64000000) != RasterUnavailableReasonRequiresCOGGeneration {
		t.Fatal("multi TIFF bypassed PAM via direct client rendering", raster)
	}
}

func TestTIFFCOGReadRechecksCurrentHumanAndMetaVersions(t *testing.T) {
	for _, fault := range []string{"none", "permission expired", "PAM version changed", "composition changed", "non human", "Meta unavailable"} {
		t.Run(fault, func(t *testing.T) {
			meta, engine, config, _ := tiffCOGFixture(t, "nfs", true)
			svc, execution := freezeTIFFFixture(t, meta, engine, config)
			raw, _ := json.Marshal(execution.ExecutionConfig)
			scope, err := commonExecution.NewManagerFileReadScope(raw, "11", "3", "5", meta.facts.Boundary.FileComposition)
			if err != nil {
				t.Fatal(err)
			}
			versions, _ := dataitem.TIFFContentVersionsFromAttributes(meta.item.Attributes)
			itemID := uint(100)
			result := &models.RasterCOG{TenantID: 7, SourceEngineID: 26, ItemID: &itemID, ItemFingerprint: meta.item.Fingerprint,
				Locator: config["target"].(commonModels.JSONMap)["locator"].(string), Metadata: commonModels.JSONMap{
					"file_source_scope": scope, "source_version": commonModels.JSONMap{"content_versions": versions},
				}}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/v1/system/engine-access/read-observations/current-source" || r.Header.Get("Authorization") != "Bearer addp_at_current_human" {
					t.Error("wrong current Human protocol")
				}
				var body struct {
					ExpectedSource commonClient.ManagerFileExpectedSource `json:"expected_source"`
				}
				if json.NewDecoder(r.Body).Decode(&body) != nil || !reflect.DeepEqual(body.ExpectedSource.FileComposition, scope.FileComposition) {
					t.Error("lost complete generated source")
				}
				if fault == "permission expired" {
					w.WriteHeader(403)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"observed_at": time.Now().UTC()})
			}))
			defer server.Close()
			svc.sourceConsumer = commonClient.NewSystemServiceClient(server.URL, ftpModelServiceTokens{}, server.Client())
			if fault == "PAM version changed" {
				changed := append([]dataitem.ContentVersion(nil), versions...)
				changed[1].SHA256 = strings.Repeat("d", 64)
				meta.item.Attributes["storage"].(map[string]interface{})["content_versions"] = changed
			}
			if fault == "composition changed" {
				meta.facts.Boundary.FileComposition.Refs[1].Required = true
				hash, err := meta.facts.ComputeBoundaryHash()
				if err != nil {
					t.Fatal(err)
				}
				meta.facts.BoundaryHash = &hash
			}
			if fault == "Meta unavailable" {
				meta.factsErr = errors.New("Meta unavailable")
			}
			err = svc.CheckResultRead(t.Context(), result, "addp_at_current_human", fault != "non human")
			if fault == "Meta unavailable" && !errors.Is(err, engineaccess.ErrUnavailable) {
				t.Fatal("dependency failure changed to permission denial", err)
			}
			if (err == nil) != (fault == "none") {
				t.Fatal("stale or unauthorized COG read", err)
			}
			if fault == "none" || fault == "permission expired" {
				if calls != 1 {
					t.Fatal("missing current permission check")
				}
			} else if calls != 0 {
				t.Fatal("invalid local source reached permission service")
			}
		})
	}
}

func TestTIFFAuthorizedStagingVerifiesBytesAndCleansWholeComposition(t *testing.T) {
	for _, engineType := range []string{"nfs", "minio"} {
		for _, fault := range []string{"none", "single file", "native failure", "same size PAM mutation", "PAM missing", "empty PAM", "revoked", "four members", "header missing", "world mutation", "overview missing", "six members", "mask present", "mask missing", "mask mutation"} {
			t.Run(engineType+"/"+fault, func(t *testing.T) {
				extras := []string{}
				if fault == "four members" || fault == "header missing" || fault == "world mutation" {
					extras = []string{"header", "world_file"}
				}
				if fault == "six members" || fault == "overview missing" {
					extras = []string{"header", "world_file", "crs", "overview"}
				}
				if strings.HasPrefix(fault, "mask ") {
					extras = []string{"mask"}
				}
				meta, engine, config, files := tiffCOGFixture(t, engineType, fault != "single file", extras...)
				wantFiles := len(files)
				if fault == "empty PAM" {
					files[meta.item.FullName+".aux.xml"] = []byte{}
					versions := meta.item.Attributes["storage"].(map[string]interface{})["content_versions"].([]dataitem.ContentVersion)
					digest := sha256.Sum256(nil)
					versions[1].SHA256 = hex.EncodeToString(digest[:])
					versions[1].SizeBytes = 0
				}
				_, execution := freezeTIFFFixture(t, meta, engine, config)
				cfg, err := readRasterCOGExecutionConfig(execution.ExecutionConfig, "manager", 7)
				if err != nil {
					t.Fatal(err)
				}
				if fault == "same size PAM mutation" {
					files[meta.item.FullName+".aux.xml"] = []byte("<BADDataset/>")
				}
				if fault == "PAM missing" {
					delete(files, meta.item.FullName+".aux.xml")
				}
				if fault == "header missing" {
					delete(files, "images/原名 %.hdr")
				}
				if fault == "world mutation" {
					files["images/原名 %.tfw"] = []byte("other world_file")
				}
				if fault == "overview missing" {
					delete(files, meta.item.FullName+".ovr")
				}
				if fault == "mask missing" {
					delete(files, meta.item.FullName+".msk")
				}
				if fault == "mask mutation" {
					files[meta.item.FullName+".msk"] = []byte("other mask")
				}
				files["images/unregistered.xml"] = []byte("must not read")
				spy := &ftpModelReadSpy{FTPPlugin: &ftpPlugin.FTPPlugin{}, engineType: engineType, t: t, files: files}
				original, err := plugin.Get(engineType)
				if err != nil {
					t.Fatal(err)
				}
				plugin.Unregister(engineType)
				plugin.Register(spy)
				t.Cleanup(func() { plugin.Unregister(engineType); plugin.Register(original) })
				lease := commonExecution.Lease{ExecutionID: execution.ExecutionID, TenantID: 7, Attempt: 1, Token: uuid.NewString(), Owner: "tiff-worker"}
				raw, _ := json.Marshal(execution.ExecutionConfig)
				scope, err := commonExecution.NewManagerFileReadScope(raw, "11", "3", "5", meta.facts.Boundary.FileComposition)
				if err != nil {
					t.Fatal(err)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/api/v1/system/execution-authorizations/51/manager-raster-cog-accesses" {
						t.Error("wrong execution purpose", r.URL.Path)
					}
					var request commonClient.ManagerFileAccessRequest
					if json.NewDecoder(r.Body).Decode(&request) != nil || !reflect.DeepEqual(&request.SourceReadScope, scope) || request.LeaseToken != lease.Token {
						t.Error("lost exact execution scope")
					}
					if fault == "revoked" {
						w.WriteHeader(403)
						return
					}
					json.NewEncoder(w).Encode(commonClient.ManagerFileAccessObservation{ObservedAt: time.Now().UTC(), Engine: engine})
				}))
				defer server.Close()
				auth := &rasterCOGSourceAuthorization{expected: cfg, fileSourceAuthorization: &fileSourceAuthorization{scope: *scope, client: commonClient.NewSystemServiceClient(server.URL, ftpModelServiceTokens{}, server.Client()), lease: lease, authorizationID: "51", expiresAt: time.Now().Add(time.Hour)}}
				store := &stagingModelStore{}
				owner := &rasterInputOwnerFixture{}
				executor := &ManagerRasterCOGExecutor{objectStore: store, infraEndpoint: "127.0.0.1:19000", infraAccessKey: "test", infraSecretKey: "secret", defaultBucket: "manager"}
				consumed := false
				err = executor.withTIFFSource(commonExecution.ContextWithLease(t.Context(), lease), RasterCOGExecutionRequest{Task: &models.RasterCOGTask{TenantID: 7}, ExecutionID: lease.ExecutionID, Config: cfg, SourceAuthorization: auth, inputOwner: owner}, func(_ context.Context, source workflowaccess.Source) error {
					consumed = true
					if len(source.RelatedFiles) != wantFiles-1 || source.Access.Path != "" || source.Entrypoint != "" || len(owner.preparation.Objects) != wantFiles {
						t.Fatal("exact TIFF staging lost", source)
					}
					if wantFiles == 2 && (source.RelatedFiles[0].Role != "auxiliary_metadata" || !strings.HasSuffix(source.RelatedFiles[0].Access.Object, "/原名 %.tif.aux.xml")) {
						t.Fatal("exact PAM staging lost", source)
					}
					roles := map[string]string{}
					for _, ref := range scope.FileComposition.Refs {
						if !ref.Primary {
							roles[path.Base(ref.Target.StringPath())] = ref.Role
						}
					}
					for _, member := range source.RelatedFiles {
						if roles[path.Base(member.Access.Object)] != member.Role {
							t.Fatal("registered role changed", member)
						}
						delete(roles, path.Base(member.Access.Object))
					}
					if len(roles) != 0 {
						t.Fatal("registered members dropped", roles)
					}
					if fault == "native failure" {
						return errors.New("native conversion failed")
					}
					return nil
				})
				want := fault == "none" || fault == "empty PAM" || fault == "single file" || fault == "four members" || fault == "six members" || fault == "mask present"
				if (err == nil) != want || consumed != (want || fault == "native failure") || len(store.objects) != 0 {
					t.Fatalf("err=%v consumed=%v objects=%v", err, consumed, store.objects)
				}
				if (fault == "same size PAM mutation" || fault == "world mutation" || fault == "mask mutation") && !errors.Is(err, ErrTIFFSourceCompositionChanged) {
					t.Fatal("changed component did not request a Meta refresh", err)
				}
				if owner.prepared && (!owner.finished || !owner.cleaned) {
					t.Fatal("temporary source cleanup incomplete")
				}
				if fault == "revoked" && (len(spy.facts) != 0 || len(spy.opens) != 0) {
					t.Fatal("revoked source reached I/O")
				}
				for _, name := range spy.opens {
					if name == "images/unregistered.xml" {
						t.Fatal("read outside Meta composition")
					}
				}
			})
		}
	}
}

func TestTIFFMissingSourceRequiresRefreshWithoutMaskingAccessErrors(t *testing.T) {
	for _, missing := range []error{os.ErrNotExist, minio.ErrorResponse{Code: "NoSuchKey"}} {
		if !errors.Is(tiffSourceReadError(errors.Join(errors.New("source read"), missing)), ErrTIFFSourceCompositionChanged) {
			t.Fatal("missing registered component did not report source composition change")
		}
	}
	for _, other := range []error{nil, ErrFileSourceAuthorizationRequired, minio.ErrorResponse{Code: "AccessDenied"}, context.DeadlineExceeded} {
		if tiffSourceReadError(other) != other {
			t.Fatal("source error was reclassified", other)
		}
	}
}
