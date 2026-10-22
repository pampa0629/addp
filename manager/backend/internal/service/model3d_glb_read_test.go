package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

func glbPublishedReadFixture(t *testing.T) (*models.Model3DGLB, *commonModels.Engine, *commonModels.MetaItem) {
	t.Helper()
	source, item := model3DGLBCandidateFixture("3ds")
	tenant := uint(7)
	engine := &commonModels.Engine{ID: 33, TenantID: &tenant, EngineType: "ftp", Version: 5, LifecycleState: "active", ConnectionStatus: "online"}
	candidate, err := model3DGLBCandidateFromMeta(7, source, item)
	if err != nil {
		t.Fatal(err)
	}
	core, _ := json.Marshal(map[string]any{"source": map[string]any{"item_locator": source.ItemLocator, "source_engine_id": 33, "item_id": 41, "item_fingerprint": source.ItemFingerprint, "format": "3ds"}, "result": map[string]any{"storage_ref": "old", "file_name": "model.glb"}, "options": map[string]any{}})
	id := uuid.NewString()
	frozen, err := freezeFileModel3DGLBConfig(&repository.Model3DGLBTaskSnapshot{TaskID: 17, TenantID: 7, Version: 3, Config: core}, engine, candidate, id, "manager")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(frozen)
	scope, err := commonExecution.NewManagerModel3DReadScope(raw, "17", "3", "5", candidate.composition)
	if err != nil {
		t.Fatal(err)
	}
	config, _ := decodeFileModel3DGLBConversion(raw)
	return &models.Model3DGLB{ID: 19, TenantID: 7, ItemID: &source.ItemID, ItemFingerprint: source.ItemFingerprint, Locator: source.ItemLocator, SourceEngineID: 33, SourceFormat: "3ds", Status: "ready", StorageRef: config.Result.StorageRef, Metadata: commonModels.JSONMap{"model3d_output_owner": "manager_execution", "model3d_source_scope": scope}}, engine, item
}

// Meta changes after Manager's local comparison. A primary-only System Allow
// for the new boundary must never authorize the previously generated boundary.
func TestGLBResultReadBindsSystemDecisionToGeneratedComposition(t *testing.T) {
	for _, outcome := range []string{"same source", "composition A to B", "engine version changed", "grant revoked", "old strict server"} {
		t.Run(outcome, func(t *testing.T) {
			result, engine, item := glbPublishedReadFixture(t)
			generated, _ := model3DGLBPublishedScope(result)
			primary, _ := generated.FileComposition.PrimaryTarget()
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/v1/system/engine-access/read-checks/manager-preview" || r.Header.Get("Authorization") != "Bearer addp_at_current_human" || r.Header.Get("Cookie") != "" {
					t.Error("changed Human read protocol")
				}
				decoder := json.NewDecoder(r.Body)
				decoder.DisallowUnknownFields()
				if outcome == "old strict server" {
					var old struct {
						Targets []plugin.EngineCatalogPath `json:"targets"`
					}
					if decoder.Decode(&old) == nil {
						t.Error("expected source not sent")
					}
					w.WriteHeader(400)
					return
				}
				var request struct {
					Targets        []plugin.EngineCatalogPath                 `json:"targets"`
					ExpectedSource *commonClient.ManagerModel3DExpectedSource `json:"expected_source"`
				}
				if decoder.Decode(&request) != nil || request.ExpectedSource == nil || request.ExpectedSource.Validate() != nil || len(request.Targets) != 1 || !reflect.DeepEqual(request.Targets[0], primary) || request.ExpectedSource.EngineVersion != generated.EngineVersion || !reflect.DeepEqual(request.ExpectedSource.FileComposition, generated.FileComposition) {
					t.Error("complete generated source missing from current decision")
					w.WriteHeader(400)
					return
				}
				// Change authoritative facts after Manager's preliminary A check.
				// A current Grant for B must not authorize the generated A artifact.
				if outcome == "composition A to B" {
					refs := item.Attributes["item"].(map[string]any)["refs"].([]map[string]any)
					refs[1]["path"] = "models/new-composition.PNG"
					item.Attributes["format_info"].(map[string]any)["3ds"].(map[string]any)["texture_refs"] = []string{"new-composition.PNG"}
				}
				if outcome == "engine version changed" {
					engine.Version++
				}
				current, err := model3DGLBCandidateFromMeta(result.TenantID, Model3DGLBSourceConfig{ItemID: *result.ItemID, ItemLocator: result.Locator, ItemFingerprint: result.ItemFingerprint, SourceEngineID: result.SourceEngineID, Format: result.SourceFormat}, item)
				if err != nil || !reflect.DeepEqual(current.composition, &request.ExpectedSource.FileComposition) || request.ExpectedSource.EngineVersion != strconv.FormatInt(engine.Version, 10) || outcome == "grant revoked" {
					w.WriteHeader(403)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"observed_at": time.Now().UTC()})
			}))
			t.Cleanup(server.Close)
			svc := NewModel3DGLBTaskService(nil)
			svc.SetSourceEngineClient(&glbSourceEngineFixture{engine: engine})
			svc.SetSourceAuthorizationClients(glbMetaDetailFixture{item}, nil, commonClient.NewSystemServiceClient(server.URL, &profileServiceTokenForTest{}, server.Client()))
			err := svc.CheckResultRead(t.Context(), result, "addp_at_current_human", true)
			if calls != 1 {
				t.Fatalf("read retried, fell back, or skipped actual check: %d", calls)
			}
			if outcome == "same source" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			want := commonClient.ErrManagerPreviewReadDenied
			if outcome == "old strict server" {
				want = commonClient.ErrManagerPreviewReadUnavailable
			}
			if !errors.Is(err, want) {
				t.Fatalf("changed source accepted: %v", err)
			}
		})
	}
}

func TestGLBReadRejectsTicketsChangedCompositionAndEngineBeforePermissionCall(t *testing.T) {
	for _, name := range []string{"resource ticket", "delegated", "service", "old native result", "invalid scope", "new composition", "changed engine", "engine changed to nfs", "engine changed to minio", "no current consumer"} {
		t.Run(name, func(t *testing.T) {
			result, engine, item := glbPublishedReadFixture(t)
			svc := NewModel3DGLBTaskService(nil)
			svc.SetSourceEngineClient(&glbSourceEngineFixture{engine: engine})
			svc.sourceMeta = glbMetaDetailFixture{item}
			credential, human := "addp_at_current_human", true
			want := ErrModel3DGLBSourceAuthorizationRequired
			switch name {
			case "resource ticket":
				credential = ""
				want = commonClient.ErrManagerPreviewCredentialRejected
			case "delegated":
				credential = "addp_dat_tool"
				want = commonClient.ErrManagerPreviewCredentialRejected
			case "service":
				human = false
				want = commonClient.ErrManagerPreviewCredentialRejected
			case "old native result":
				delete(result.Metadata, "model3d_output_owner")
			case "invalid scope":
				result.Metadata["model3d_source_scope"] = map[string]any{}
			case "new composition":
				refs := item.Attributes["item"].(map[string]any)["refs"].([]map[string]any)
				refs[1]["path"] = "models/changed.PNG"
			case "changed engine":
				engine.Version++
			case "engine changed to nfs":
				engine.EngineType = "nfs"
				engine.Version++
			case "engine changed to minio":
				engine.EngineType = "minio"
			case "no current consumer":
				want = engineaccess.ErrUnavailable
			}
			if err := svc.CheckResultRead(t.Context(), result, credential, human); !errors.Is(err, want) {
				t.Fatalf("%s err=%v", name, err)
			}
		})
	}
}
