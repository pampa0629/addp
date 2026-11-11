package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
)

func TestPublicGeoPackageWorkflowSubmission(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		saved     bool
		status    int
		wantState string
		wantCalls int32
	}{
		{"temporary", false, 201, "pending", 1},
		{"saved", true, 201, "pending", 1},
		{"saved admission rejected", true, 201, "pending", 1},
		{"denied", true, 403, "rejected", 1},
		{"unknown", false, 503, "pending", 1},
		{"incomplete", false, 0, "", 0},
		{"unknown format", false, 0, "", 0},
		{"metadata mismatch", false, 0, "", 0},
		{"wrong snapshot identity", false, 0, "", 0},
		{"mixed table", false, 0, "", 0},
		{"save", false, 0, "", 0},
		{"other format", false, 201, "", 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db := privateFileSQLiteDB(t)
			if scenario.saved {
				if err := db.Exec(`CREATE TABLE develop.dev_tasks (
 id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT NOT NULL,
 display_name TEXT, dev_type TEXT NOT NULL, content JSON NOT NULL, execution_config JSON,
 editor_layout JSON, timeout INTEGER, description TEXT, tags TEXT, created_by INTEGER,
 updated_by INTEGER, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME,
 status TEXT, last_execution_id TEXT, last_execution_status TEXT, last_run_at DATETIME
 )`).Error; err != nil {
					t.Fatal(err)
				}
			}
			fileRepo := repository.NewFileExecutionRepository(db)
			auth := authtest.NewTenantUserAuthContext("3", "31", []string{"develop.task.execute", "develop.data_read.execute"})
			ctx := WithExecutionAuthContext(t.Context(), auth)
			path := "sources/ordinary.bin"
			if scenario.name == "metadata mismatch" {
				path = "sources/snapshot.gpkg"
			} else if scenario.name == "other format" {
				path = "sources/snapshot.geojson"
			}

			var eaCalls, genericCalls, notifications atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/meta/items/by-catalog-path":
					if r.URL.Query().Get("engine_id") != "11" || r.URL.Query().Get("catalog_path") != path || r.Header.Get("Authorization") != "Bearer meta-fixture" {
						t.Error("Meta request did not use exact path and tenant service")
					}
					name := path
					format, kind := "geopackage", "container"
					switch scenario.name {
					case "unknown format":
						format = "unknown"
					case "metadata mismatch", "other format":
						format, kind = "geojson", "table"
					case "wrong snapshot identity":
						name = "sources/other"
					}
					_ = json.NewEncoder(w).Encode(commonModels.MetaItem{ID: 19, TenantID: 3, EngineID: 11, FullName: name, ItemType: "file",
						Attributes: map[string]interface{}{
							"item":        map[string]interface{}{"format": format, "layout": "single", "data_type": kind},
							"format_info": map[string]interface{}{"geopackage": map[string]interface{}{"composition_complete": scenario.name != "incomplete"}},
						}})
				case "/api/v1/system/auth/execution-authorizations/develop-geopackage-workflows":
					eaCalls.Add(1)
					var request struct {
						ExecutionID  string `json:"execution_id"`
						IntentDigest string `json:"intent_digest"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil || r.Header.Get("Authorization") != "Bearer addp_at_user" {
						t.Error("bad precise issuance request", err)
						w.WriteHeader(500)
						return
					}
					pending, err := fileRepo.Read(r.Context(), 3, request.ExecutionID)
					if err != nil {
						t.Error("issuance occurred before pending was visible", err)
						w.WriteHeader(500)
						return
					}
					digest, _ := pending.Intent.Digest()
					if pending.AdmissionState != "pending" || pending.AuthorizationID != "" || digest != request.IntentDigest ||
						pending.Intent.Target != nil || len(pending.Intent.Sources) != 1 || pending.Intent.Sources[0].Target.StringPath() != path {
						t.Error("wrong pending intent")
					}
					if scenario.status != 201 {
						w.WriteHeader(scenario.status)
						return
					}
					scope, err := execution.NewDevelopBoundFileScope(pending.Intent, []execution.DevelopFileSourceReference{{
						Target: pending.Intent.Sources[0].Target, ItemFingerprint: strings.Repeat("a", 64), BoundaryHash: strings.Repeat("b", 64), EngineVersion: "2",
					}}, "")
					if err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					scopeDigest, _ := scope.Digest()
					w.WriteHeader(201)
					_ = json.NewEncoder(w).Encode(client.IssuedDevelopFileAuthorization{
						Authorization: client.IssuedExecutionAuthorization{ID: "9", ExecutionID: request.ExecutionID, Audience: "develop", TenantID: "3", ActorPrincipalID: "31",
							TenantMembershipID: pending.Actor.TenantMembershipID, IssuedAuthorizationVersion: pending.Actor.AuthorizationVersion,
							SourceType: "user", Accesses: []client.ExecutionEngineAccessScope{{EngineID: "11", Effects: []string{"read"}}}, ExpiresAt: time.Now().Add(time.Hour)},
						Scope: *scope, ScopeDigest: scopeDigest,
					})
				case "/api/v1/system/auth/execution-authorizations":
					genericCalls.Add(1)
					var req client.IssueExecutionAuthorizationRequest
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Error(err)
					}
					w.WriteHeader(201)
					_ = json.NewEncoder(w).Encode(client.IssuedExecutionAuthorization{ID: "9", ExecutionID: req.ExecutionID, Audience: "develop", TenantID: "3", ActorPrincipalID: "31",
						TenantMembershipID: *auth.Context.TenantMembershipID, IssuedAuthorizationVersion: auth.Authorization.AuthorizationVersion, SourceType: "user",
						Accesses: req.Accesses, ExpiresAt: time.Now().Add(time.Hour)})
				default:
					t.Error("unexpected request", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()

			e := NewDevExecutor(repository.NewDevTaskRepository(db), execution.NewTaskExecutionRepository(db), nil,
				fileSubmissionDiscovery(t), client.NewMetaClient(server.URL, staticServiceTokenSource("meta-fixture")),
				NewSQLEngineService(&config.Config{}, client.NewSystemServiceClient(server.URL, staticServiceTokenSource("fixture-service"), server.Client()),
					client.NewSystemExecutionAuthorizationClient(server.URL, server.Client())), nil, nil)
			e.fileExecutions = fileRepo
			e.SetExecutionNotifier(func() { notifications.Add(1) })
			nodes := []interface{}{map[string]interface{}{"id": "a", "operator": "load", "params": map[string]interface{}{
				"locator": "addp://engine/11/path/" + path + "?type=file&item_id=999",
			}, "depends_on": []interface{}{}}}
			switch scenario.name {
			case "mixed table":
				nodes = append(nodes, map[string]interface{}{"id": "b", "operator": "load", "params": map[string]interface{}{
					"locator": "addp://engine/11/path/public/table?type=table",
				}, "depends_on": []interface{}{}})
			case "save":
				nodes = append(nodes, map[string]interface{}{"id": "b", "operator": "save", "params": map[string]interface{}{
					"target_parent_locator": "addp://engine/11/path/outputs?type=directory", "target_name": "result.gpkg",
				}, "depends_on": []interface{}{"a"}})
			}
			content := map[string]interface{}{"workflow_definition": map[string]interface{}{"tasks": nodes}}
			cfg := map[string]interface{}{"engine_id": float64(50)}
			var id string
			var err error
			if scenario.saved {
				task := &models.DevTask{TenantID: 3, Name: "saved", DevType: "workflow", Content: content, ExecutionConfig: cfg, Timeout: 300}
				if err := e.devTaskRepo.Create(task); err != nil {
					t.Fatal(err)
				}
				id, err = e.ExecuteWithParams(ctx, task.ID, nil, 3, 31, "addp_at_user")
				var persisted models.DevTask
				if readErr := db.First(&persisted, task.ID).Error; readErr != nil || persisted.LastExecutionID != nil {
					t.Fatal("an unstarted submission changed the last actual run", readErr)
				}
			} else {
				id, err = e.ExecuteContent(ctx, "workflow", content, cfg, nil, 3, 31, "addp_at_user", "manual", 300, "")
			}
			success := scenario.status == 201
			if success != (err == nil && id != "") {
				t.Fatal("unexpected submission result", id, err)
			}
			expectedPrecise, expectedGeneric := scenario.wantCalls, int32(0)
			if scenario.name == "other format" {
				expectedPrecise, expectedGeneric = 0, 1
			}
			if eaCalls.Load() != expectedPrecise || genericCalls.Load() != expectedGeneric {
				t.Fatal("unexpected authorization path", eaCalls.Load(), genericCalls.Load(), err)
			}
			if (notifications.Load() == 1) != success {
				t.Fatal("unexpected execution wakeup", notifications.Load(), err)
			}
			var rows []models.FileExecution
			if err := db.Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if scenario.wantState == "" {
				if len(rows) != 0 {
					t.Fatal("unexpected private execution")
				}
				return
			}
			if len(rows) != 1 || rows[0].AdmissionState != scenario.wantState || (len(rows[0].IssuedAuthorizationJSON) != 0) != success {
				t.Fatal("wrong durable admission state", len(rows))
			}
			if _, lease, err := repository.NewExecutionRepository(db).ClaimNext("workflow", t.Context(), "fixture", time.Now(), time.Minute, nil); err != nil || lease != nil {
				t.Fatal("private execution was claimable before ready", lease, err)
			}
			if scenario.name == "saved admission rejected" {
				facts, readErr := fileRepo.Read(ctx, 3, id)
				if readErr != nil {
					t.Fatal(readErr)
				}
				now := time.Now()
				if err := fileRepo.ApplyAdmission(ctx, 3, id, client.DevelopFileAdmissionObservation{
					AuthorizationID: facts.AuthorizationID, ExecutionID: id, ExpectedAttempt: 1, ScopeDigest: facts.ScopeDigest,
					State: "rejected", ObservedAt: now, ExpiresAt: now.Add(time.Minute),
				}); err != nil {
					t.Fatal(err)
				}
				facts, readErr = fileRepo.Read(ctx, 3, id)
				var task models.DevTask
				if readErr != nil || facts.AdmissionState != "rejected" || db.First(&task).Error != nil || task.LastExecutionID != nil {
					t.Fatal("rejected observation changed the last actual run or left admission pending", readErr)
				}
			}
			if scenario.name == "temporary" {
				var old execution.TaskExecution
				if err := db.Where("execution_id = ?", id).First(&old).Error; err != nil {
					t.Fatal(err)
				}
				task, err := devRuntimeTaskFromExecution(&old)
				if err != nil {
					t.Fatal(err)
				}
				worker := &ExecutionService{executor: e}
				if _, err := worker.workflowAuthorization(ctx, &old, execution.Lease{}, task); err == nil {
					t.Fatal("old generic worker accepted a protected GeoPackage route")
				}
				child := uuid.New()
				if _, err := e.prepareWorkflowExecutionAuthorizationFromExecution(ctx, task, 3, uuid.New(), child,
					execution.Lease{ExecutionID: child.String(), TenantID: 3, Attempt: 1, Token: uuid.NewString()}); err == nil {
					t.Fatal("parent authorization accepted a protected GeoPackage route")
				}
				if genericCalls.Load() != 0 || eaCalls.Load() != 1 {
					t.Fatal("generic or parent fallback issued another authorization")
				}
			}

		})
	}
}

func fileSubmissionDiscovery(t *testing.T) *OperatorDiscoveryService {
	t.Helper()
	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewWorkflowCapabilities("geopython_workflow", plugin.WorkflowRuntimeAPIAddpV1))
	if err != nil {
		t.Fatal(err)
	}
	capJSON := commonModels.JSONString(capabilities)
	return &OperatorDiscoveryService{
		getRuntimeDescriptor: func(context.Context, uint, uint) (*commonModels.EngineRuntimeDescriptor, error) {
			return &commonModels.EngineRuntimeDescriptor{ID: 50, EngineType: "geopython_workflow", LifecycleState: "active", ConnectionStatus: commonModels.EngineConnectionOnline,
				Capabilities: &capJSON, RuntimeEndpoint: &commonModels.EngineRuntimeEndpoint{Protocol: "http", Host: "fixture", Port: 8099}}, nil
		},
		listWorkflowOperators: func(context.Context, *commonModels.Engine) ([]commonModels.OperatorDescriptor, error) {
			return []commonModels.OperatorDescriptor{
				{ID: "load", Name: "load", EngineType: "geopython_workflow", ExecutionModes: []string{"workflow"}, Effects: []string{"read"},
					Parameters: []commonModels.ParameterDescriptor{{Name: "connection_info"}, {Name: "schema"}, {Name: "table"}, {Name: "path"}, {Name: "geojson"}}},
				{ID: "save", Name: "save", EngineType: "geopython_workflow", ExecutionModes: []string{"workflow"}, Effects: []string{"write"},
					Parameters: []commonModels.ParameterDescriptor{{Name: "connection_info"}, {Name: "schema"}, {Name: "table"}, {Name: "path"}, {Name: "mode"}}},
			}, nil
		},
	}
}
