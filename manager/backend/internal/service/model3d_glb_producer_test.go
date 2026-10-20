package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/authorization/authtest"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type glbMetaDetailFixture struct{ item *commonModels.MetaItem }

func (f glbMetaDetailFixture) GetItemByIDForTenant(_ context.Context, tenant, id uint) (*commonModels.MetaItem, error) {
	if f.item.TenantID != tenant || f.item.ID != id {
		return nil, commonAPI.ErrNotFound
	}
	return f.item, nil
}

func TestGLBProducerBindsOnlySystemScopeForExactPendingConfig(t *testing.T) {
	for _, outcome := range []string{"bound", "denied", "expired human", "wrong scope", "raw config drift", "actor drift", "unknown then original", "still unknown"} {
		t.Run(outcome, func(t *testing.T) {
			db := newModel3DGLBTaskServiceTestDB(t)
			if err := executiontest.EnsureSQLiteStore(db); err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(`CREATE TABLE manager.model_3d_glb (id INTEGER PRIMARY KEY,tenant_id INTEGER,task_id INTEGER,item_fingerprint TEXT,status TEXT,metadata JSON,updated_at DATETIME,deleted_at DATETIME)`).Error; err != nil {
				t.Fatal(err)
			}
			source, item := model3DGLBCandidateFixture("3ds")
			tenant := uint(7)
			engine := &commonModels.Engine{ID: 33, TenantID: &tenant, EngineType: "ftp", Version: 5, LifecycleState: "active", ConnectionStatus: "online"}
			repo := repository.NewModel3DGLBRepository(db)
			svc := NewModel3DGLBTaskService(repo)
			svc.SetBucket("manager")
			svc.SetSourceEngineClient(&glbSourceEngineFixture{engine: engine})
			task := &models.Model3DGLBTask{TenantID: 7, Name: outcome, Enabled: true, Config: commonModels.JSONMap{
				"source":  commonModels.JSONMap{"item_locator": source.ItemLocator, "source_engine_id": 33, "item_id": source.ItemID, "item_fingerprint": source.ItemFingerprint, "format": "3ds"},
				"options": commonModels.JSONMap{"number": json.Number("9007199254740993")},
			}}
			if err := svc.Create(t.Context(), task); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int64
			originalExpiry := time.Now().Add(30 * time.Minute).UTC()
			var firstUUID string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				var request map[string]json.RawMessage
				if r.Method != "POST" || r.URL.Path != "/api/v1/system/auth/execution-authorizations/manager-model3d-glb" || r.Header.Get("Authorization") != "Bearer addp_at_current_human" || r.Header.Get("Cookie") != "" || json.NewDecoder(r.Body).Decode(&request) != nil || len(request) != 2 {
					t.Error("issuance sent noncanonical request")
				}
				var executionID string
				_ = json.Unmarshal(request["execution_id"], &executionID)
				if call == 1 {
					firstUUID = executionID
				} else if executionID != firstUUID {
					t.Error("reconciliation changed UUID")
				}
				row, err := repo.GetExecution(t.Context(), 7, executionID)
				if err != nil || row == nil || row.Status != commonExecution.ExecutionStatusPending || row.Attempt != 0 || row.ExecutionAuthorizationID != nil || row.ActorPrincipalID == nil || *row.ActorPrincipalID != 31 {
					t.Error("issue before durable human pending")
					w.WriteHeader(500)
					return
				}
				raw, err := repo.RawExecutionConfig(t.Context(), 7, executionID)
				if err != nil || !strings.Contains(string(raw), "9007199254740993") {
					t.Error("raw number lost")
				}
				header, err := decodeModel3DGLBFrozenHeader(raw)
				if err != nil {
					t.Error(err)
					w.WriteHeader(500)
					return
				}
				scope, err := commonExecution.NewManagerModel3DReadScope(raw, header.OwnerTask.ID, header.OwnerTask.Version, header.EngineVersion, header.Composition)
				if err != nil {
					t.Error(err)
					w.WriteHeader(500)
					return
				}
				switch outcome {
				case "unknown then original":
					if call == 1 {
						w.WriteHeader(502)
						return
					}
				case "still unknown":
					w.WriteHeader(502)
					return
				case "denied":
					w.WriteHeader(403)
					return
				case "expired human":
					w.WriteHeader(401)
					return
				case "wrong scope":
					scope.ConfigDigest = strings.Repeat("a", 64)
				case "raw config drift":
					changed := strings.Replace(string(raw), "9007199254740993", "9007199254740992", 1)
					if err := db.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", executionID).UpdateColumn("execution_config", changed).Error; err != nil {
						t.Error(err)
					}
				case "actor drift":
					if err := db.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", executionID).UpdateColumn("actor_principal_id", 32).Error; err != nil {
						t.Error(err)
					}
				}
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(commonClient.IssuedManagerModel3DAuthorization{ID: "51", ExecutionID: executionID, TenantID: "7", Audience: "manager", SourceReadScope: *scope, ExpiresAt: originalExpiry})
			}))
			t.Cleanup(server.Close)
			svc.SetSourceAuthorizationClients(glbMetaDetailFixture{item}, commonClient.NewSystemExecutionAuthorizationClient(server.URL, server.Client()), commonClient.NewSystemServiceClient(server.URL, &profileServiceTokenForTest{}, server.Client()))
			ctx := commonExecution.WithUserOrigin(t.Context(), authtest.NewTenantUserAuthContext("7", "31", nil))
			id, err := svc.Execute(ctx, task.ID, 7, "manual", "manager", nil, false, "addp_at_current_human")
			var row commonExecution.TaskExecution
			if readErr := db.Where("tenant_id = 7").First(&row).Error; readErr != nil {
				t.Fatal(readErr)
			}
			wantCalls := int64(1)
			if outcome == "unknown then original" || outcome == "still unknown" {
				wantCalls = 2
			}
			if calls.Load() != wantCalls {
				t.Fatalf("issue calls=%d", calls.Load())
			}
			if outcome == "bound" || outcome == "unknown then original" {
				if err != nil || id != row.ExecutionID || row.ExecutionAuthorizationID == nil || *row.ExecutionAuthorizationID != 51 || row.AuthorizationExpiresAt == nil || !row.AuthorizationExpiresAt.Equal(originalExpiry) || row.Status != "pending" {
					t.Fatalf("bound=%#v err=%v", row, err)
				}
				return
			}
			if err == nil || id != "" || row.ExecutionAuthorizationID != nil || row.Attempt != 0 {
				t.Fatalf("untrusted response bound: %#v err=%v", row, err)
			}
			if outcome == "expired human" && !errors.Is(err, commonClient.ErrManagerPreviewCredentialRejected) {
				t.Fatal(err)
			}
			// A changed Actor cannot be closed by the original request. The normal
			// bounded recovery gate must reap that malformed pending instead.
			if outcome != "actor drift" && row.Status != "failed" {
				t.Fatalf("unbound pending left open: %#v", row)
			}
		})
	}
}
