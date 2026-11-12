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
		saving    bool
		status    int
		wantState string
		wantCalls int32
	}{
		{"temporary", false, false, 201, "pending", 1},
		{"saved", true, false, 201, "pending", 1},
		{"saved admission rejected", true, false, 201, "pending", 1},
		{"denied", true, false, 403, "rejected", 1},
		{"unknown", false, false, 503, "pending", 1},
		{"wrong layout", false, false, 0, "", 0},
		{"integrity unverified", false, false, 201, "pending", 1},
		{"unknown format", false, false, 0, "", 0},
		{"metadata mismatch", false, false, 0, "", 0},
		{"wrong snapshot identity", false, false, 0, "", 0},
		{"mixed table", false, false, 0, "", 0},
		{"save", false, false, 0, "", 0},
		{"save temporary", false, true, 201, "pending", 1},
		{"save task", true, true, 201, "pending", 1},
		{"save denied", false, true, 403, "rejected", 1},
		{"save unknown", true, true, 503, "pending", 1},
		{"save overwrite", false, true, 0, "", 0},
		{"save append", false, true, 0, "", 0},
		{"save multiple", false, true, 0, "", 0},
		{"save traversal", false, true, 0, "", 0},
		{"save nested name", false, true, 0, "", 0},
		{"save wrong format", false, true, 0, "", 0},
		{"save wrong parent", false, true, 0, "", 0},
		{"save missing input", false, true, 0, "", 0},
		{"save connection override", false, true, 0, "", 0},
		{"save mixed source", false, true, 0, "", 0},
		{"save external effect", false, true, 0, "", 0},
		{"other format", false, false, 201, "", 1},
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
			auth := authtest.NewTenantUserAuthContext("3", "31", []string{"develop.task.execute", "develop.data_read.execute", "develop.data_write.execute"})
			ctx := WithExecutionAuthContext(t.Context(), auth)
			path := "sources/ordinary.bin"
			if scenario.name == "metadata mismatch" {
				path = "sources/snapshot.gpkg"
			} else if scenario.name == "other format" {
				path = "sources/snapshot.geojson"
			}

			var eaCalls, genericCalls, notifications, admissionCalls atomic.Int32
			approvalID := uuid.NewString()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/meta/items/by-catalog-path":
					if r.URL.Query().Get("engine_id") != "11" || r.URL.Query().Get("catalog_path") != path || r.Header.Get("Authorization") != "Bearer meta-fixture" {
						t.Error("Meta request did not use exact path and tenant service")
					}
					name := path
					format, kind, layout := "geopackage", "container", "single"
					switch scenario.name {
					case "wrong layout":
						layout = "multi"
					case "unknown format":
						format = "unknown"
					case "metadata mismatch", "other format":
						format, kind = "geojson", "table"
					case "wrong snapshot identity":
						name = "sources/other"
					}
					attributes := map[string]interface{}{"item": map[string]interface{}{"format": format, "layout": layout, "data_type": kind}}
					if scenario.name == "integrity unverified" {
						attributes["format_info"] = map[string]interface{}{"geopackage": map[string]interface{}{"integrity_verified": false}}
					}
					_ = json.NewEncoder(w).Encode(commonModels.MetaItem{ID: 19, TenantID: 3, EngineID: 11, FullName: name, ItemType: "file",
						Attributes: attributes})
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
						(pending.Intent.Target != nil) != scenario.saving || len(pending.Intent.Sources) != 1 || pending.Intent.Sources[0].Target.StringPath() != path {
						t.Error("wrong pending intent")
					}
					if scenario.status != 201 {
						w.WriteHeader(scenario.status)
						return
					}
					targetVersion := ""
					if scenario.saving {
						if pending.Intent.Target.NodeID != "save_result" || pending.Intent.Target.Target.EngineID != 12 || pending.Intent.Target.Target.StringPath() != "outputs/result.gpkg" || pending.Intent.Target.Mode != "fail" {
							t.Error("target was not frozen exactly")
						}
						targetVersion = "4"
					}
					scope, err := execution.NewDevelopBoundFileScope(pending.Intent, []execution.DevelopFileSourceReference{{
						Target: pending.Intent.Sources[0].Target, ItemFingerprint: strings.Repeat("a", 64), BoundaryHash: strings.Repeat("b", 64), EngineVersion: "2",
					}}, targetVersion)
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
				case "/api/v1/system/execution-authorizations/9/develop-geopackage-admission-checks":
					if r.Header.Get("Authorization") != "Bearer addp_at_current_machine" {
						t.Error("admission used a retained user credential")
					}
					var request client.DevelopFileAdmissionRequest
					if json.NewDecoder(r.Body).Decode(&request) != nil {
						t.Error("invalid observation request")
					}
					state, approval := "awaiting_approval", ""
					if admissionCalls.Add(1) > 1 {
						state, approval = "ready", approvalID
					}
					now := time.Now()
					_ = json.NewEncoder(w).Encode(client.DevelopFileAdmissionObservation{AuthorizationID: "9", ExecutionID: request.ExecutionID, ExpectedAttempt: request.ExpectedAttempt, ScopeDigest: request.ScopeDigest, State: state, ApprovalID: approval, ObservedAt: now, ExpiresAt: now.Add(time.Minute)})
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
			if scenario.saving {
				params := map[string]interface{}{"input_df": map[string]interface{}{"$ref": "a"}, "mode": "fail", "target_parent_locator": "addp://engine/12/path/outputs?type=directory", "target_name": "result.gpkg"}
				switch scenario.name {
				case "save overwrite":
					params["mode"] = "replace"
				case "save append":
					params["mode"] = "append"
				case "save traversal":
					params["target_name"] = "../result.gpkg"
				case "save nested name":
					params["target_name"] = "nested/result.gpkg"
				case "save wrong format":
					params["target_name"] = "result.geojson"
				case "save wrong parent":
					params["target_parent_locator"] = "addp://engine/12/path/outputs?type=bucket"
				case "save missing input":
					delete(params, "input_df")
				case "save connection override":
					params["connection_info"] = map[string]interface{}{"host": "forbidden"}
				case "save mixed source":
					nodes = append(nodes, map[string]interface{}{"id": "other", "operator": "load", "params": map[string]interface{}{"locator": "addp://engine/11/path/public/table?type=table"}, "depends_on": []interface{}{}})
				case "save external effect":
					nodes = append(nodes, map[string]interface{}{"id": "external", "operator": "external", "params": map[string]interface{}{}, "depends_on": []interface{}{}})
				}
				nodes = append(nodes, map[string]interface{}{"id": "save_result", "operator": "save", "params": params, "depends_on": []interface{}{"a"}})
				if scenario.name == "save multiple" {
					nodes = append(nodes, map[string]interface{}{"id": "second_save", "operator": "save", "params": params, "depends_on": []interface{}{"a"}})
				}
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
				if scenario.name != "other format" {
					var count int64
					if countErr := db.Model(&execution.TaskExecution{}).Count(&count).Error; countErr != nil || count != 0 {
						t.Fatal("rejected input left an execution behind", count, countErr)
					}
				}
				return
			}
			if len(rows) != 1 || rows[0].AdmissionState != scenario.wantState || (len(rows[0].IssuedAuthorizationJSON) != 0) != success {
				t.Fatal("wrong durable admission state", len(rows))
			}
			if _, lease, err := repository.NewExecutionRepository(db).ClaimNext("workflow", t.Context(), "fixture", time.Now(), time.Minute, nil); err != nil || lease != nil {
				t.Fatal("private execution was claimable before ready", lease, err)
			}
			if scenario.saving && success {
				sdk := client.NewSystemServiceClient(server.URL, staticServiceTokenSource("addp_at_current_machine"), server.Client())
				e.workflowEngine = NewWorkflowEngineService(sdk)
				service, createErr := NewExecutionService(e, repository.NewExecutionRepository(db), fileRepo, "submission-fixture")
				if createErr != nil {
					t.Fatal(createErr)
				}
				if recoverErr := service.RecoverPrivateFiles(ctx); recoverErr != nil {
					t.Fatal(recoverErr)
				}
				pending, readErr := fileRepo.Read(ctx, 3, id)
				if readErr != nil || pending.AdmissionState != "pending" {
					t.Fatal("approval wait activated execution", readErr)
				}
				if _, lease, claimErr := repository.NewExecutionRepository(db).ClaimNext("workflow", ctx, "fixture", time.Now(), time.Minute, nil); claimErr != nil || lease != nil {
					t.Fatal("unapproved save was claimable", claimErr)
				}
				if recoverErr := service.RecoverPrivateFiles(ctx); recoverErr != nil {
					t.Fatal(recoverErr)
				}
				ready, readErr := fileRepo.Read(ctx, 3, id)
				if readErr != nil || ready.AdmissionState != "ready" || ready.AuthorizationID != "9" || ready.BoundScope.Target.EngineVersion != "4" {
					t.Fatal("original save scope did not activate", readErr)
				}
				if _, lease, claimErr := repository.NewExecutionRepository(db).ClaimNext("workflow", ctx, "fixture", time.Now(), time.Minute, nil); claimErr != nil || lease == nil || lease.ExecutionID != id || lease.Attempt != 1 {
					t.Fatal("approved original save was not claimable", claimErr)
				}
				if eaCalls.Load() != 1 || genericCalls.Load() != 0 || admissionCalls.Load() != 2 {
					t.Fatal("save reissued or used generic authorization")
				}
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
				{ID: "external", Name: "external", EngineType: "geopython_workflow", ExecutionModes: []string{"workflow"}, Effects: []string{"external_effect"}},
				{ID: "load", Name: "load", EngineType: "geopython_workflow", ExecutionModes: []string{"workflow"}, Effects: []string{"read"},
					Parameters: []commonModels.ParameterDescriptor{{Name: "connection_info"}, {Name: "schema"}, {Name: "table"}, {Name: "path"}, {Name: "geojson"}}},
				{ID: "save", Name: "save", EngineType: "geopython_workflow", ExecutionModes: []string{"workflow"}, Effects: []string{"write"},
					Parameters: []commonModels.ParameterDescriptor{{Name: "input_df"}, {Name: "connection_info"}, {Name: "schema"}, {Name: "table"}, {Name: "path"}, {Name: "mode"}}},
			}, nil
		},
	}
}
