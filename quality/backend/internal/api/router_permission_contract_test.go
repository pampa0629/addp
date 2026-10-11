package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/modulelifecycle"
	qualityauthorization "github.com/addp/quality/internal/authorization"
	"github.com/addp/quality/internal/repository"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
)

func TestQualityStandardReferenceGuardPermissionAndTerminalContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer standard":         {ClientID: "addp-standard", Permissions: []string{"quality.standard_reference.update"}},
		"Bearer wrong-client":     {ClientID: "addp-model", Permissions: []string{"quality.standard_reference.update"}},
		"Bearer wrong-permission": {ClientID: "addp-standard", Permissions: []string{"quality.rule.update"}},
	})
	defer authServer.Close()
	db := newExecutionHandlerTestDB(t)
	for _, query := range []string{
		`ATTACH DATABASE ':memory:' AS quality`,
		`CREATE TABLE quality.standard_reference_guards (id INTEGER PRIMARY KEY, tenant_id INTEGER, resource_type TEXT, resource_id INTEGER, state TEXT, created_at DATETIME, updated_at DATETIME, UNIQUE(tenant_id,resource_type,resource_id))`,
		`INSERT INTO quality.standard_reference_guards (tenant_id,resource_type,resource_id,state) VALUES (7,'domain',42,'deleted')`,
	} {
		if err := db.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}
	router := SetupRouter(nil, nil, nil, nil, db, authServer.URL, nil, modulelifecycle.NewStandalone("quality"), service.NewStandardReferenceGuardService(repository.NewStandardReferenceGuardRepository(db)))
	for _, tc := range []struct {
		token, state string
		want         int
	}{
		{"standard", "deleted", 200}, {"standard", "open", 409}, {"standard", "invalid", 400}, {"wrong-client", "deleted", 403}, {"wrong-permission", "deleted", 403},
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/quality/standard-reference-guards/domain/42", strings.NewReader(`{"state":"`+tc.state+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tc.token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != tc.want {
			t.Fatalf("%s %s: %d %s", tc.token, tc.state, response.Code, response.Body.String())
		}
	}
}

func TestQualityHumanExecutionRoutesUseMonitorExecutionRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer monitor-read":       {"monitor.execution.read"},
		"Bearer execution-read":     {"monitor.execution.read", qualityauthorization.PermissionQualityPlanRead},
		"Bearer quality-task-read":  {qualityauthorization.PermissionQualityPlanRead},
		"Bearer rule-read":          {"quality.rule.read"},
		"Bearer plan-editor-only":   {"quality.plan.update"},
		"Bearer task-provider-read": {qualityauthorization.PermissionQualityTaskProviderRead},
	})
	defer authServer.Close()

	db := newExecutionHandlerTestDB(t)
	insertExecutionHandlerRow(t, db, 1, 7, "quality-7", commonExecution.ModuleQuality, commonExecution.TaskTypeQualityPlan, commonExecution.ExecutionStatusSuccess)
	router := SetupRouter(nil, nil, nil, nil, db, authServer.URL, nil, modulelifecycle.NewStandalone("quality"), nil)

	for _, test := range []struct {
		name   string
		path   string
		token  string
		want   int
		method string
	}{
		{name: "monitor reader alone cannot list", path: "/api/v1/quality/executions", token: "monitor-read", want: http.StatusForbidden},
		{name: "owner reader can list", path: "/api/v1/quality/executions", token: "execution-read", want: http.StatusOK},
		{name: "overview requires more than monitor read", path: "/api/v1/quality/overview", token: "monitor-read", want: http.StatusForbidden},
		{name: "overview requires more than plan read", path: "/api/v1/quality/overview", token: "quality-task-read", want: http.StatusForbidden},
		{name: "monitor reader alone cannot inspect detail", path: "/api/v1/quality/executions/quality-7", token: "monitor-read", want: http.StatusForbidden},
		{name: "quality task reader cannot inspect execution", path: "/api/v1/quality/executions/quality-7", token: "quality-task-read", want: http.StatusForbidden},
		{name: "machine permission cannot inspect human route", path: "/api/v1/quality/executions/quality-7", token: "task-provider-read", want: http.StatusForbidden},
		{name: "user cannot call TaskProvider", path: "/api/v1/quality/task-provider/tasks/unsupported/1", token: "task-provider-read", want: http.StatusForbidden},
		{name: "plan reader cannot access rule library", path: "/api/v1/quality/rules", token: "quality-task-read", want: http.StatusForbidden},
		{name: "rule reader cannot see plan references without plan read", path: "/api/v1/quality/rules/1/plans", token: "rule-read", want: http.StatusForbidden},
		{name: "plan edit requires rule read", method: http.MethodPut, path: "/api/v1/quality/plans/1", token: "plan-editor-only", want: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			method := test.method
			if method == "" {
				method = http.MethodGet
			}
			request := httptest.NewRequest(method, test.path, nil)
			request.Header.Set("Authorization", "Bearer "+test.token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestQualityProfessionalExecutionOwnerReadScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	permissions := []string{"monitor.execution.read", qualityauthorization.PermissionQualityPlanRead}
	contexts := map[string]authorization.AuthContext{
		"owner":     authtest.NewTenantUserAuthContext("7", "11", permissions),
		"peer":      authtest.NewTenantUserAuthContext("7", "22", permissions),
		"foreign":   authtest.NewTenantUserAuthContext("8", "33", permissions),
		"oauth":     authtest.NewTenantUserAuthContext("7", "11", permissions),
		"delegated": authtest.NewTenantUserAuthContext("7", "11", permissions),
		"ticket":    authtest.NewTenantUserAuthContext("7", "11", permissions),
		"machine":   authtest.NewTenantUserAuthContext("7", "11", permissions),
	}
	for _, key := range []string{"oauth", "delegated", "ticket"} {
		facts := contexts[key]
		facts.Token.Type = "oauth_access_token"
		facts.Client.ScopeMode = "restricted"
		facts.Client.Scopes = []string{"addp.api"}
		if key == "delegated" {
			facts.Token.Type = "delegated_access_token"
			facts.Client.Audiences, facts.Client.Scopes = []string{"quality"}, []string{"quality.read"}
			facts.Delegation = &authorization.DelegationFacts{DelegatedByClientID: *facts.Client.ClientID, AgentRunID: "run", ToolCallID: "call"}
		}
		if key == "ticket" {
			facts.Token.Type = "resource_access_ticket"
			facts.Client.Audiences, facts.Client.Scopes = []string{"quality"}, []string{"resource:read"}
		}
		contexts[key] = facts
	}
	machine := contexts["machine"]
	machine.Principal.Type, machine.Token.Type = "service_principal", "service_access_token"
	machine.Authentication.Methods, machine.Authentication.AssuranceLevel = []string{"service_secret"}, "not_applicable"
	contexts["machine"] = machine
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/auth/context" {
			http.NotFound(w, r)
			return
		}
		facts, ok := contexts[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(facts); err != nil {
			t.Errorf("encode AuthContext: %v", err)
		}
	}))
	defer authServer.Close()
	db := newExecutionHandlerTestDB(t)
	for index, row := range []struct {
		id               string
		tenant           int
		module, taskType string
		source           interface{}
		actor            int
	}{
		{"deleted-plan", 7, "quality", "quality_plan", "999", 22},
		{"own-ad-hoc", 7, "quality", "quality_plan", nil, 11},
		{"peer-ad-hoc", 7, "quality", "quality_plan", nil, 22},
		{"unknown-actor", 7, "quality", "quality_plan", nil, 0},
		{"foreign", 8, "quality", "quality_plan", "999", 33},
		{"other-module", 7, "meta", "scan", "999", 11},
		{"other-type", 7, "quality", "cleanup_executor", "999", 11},
		{"empty-source", 7, "quality", "quality_plan", "", 11},
	} {
		insertExecutionHandlerRow(t, db, index+1, row.tenant, row.id, row.module, row.taskType, "success")
		if err := db.Exec("UPDATE common.task_executions SET source_task_id = ?, triggered_by = ? WHERE execution_id = ?", row.source, row.actor, row.id).Error; err != nil {
			t.Fatal(err)
		}
	}
	router := SetupRouter(nil, nil, nil, nil, db, authServer.URL, nil, modulelifecycle.NewStandalone("quality"), nil)
	request := func(token, path string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/quality/executions"+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != status {
			t.Fatalf("%s %s: status=%d, want=%d, body=%s", token, path, response.Code, status, response.Body.String())
		}
		return response
	}
	for _, token := range []string{"owner", "peer", "oauth"} {
		for _, page := range []string{"1", "2"} {
			response := request(token, "?page_size=1&page="+page, http.StatusOK)
			var body qualityExecutionListResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Total != 2 || body.TotalPages != 2 || len(body.Data) != 1 {
				t.Fatalf("list scope leaked: %s", response.Body.String())
			}
		}
		request(token, "/deleted-plan", http.StatusOK)
		own, denied := "own-ad-hoc", "peer-ad-hoc"
		if token == "peer" {
			own, denied = denied, own
		}
		request(token, "/"+own, http.StatusOK)
		request(token, "/"+denied, http.StatusNotFound)
	}
	for _, id := range []string{"unknown-actor", "foreign", "other-module", "other-type", "empty-source", "missing"} {
		request("owner", "/"+id, http.StatusNotFound)
	}
	request("foreign", "/deleted-plan", http.StatusNotFound)
	for _, token := range []string{"machine", "delegated", "ticket"} {
		request(token, "", http.StatusForbidden)
		request(token, "/deleted-plan", http.StatusForbidden)
	}
	revoked := contexts["owner"]
	revoked.Authorization.RoleAssignments[0].Permissions = []string{"monitor.execution.read"}
	contexts["owner"] = revoked
	request("owner", "", http.StatusForbidden)
	request("owner", "/deleted-plan", http.StatusForbidden)
}

func TestQualityTaskProviderRoutesRequireOrchestratorRuntimeIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer orchestrator-read": {
			ClientID:    "addp-orchestrator",
			Permissions: []string{qualityauthorization.PermissionQualityTaskProviderRead},
		},
		"Bearer orchestrator-execute": {
			ClientID:    "addp-orchestrator",
			Permissions: []string{qualityauthorization.PermissionQualityTaskProviderExecute},
		},
		"Bearer wrong-client": {
			ClientID:    "addp-develop",
			Permissions: []string{qualityauthorization.PermissionQualityTaskProviderRead},
		},
		"Bearer wrong-client-execute": {
			ClientID:    "addp-develop",
			Permissions: []string{qualityauthorization.PermissionQualityTaskProviderExecute},
		},
		"Bearer human-read": {
			ClientID:    "addp-orchestrator",
			Permissions: []string{"monitor.execution.read"},
		},
	})
	defer authServer.Close()

	db := newExecutionHandlerTestDB(t)
	insertExecutionHandlerRow(t, db, 1, 7, "provider-quality-7", commonExecution.ModuleQuality, commonExecution.TaskTypeQualityPlan, commonExecution.ExecutionStatusSuccess)
	router := SetupRouter(nil, nil, nil, nil, db, authServer.URL, nil, modulelifecycle.NewStandalone("quality"), nil)

	for _, test := range []struct {
		name   string
		method string
		path   string
		token  string
		want   int
	}{
		{name: "orchestrator reaches provider task handler", method: http.MethodGet, path: "/api/v1/quality/task-provider/tasks/unsupported/1", token: "orchestrator-read", want: http.StatusBadRequest},
		{name: "orchestrator reads provider execution", method: http.MethodGet, path: "/api/v1/quality/task-provider/executions/provider-quality-7", token: "orchestrator-read", want: http.StatusOK},
		{name: "orchestrator reaches provider execute handler", method: http.MethodPost, path: "/api/v1/quality/task-provider/tasks/unsupported/1/execute", token: "orchestrator-execute", want: http.StatusBadRequest},
		{name: "wrong read client rejected", method: http.MethodGet, path: "/api/v1/quality/task-provider/tasks/unsupported/1", token: "wrong-client", want: http.StatusForbidden},
		{name: "wrong execute client rejected", method: http.MethodPost, path: "/api/v1/quality/task-provider/tasks/unsupported/1/execute", token: "wrong-client-execute", want: http.StatusForbidden},
		{name: "human read permission rejected", method: http.MethodGet, path: "/api/v1/quality/task-provider/tasks/unsupported/1", token: "human-read", want: http.StatusForbidden},
		{name: "read permission cannot execute", method: http.MethodPost, path: "/api/v1/quality/task-provider/tasks/unsupported/1/execute", token: "orchestrator-read", want: http.StatusForbidden},
		{name: "provider permission cannot read human execution route", method: http.MethodGet, path: "/api/v1/quality/executions/provider-quality-7", token: "orchestrator-read", want: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			request.Header.Set("Authorization", "Bearer "+test.token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}

	legacyRoute := httptest.NewRecorder()
	router.ServeHTTP(legacyRoute, httptest.NewRequest(http.MethodGet, "/api/v1/quality/tasks/check/1", nil))
	if legacyRoute.Code != http.StatusNotFound {
		t.Fatalf("legacy mixed route status = %d, want %d", legacyRoute.Code, http.StatusNotFound)
	}
	legacyExecuteRoute := httptest.NewRecorder()
	router.ServeHTTP(legacyExecuteRoute, httptest.NewRequest(http.MethodPost, "/api/v1/quality/tasks/check/1/execute", nil))
	if legacyExecuteRoute.Code != http.StatusNotFound {
		t.Fatalf("legacy mixed execute route status = %d, want %d", legacyExecuteRoute.Code, http.StatusNotFound)
	}
}
