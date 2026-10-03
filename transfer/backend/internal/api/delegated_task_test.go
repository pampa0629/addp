package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/service"
)

func TestDelegatedCreatePersistsStoppedTaskWithoutExecution(t *testing.T) {
	db := newTransferTaskHandlerTestDB(t)
	for _, tc := range []struct {
		name          string
		method        string
		path          string
		mutateAuth    func(*authorization.AuthContext)
		mutateRequest func(*models.CreateTaskRequest)
		want          int
	}{
		{name: "create", want: 201},
		{name: "wrong_audience", mutateAuth: func(a *authorization.AuthContext) { a.Client.Audiences = []string{"ontology"} }, want: 403},
		{name: "extra_scope", mutateAuth: func(a *authorization.AuthContext) { a.Client.Scopes = append(a.Client.Scopes, "transfer.task.execute") }, want: 403},
		{name: "missing_permission", mutateAuth: func(a *authorization.AuthContext) {
			a.Authorization.RoleAssignments[0].Permissions = []string{"transfer.task.read"}
		}, want: 403},
		{name: "project_not_tenant", mutateAuth: func(a *authorization.AuthContext) {
			id := "7"
			a.Authorization.RoleAssignments[0].Scope.Type = "project_group"
			a.Authorization.RoleAssignments[0].Scope.ProjectGroupID = &id
		}, want: 403},
		{name: "cannot_read_tasks", method: "GET", want: 403},
		{name: "cannot_start", path: "/1/start", want: 403},
		{name: "schedule_rejected", mutateRequest: func(r *models.CreateTaskRequest) { r.Schedule = "* * * * *" }, want: 400},
		{name: "enabled_rejected", mutateRequest: func(r *models.CreateTaskRequest) { yes := true; r.Enabled = &yes }, want: 400},
		{name: "scan_rejected", mutateRequest: func(r *models.CreateTaskRequest) { r.AutoScanMetadata = nil }, want: 400},
		{name: "continuous_rejected", mutateRequest: func(r *models.CreateTaskRequest) {
			r.Config["runtime"] = map[string]interface{}{"boundary": "continuous"}
		}, want: 400},
		{name: "incremental_rejected", mutateRequest: func(r *models.CreateTaskRequest) { r.Config["load"] = map[string]interface{}{"mode": "incremental"} }, want: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := authtest.NewTenantUserAuthContext("7", "91", []string{"transfer.task.create", "transfer.task.execute", "transfer.task.read"})
			a.Token.Type = "delegated_access_token"
			a.Client.ScopeMode = "restricted"
			a.Client.Audiences = []string{"transfer"}
			a.Client.Scopes = []string{"transfer.task.create"}
			a.Delegation = &authorization.DelegationFacts{DelegatedByClientID: *a.Client.ClientID, AgentRunID: "run", ToolCallID: "call"}
			if tc.mutateAuth != nil {
				tc.mutateAuth(&a)
			}
			system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(a) }))
			defer system.Close()
			no := false
			config := validTransferTaskHandlerConfig()
			config["source"].(map[string]interface{})["locator"] = "addp://engine/1/path/Outdoor/Activities?type=collection"
			config["source"].(map[string]interface{})["query"] = map[string]interface{}{
				"language": "mql", "statement": `{"aggregate":"Activities","pipeline":[{"$project":{"activity_id":"$_id","status":1,"_id":0}}]}`,
			}
			config["transforms"] = []interface{}{map[string]interface{}{
				"type": "field_mapping", "version": "v1", "mode": "project",
				"fields": []interface{}{
					map[string]interface{}{"source": "activity_id", "target": "activity_id", "target_type": "string", "nullable": false},
					map[string]interface{}{"source": "status", "target": "status", "target_type": "string", "nullable": true},
				},
			}}
			config["target"] = map[string]interface{}{"parent_locator": "addp://engine/2/path/demo?type=schema", "name": "outdoor_activities", "data_type": "table", "representation": "native", "policy": map[string]interface{}{"apply_mode": "replace"}}
			r := models.CreateTaskRequest{Name: "Outdoor MongoDB to PG fixture", TaskType: "sync", Config: config, Enabled: &no, AutoScanMetadata: &no}
			if tc.mutateRequest != nil {
				tc.mutateRequest(&r)
			}
			payload, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			svc := service.NewTaskService(db, nil, nil)
			svc.SetEngineResolver(planner.StaticEngineResolver{1: {Type: "mongodb"}})
			router := SetupRouter(svc, nil, nil, nil, system.URL, "", nil, nil, nil, modulelifecycle.NewStandalone("transfer"))
			method := tc.method
			if method == "" {
				method = "POST"
			}
			request := httptest.NewRequest(method, "/api/v1/transfer/task-definitions"+tc.path, strings.NewReader(string(payload)))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer addp_dat_fixture")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.want, response.Body)
			}
			if response.Code == 201 {
				var task models.TransferTask
				if err := json.Unmarshal(response.Body.Bytes(), &task); err != nil {
					t.Fatal(err)
				}
				if task.ID == 0 || task.TenantID != 7 || task.CreatedBy == nil || *task.CreatedBy != 91 || task.Status != models.TaskStatusIdle || task.DesiredState != models.TaskDesiredStateStopped || task.Enabled || task.AutoScanMetadata || task.NextRunAt != nil || task.LastExecutionID != nil {
					t.Fatalf("unexpected task effects: %+v", task)
				}
			}
		})
	}
	var tasks, executions int64
	if err := db.Model(&models.TransferTask{}).Count(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.TaskExecution{}).Count(&executions).Error; err != nil {
		t.Fatal(err)
	}
	if tasks != 1 || executions != 0 {
		t.Fatalf("tasks=%d executions=%d", tasks, executions)
	}
}
