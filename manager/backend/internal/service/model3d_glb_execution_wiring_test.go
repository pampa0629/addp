package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/workflowaccess"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

type glbOwnerConversionFixture struct {
	t        *testing.T
	repo     *repository.Model3DGLBRepository
	previous *models.Model3DGLB
	revoked  *atomic.Bool
	revoke   bool
	calls    int
}

func (f *glbOwnerConversionFixture) BuildModel3DGLB(ctx context.Context, req Model3DGLBExecutionRequest) (*Model3DGLBExecutionResult, error) {
	f.calls++
	if req.SourceAuthorization == nil || req.SourceAuthorization.ValidateConversion(req.Config) != nil || req.Config.Options["exact_integer"] != json.Number("9007199254740993") {
		f.t.Error("dispatcher lost frozen config or strong source guard")
	}
	current, err := f.repo.GetByID(ctx, f.previous.ID, 7)
	if err != nil || current.StorageRef != f.previous.StorageRef || current.Status != "ready" || *current.LastExecutionID != *f.previous.LastExecutionID {
		f.t.Fatal("owner exposed pending conversion before publication")
	}
	if err := req.SourceAuthorization.Checkpoint(ctx); err != nil {
		return nil, err
	}
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || req.inputOwner == nil {
		f.t.Fatal("dispatcher omitted durable input owner")
	}
	store := &stagingModelStore{}
	err = stagingModelExecutor(store).withStagedModel3DSource(ctx, lease, "3ds", workflowaccess.KindDirectory, "Scene.3DS", []model3DStagingFile{stagingModelFile("Scene.3DS", "model"), stagingModelFile("Texture.PNG ", "image")}, req.inputOwner, func(context.Context, workflowaccess.Source) error {
		owner, readErr := f.repo.GetByID(ctx, f.previous.ID, 7)
		if readErr != nil {
			return readErr
		}
		raw, _ := json.Marshal(owner.Metadata["model3d_output_lifecycle"])
		var state struct {
			Inputs []repository.Model3DGLBInputPreparation `json:"input_preparations"`
		}
		if json.Unmarshal(raw, &state) != nil || len(state.Inputs) != 1 || len(state.Inputs[0].Objects) != 2 || state.Inputs[0].Released {
			f.t.Fatal("uploads reached converter without durable live ownership")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if f.revoke {
		f.revoked.Store(true)
	}
	return &Model3DGLBExecutionResult{StorageRef: req.Config.Result.StorageRef, FileName: req.Config.Result.FileName, ContentURL: "https://runtime.invalid/must-not-bypass-owner.glb", SizeBytes: 72, Metadata: commonModels.JSONMap{"runtime_fixture": true}}, nil
}

// Exercise the actual producer, queue, claimed-plan and owner publication
// glue. Conversion and System policy use protocol fixtures, never real Allow.
func TestGLBOwnerWiringPublishesOnlyAfterBoundLeaseAndFinalConsumption(t *testing.T) {
	for _, outcome := range []string{"success", "revoke before publication"} {
		t.Run(outcome, func(t *testing.T) {
			db := newModel3DGLBTaskServiceTestDB(t)
			if err := executiontest.EnsureSQLiteStore(db); err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(`CREATE TABLE manager.model_3d_glb (
			 id INTEGER PRIMARY KEY AUTOINCREMENT,tenant_id INTEGER,item_fingerprint TEXT,item_id INTEGER,locator TEXT,
			 task_id INTEGER,last_execution_id TEXT,source_engine_id INTEGER,source_format TEXT,source_size_bytes INTEGER,
			 storage_ref TEXT,file_name TEXT,size_bytes INTEGER,content_url TEXT,status TEXT,metadata JSON,error_message TEXT,
			 created_by INTEGER,created_at DATETIME,updated_at DATETIME,deleted_at DATETIME)`).Error; err != nil {
				t.Fatal(err)
			}
			repo := repository.NewModel3DGLBRepository(db)
			svc := NewModel3DGLBTaskService(repo)
			svc.SetBucket("manager")
			source, item := model3DGLBCandidateFixture("3ds")
			source.ItemFingerprint = commonModels.GenerateItemFingerprint(33, item.FullName)
			item.Fingerprint = source.ItemFingerprint
			tenant := uint(7)
			engine := &commonModels.Engine{ID: 33, TenantID: &tenant, EngineType: "ftp", Version: 5, LifecycleState: "active", ConnectionStatus: "online", ConnectionInfo: commonModels.ConnectionInfo{"host": "fixture.invalid"}}
			svc.SetSourceEngineClient(&glbSourceEngineFixture{engine: engine})
			task := &models.Model3DGLBTask{TenantID: 7, Name: outcome, Enabled: true, Config: commonModels.JSONMap{"source": commonModels.JSONMap{"item_locator": source.ItemLocator, "source_engine_id": 33, "item_id": 41, "item_fingerprint": source.ItemFingerprint, "format": "3ds"}, "options": commonModels.JSONMap{"exact_integer": json.Number("9007199254740993")}}}
			if err := svc.Create(t.Context(), task); err != nil {
				t.Fatal(err)
			}
			previousID := uuid.NewString()
			previous := &models.Model3DGLB{TenantID: 7, ItemFingerprint: source.ItemFingerprint, ItemID: &source.ItemID, Locator: source.ItemLocator, TaskID: &task.ID, LastExecutionID: &previousID, SourceEngineID: 33, SourceFormat: "3ds", StorageRef: "previous.glb", FileName: "previous.glb", ContentURL: "previous-url", Status: models.Model3DGLBStatusReady, Metadata: commonModels.JSONMap{"old_fact": "preserved"}}
			if err := repo.Create(t.Context(), previous); err != nil {
				t.Fatal(err)
			}
			var revoked atomic.Bool
			var issueCalls, consumeCalls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.Header.Get("Cookie") != "" {
					t.Error("noncanonical authorization request")
				}
				var request struct {
					ExecutionID string                                   `json:"execution_id"`
					Attempt     int                                      `json:"attempt"`
					LeaseToken  string                                   `json:"lease_token"`
					Scope       *commonExecution.ManagerModel3DReadScope `json:"source_read_scope"`
				}
				if json.NewDecoder(r.Body).Decode(&request) != nil {
					t.Error("invalid authorization request")
					w.WriteHeader(400)
					return
				}
				raw, err := repo.RawExecutionConfig(t.Context(), 7, request.ExecutionID)
				header, headerErr := decodeModel3DGLBFrozenHeader(raw)
				if err != nil || headerErr != nil {
					t.Error("pending config missing")
					w.WriteHeader(400)
					return
				}
				scope, err := commonExecution.NewManagerModel3DReadScope(raw, header.OwnerTask.ID, header.OwnerTask.Version, header.EngineVersion, header.Composition)
				if err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/auth/execution-authorizations/manager-model3d-glb") {
					issueCalls.Add(1)
					if r.Header.Get("Authorization") != "Bearer addp_at_current_human" {
						t.Error("issue used machine instead of current User")
					}
					w.WriteHeader(201)
					_ = json.NewEncoder(w).Encode(commonClient.IssuedManagerModel3DAuthorization{ID: "51", ExecutionID: request.ExecutionID, TenantID: "7", Audience: "manager", SourceReadScope: *scope, ExpiresAt: time.Now().Add(30 * time.Minute)})
					return
				}
				consumeCalls.Add(1)
				row, err := repo.GetExecution(t.Context(), 7, request.ExecutionID)
				if r.URL.Path != "/api/v1/system/execution-authorizations/51/manager-model3d-glb-accesses" || r.Header.Get("Authorization") != "Bearer addp_at_current_manager_service" || err != nil || row.LeaseToken == nil || request.LeaseToken != *row.LeaseToken || request.Attempt != row.Attempt || !reflect.DeepEqual(request.Scope, scope) {
					t.Error("consume lost real claimed lease or raw scope")
					w.WriteHeader(403)
					return
				}
				if revoked.Load() {
					w.WriteHeader(403)
					return
				}
				_ = json.NewEncoder(w).Encode(commonClient.ManagerModel3DAccessObservation{ObservedAt: time.Now().UTC(), Engine: engine})
			}))
			t.Cleanup(server.Close)
			svc.SetSourceAuthorizationClients(glbMetaDetailFixture{item}, commonClient.NewSystemExecutionAuthorizationClient(server.URL, server.Client()), commonClient.NewSystemServiceClient(server.URL, &profileServiceTokenForTest{}, server.Client()))
			executor := &glbOwnerConversionFixture{t: t, repo: repo, previous: previous, revoked: &revoked, revoke: outcome != "success"}
			svc.SetExecutor(executor)
			userCtx := commonExecution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "31", nil))
			id, err := svc.Execute(userCtx, task.ID, 7, "manual", "manager", nil, true, "addp_at_current_human")
			if err != nil {
				t.Fatal(err)
			}
			claimed, lease, err := repository.NewBoundedExecutionQueueRepository(db).ClaimNext(t.Context(), []string{commonExecution.TaskTypeModel3DGLBGeneration}, "glb-owner-wiring", time.Now().UTC(), time.Minute)
			if err != nil || claimed == nil || lease == nil || claimed.ExecutionID != id {
				t.Fatalf("claim=%#v %v", claimed, err)
			}
			workerCtx := commonExecution.ContextWithLease(t.Context(), *lease)
			plan, err := svc.prepareClaim(workerCtx, task, claimed)
			if err != nil || plan == nil {
				t.Fatalf("claimed plan=%#v %v", plan, err)
			}
			svc.runModel3DGLBGeneration(workerCtx, task, id, plan)
			terminal, err := repo.GetExecution(t.Context(), 7, id)
			result, readErr := repo.GetByID(t.Context(), previous.ID, 7)
			if err != nil || readErr != nil || executor.calls != 1 || issueCalls.Load() != 1 || consumeCalls.Load() < 3 {
				t.Fatalf("owner flow failed: %v %v", err, readErr)
			}
			wantStatus := commonExecution.ExecutionStatusFailed
			if outcome == "success" {
				wantStatus = commonExecution.ExecutionStatusSuccess
				saved, scopeErr := model3DGLBPublishedScope(result)
				if scopeErr != nil || !reflect.DeepEqual(saved, &plan.authorization.scope) || result.StorageRef != plan.config.Result.StorageRef || *result.LastExecutionID != id || result.ContentURL != fmt.Sprintf("/api/v1/manager/model_3d_glb/%d/content", result.ID) {
					t.Fatalf("published unbound or noncanonical result: %#v %v", result, scopeErr)
				}
			} else if result.StorageRef != previous.StorageRef || result.Status != "ready" || *result.LastExecutionID != previousID || result.Metadata["old_fact"] != "preserved" {
				t.Fatalf("revocation destroyed previous renderer version: %#v", result)
			}
			rawInput, _ := json.Marshal(result.Metadata["model3d_output_lifecycle"])
			var inputState struct {
				Inputs []repository.Model3DGLBInputPreparation `json:"input_preparations"`
			}
			if json.Unmarshal(rawInput, &inputState) != nil || len(inputState.Inputs) != 1 || !inputState.Inputs[0].Cleaned || inputState.Inputs[0].ExecutionID != id {
				t.Fatal("publication/denial lost exact cleaned input manifest")
			}
			storedTask, err := repo.GetTask(t.Context(), task.ID, 7)
			if terminal.Status != wantStatus || terminal.CompletedAt == nil || err != nil || storedTask.LastExecutionStatus == nil || *storedTask.LastExecutionStatus != wantStatus {
				t.Fatalf("non-atomic terminal/owner status: %#v %#v %v", terminal, storedTask, err)
			}
		})
	}
}
