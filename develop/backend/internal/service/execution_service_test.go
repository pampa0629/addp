package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestClaimedWorkflowPersistsReceiptBeforePollingAndClosesSafely(t *testing.T) {
	for _, scenario := range []string{"success", "runtime_failed", "revoked", "authorization_expired", "lease_lost"} {
		t.Run(scenario, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := db.DB()
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { sqlDB.Close() })
			if err := executiontest.EnsureSQLiteStore(db); err != nil {
				t.Fatal(err)
			}
			queue := repository.NewExecutionRepository(db)
			engineType := "test_leased_workflow_" + scenario
			plugin.Register(plugin.NewHTTPWorkflowRuntimeProvider(engineType, "Leased Workflow"))
			executionID := uuid.NewString()
			runtimeCalls, statusCalls := 0, 0
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/operators":
					json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "operators": []map[string]interface{}{{"id": "contract_op", "name": "contract_op", "display_name": "Contract", "engine_type": engineType, "category": "test", "category_path": []string{"test"}, "description": "contract", "execution_modes": []string{"workflow"}, "effects": []string{"read"}, "parameters": []interface{}{}, "output_ports": []map[string]interface{}{{"name": "default", "type": "object", "is_default": true}}}}})
				case "/api/workflow":
					runtimeCalls++
					if scenario == "lease_lost" {
						db.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", executionID).Update("lease_expires_at", time.Now().UTC().Add(-time.Minute))
					}
					json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "execution_id": "remote-private-sentinel"})
				case "/api/executions/remote-private-sentinel":
					statusCalls++
					var stored commonExecution.TaskExecution
					db.Where("execution_id = ?", executionID).First(&stored)
					if stored.Metadata["runtime_execution_id"] != "remote-private-sentinel" {
						t.Error("runtime was polled before its receipt committed")
					}
					status := "success"
					if scenario == "runtime_failed" {
						status = "failed"
					}
					json.NewEncoder(w).Encode(map[string]interface{}{"status": status, "execution_id": "remote-private-sentinel", "progress": 100, "error": "runtime-private-sentinel"})
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(runtime.Close)
			endpoint, _ := url.Parse(runtime.URL)
			port, _ := strconv.Atoi(endpoint.Port())
			capabilities := workflowRuntimeCapabilitiesForTest(t, engineType)
			tenant := uint(7)
			engine := &commonModels.Engine{ID: 91, Name: "Leased runtime", TenantID: &tenant, EngineType: engineType, LifecycleState: "active", ConnectionStatus: "online", Capabilities: capabilities, ConnectionInfo: commonModels.ConnectionInfo{"protocol": "http", "host": endpoint.Hostname(), "port": port}}
			system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/system/execution-authorizations/1/engine-accesses" {
					http.NotFound(w, r)
					return
				}
				if scenario == "revoked" {
					http.Error(w, "revoked", 403)
					return
				}
				var request commonClient.ExecutionEngineAccessRequest
				json.NewDecoder(r.Body).Decode(&request)
				json.NewEncoder(w).Encode(commonClient.ExecutionEngineAccess{AuthorizationID: "1", ExecutionID: request.ExecutionID, EngineID: "91", Effects: request.RequiredEffects, ExpiresAt: time.Now().Add(time.Minute), Engine: engine})
			}))
			t.Cleanup(system.Close)
			client := commonClient.NewSystemServiceClient(system.URL, staticServiceTokenSource("addp_at_service"), system.Client())
			discovery := &OperatorDiscoveryService{
				getRuntimeDescriptor: func(context.Context, uint, uint) (*commonModels.EngineRuntimeDescriptor, error) {
					return &commonModels.EngineRuntimeDescriptor{ID: 91, Name: engine.Name, EngineType: engineType, LifecycleState: "active", ConnectionStatus: "online", Capabilities: capabilities, RuntimeEndpoint: &commonModels.EngineRuntimeEndpoint{Protocol: "http", Host: endpoint.Hostname(), Port: port}}, nil
				},
				listWorkflowOperators: func(context.Context, *commonModels.Engine) ([]commonModels.OperatorDescriptor, error) {
					return []commonModels.OperatorDescriptor{{ID: "contract_op", Name: "contract_op", EngineType: engineType, ExecutionModes: []string{"workflow"}, Effects: []string{"read"}, Parameters: []commonModels.ParameterDescriptor{}}}, nil
				},
			}
			executor := NewDevExecutor(nil, commonExecution.NewTaskExecutionRepository(db), NewWorkflowEngineService(client), discovery, nil, NewSQLEngineService(&config.Config{}, client, nil), nil, nil)
			runner, err := NewExecutionService(executor, queue)
			if err != nil {
				t.Fatal(err)
			}
			principal, membership, version, authorization := int64(11), int64(12), int64(3), int64(1)
			expires := time.Now().Add(time.Minute)
			if scenario == "authorization_expired" {
				expires = time.Now().Add(-time.Minute)
			}
			item := &commonExecution.TaskExecution{TenantID: 7, ExecutionID: executionID, Module: "develop", TaskType: "workflow", Source: "develop", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", MaxAttempts: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(), ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version, ExecutionAuthorizationID: &authorization, AuthorizationExpiresAt: &expires,
				ExecutionConfig: commonModels.JSONMap{"engine_id": 91, "timeout": 30, "content": map[string]interface{}{"workflow_definition": map[string]interface{}{"tasks": []interface{}{map[string]interface{}{"id": "contract", "operator": "contract_op", "params": map[string]interface{}{}, "depends_on": []interface{}{}}}}}},
			}
			if err := db.Create(item).Error; err != nil {
				t.Fatal(err)
			}
			claimed, lease, err := queue.ClaimNext("workflow", t.Context(), "owner", time.Now(), time.Minute, nil)
			if err != nil || lease == nil {
				t.Fatalf("claim: %v", err)
			}
			err = runner.Execute(t.Context(), claimed, *lease)
			if scenario == "lease_lost" {
				if err == nil || statusCalls != 0 {
					t.Fatal("lost lease accepted a receipt or polled remote")
				}
				if count, err := queue.RecoverExpired(t.Context(), time.Now(), 1); err != nil || count != 1 {
					t.Fatalf("recovery: %d %v", count, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			var stored commonExecution.TaskExecution
			db.Where("execution_id = ?", executionID).First(&stored)
			want := "failed"
			if scenario == "success" {
				want = "success"
			}
			if stored.Status != want || stored.LeaseToken != nil {
				t.Fatalf("terminal state=%s lease=%v", stored.Status, stored.LeaseToken != nil)
			}
			if scenario == "revoked" || scenario == "authorization_expired" {
				if runtimeCalls != 0 {
					t.Fatal("unauthorized runtime dispatch")
				}
			}
			if scenario == "success" || scenario == "runtime_failed" {
				if stored.Metadata["runtime_execution_id"] != "remote-private-sentinel" {
					t.Fatal("terminal result lost receipt")
				}
			}
			var events []commonExecution.Event
			db.Where("execution_id = ?", executionID).Order("id").Find(&events)
			for _, event := range events {
				encoded, _ := json.Marshal(event)
				if strings.Contains(string(encoded), "private-sentinel") {
					t.Fatal("private runtime facts entered events")
				}
			}
		})
	}
}
