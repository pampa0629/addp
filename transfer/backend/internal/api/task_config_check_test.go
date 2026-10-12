package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/service"
)

type configCheckRouteFacts struct{ calls int }

func (f *configCheckRouteFacts) GetEngineForTenant(_ context.Context, tenant, id uint) (*commonmodels.Engine, error) {
	f.calls++
	pg, _ := plugin.Get("postgresql")
	caps, _ := plugin.MarshalEngineCapabilities(pg.Capabilities())
	declared := commonmodels.JSONString(caps)
	return &commonmodels.Engine{ID: id, TenantID: &tenant, EngineType: "postgresql", Version: 1, LifecycleState: "active", Capabilities: &declared}, nil
}
func (f *configCheckRouteFacts) GetItemSnapshot(context.Context, uint, uint, uint, string) (*commonmodels.MetaItem, error) {
	f.calls++
	return nil, commonapi.ErrNotFound
}

// Only control-plane snapshots are available; no business connection is used.
// Authentication failures must not reach any service-side fact read.
func TestTaskConfigCheckRoutePermissionsAndDelegatedIsolation(t *testing.T) {
	for _, name := range []string{"console", "delegated", "missing_meta", "missing_system", "missing_create", "project_scope", "service", "wrong_audience", "extra_scope", "create_scope", "check_cannot_create", "expired", "dependency_failure", "invalid_request"} {
		t.Run(name, func(t *testing.T) {
			db := newTransferTaskHandlerTestDB(t)
			permissions := []string{"transfer.task.create", "meta.catalog.read", "system.engine.read"}
			subject := authtest.NewTenantUserAuthContext("7", "91", permissions)
			delegated := name != "console" && name != "service" && name != "dependency_failure" && name != "invalid_request"
			if delegated {
				subject.Token.Type = "delegated_access_token"
				subject.Client.ScopeMode = "restricted"
				subject.Client.Audiences = []string{"transfer"}
				subject.Client.Scopes = []string{"transfer.task.config.check"}
				subject.Delegation = &authorization.DelegationFacts{DelegatedByClientID: *subject.Client.ClientID, AgentRunID: "synthetic-run", ToolCallID: "synthetic-call"}
			}
			want := http.StatusOK
			path := "/api/v1/transfer/task-config-checks"
			switch name {
			case "missing_create":
				subject.Authorization.RoleAssignments[0].Permissions = permissions[1:]
				want = 403
			case "missing_meta":
				subject.Authorization.RoleAssignments[0].Permissions = []string{"transfer.task.create", "system.engine.read"}
				want = 403
			case "missing_system":
				subject.Authorization.RoleAssignments[0].Permissions = permissions[:2]
				want = 403
			case "project_scope":
				subject.Authorization.RoleAssignments[0].Scope.Type = "project_group"
				group := "8"
				subject.Authorization.RoleAssignments[0].Scope.ProjectGroupID = &group
				want = 403
			case "service":
				subject.Principal.Type = "service_principal"
				subject.Token.Type = "service_access_token"
				subject.Authentication.Methods = []string{"service_secret"}
				subject.Authentication.AssuranceLevel = "not_applicable"
				client := "addp-transfer"
				subject.Client.ClientID = &client
				want = 403
			case "wrong_audience":
				subject.Client.Audiences = []string{"meta"}
				want = 403
			case "extra_scope":
				subject.Client.Scopes = append(subject.Client.Scopes, "transfer.task.create")
				want = 403
			case "create_scope":
				subject.Client.Scopes = []string{"transfer.task.create"}
				want = 403
			case "check_cannot_create":
				path = "/api/v1/transfer/task-definitions"
				want = 403
			case "expired":
				subject.Token.ExpiresAt = time.Now().UTC().Add(-time.Minute)
				subject.Token.IssuedAt = subject.Token.ExpiresAt.Add(-time.Hour)
				subject.Authentication.AuthenticatedAt = subject.Token.IssuedAt
				want = 401
			case "dependency_failure":
				want = 502
			case "invalid_request":
				want = 400
			}
			sort.Strings(subject.Authorization.RoleAssignments[0].Permissions)
			system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("auth dependency mutation")
				}
				_ = json.NewEncoder(w).Encode(subject)
			}))
			defer system.Close()
			svc := service.NewTaskService(db, nil, nil)
			facts := &configCheckRouteFacts{}
			if name == "dependency_failure" {
				svc.SetTaskConfigChecker(service.NewTaskConfigCheckService(nil, nil))
			} else {
				svc.SetTaskConfigChecker(service.NewTaskConfigCheckService(facts, facts))
			}
			router := SetupRouter(svc, nil, nil, nil, system.URL, "", nil, nil, nil, modulelifecycle.NewStandalone("transfer"))
			config := validTransferTaskHandlerConfig()
			config["source"].(map[string]interface{})["locator"] = "addp://engine/1/path/synthetic/source?type=table"
			config["source"].(map[string]interface{})["query"] = map[string]interface{}{"language": "sql", "statement": "SELECT private_expression FROM synthetic.source"}
			config["transforms"] = []interface{}{map[string]interface{}{"type": "field_mapping", "mode": "project", "fields": []interface{}{map[string]interface{}{"source": "payload", "target": "payload", "target_type": "json", "nullable": true}}}}
			config["target"] = map[string]interface{}{"parent_locator": "addp://engine/2/path/synthetic?type=schema", "name": "target", "data_type": "table", "representation": "native", "policy": map[string]interface{}{"apply_mode": "replace"}}
			if name == "dependency_failure" {
				delete(config["source"].(map[string]interface{}), "query")
			}
			no := false
			payload, _ := json.Marshal(models.CreateTaskRequest{Name: "synthetic check", TaskType: "sync", Config: config, Enabled: &no, AutoScanMetadata: &no})
			if name == "invalid_request" {
				payload = append(payload, []byte("{}")...)
			}
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(payload)))
			request.Header.Set("Authorization", "Bearer addp_dat_fixture")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body)
			}
			if response.Code == 200 {
				if facts.calls != 4 {
					t.Fatalf("facts reads=%d", facts.calls)
				}
				var result service.TaskConfigCheckReport
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Status != "unknown" || result.ConfigHash == "" || result.BindingHash == "" || response.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("result=%+v", result)
				}
				if strings.Contains(response.Body.String(), "private_expression") {
					t.Fatal("query echoed")
				}
			}
			if response.Code != 200 && facts.calls != 0 {
				t.Fatal("rejected request read facts")
			}
			var tasks, executions int64
			if err := db.Model(&models.TransferTask{}).Count(&tasks).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&models.TaskExecution{}).Count(&executions).Error; err != nil {
				t.Fatal(err)
			}
			if tasks != 0 || executions != 0 {
				t.Fatalf("tasks=%d executions=%d", tasks, executions)
			}
		})
	}
}

func TestTaskConfigCheckDelegatedPolicyDeclaresCompletePermissionIntersection(t *testing.T) {
	policy := transferDelegatedToolPolicies()["POST /api/v1/transfer/task-config-checks"]
	if len(policy.RequiredScopes) != 1 || policy.RequiredScopes[0] != "transfer.task.config.check" {
		t.Fatalf("unexpected scopes: %v", policy.RequiredScopes)
	}
	want := []string{"meta.catalog.read", "system.engine.read", "transfer.task.create"}
	actual := append([]string(nil), policy.RequiredPermissions...)
	sort.Strings(actual)
	if strings.Join(actual, ",") != strings.Join(want, ",") {
		t.Fatalf("delegated guard does not declare complete all-of: %v", actual)
	}
}
