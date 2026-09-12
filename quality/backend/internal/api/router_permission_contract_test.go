package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/authorization/authtest"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/modulelifecycle"
	qualityauthorization "github.com/addp/quality/internal/authorization"
	"github.com/gin-gonic/gin"
)

func TestQualityHumanExecutionRoutesUseMonitorExecutionRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authServer := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{
		"Bearer monitor-read":       {"monitor.execution.read"},
		"Bearer quality-task-read":  {qualityauthorization.PermissionQualityCheckTaskRead},
		"Bearer task-provider-read": {qualityauthorization.PermissionQualityTaskProviderRead},
	})
	defer authServer.Close()

	db := newExecutionHandlerTestDB(t)
	insertExecutionHandlerRow(t, db, 1, 7, "quality-7", commonExecution.ModuleQuality, commonExecution.TaskTypeQualityCheck, commonExecution.ExecutionStatusSuccess)
	router := SetupRouter(nil, nil, nil, nil, nil, nil, db, authServer.URL, nil, modulelifecycle.NewStandalone("quality"))

	for _, test := range []struct {
		name  string
		path  string
		token string
		want  int
	}{
		{name: "monitor reader can list", path: "/api/v1/quality/executions", token: "monitor-read", want: http.StatusOK},
		{name: "monitor reader can inspect detail", path: "/api/v1/quality/executions/quality-7", token: "monitor-read", want: http.StatusOK},
		{name: "quality task reader cannot inspect execution", path: "/api/v1/quality/executions/quality-7", token: "quality-task-read", want: http.StatusForbidden},
		{name: "machine permission cannot inspect human route", path: "/api/v1/quality/executions/quality-7", token: "task-provider-read", want: http.StatusForbidden},
		{name: "user cannot call TaskProvider", path: "/api/v1/quality/task-provider/tasks/unsupported/1", token: "task-provider-read", want: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Header.Set("Authorization", "Bearer "+test.token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
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
	insertExecutionHandlerRow(t, db, 1, 7, "provider-quality-7", commonExecution.ModuleQuality, commonExecution.TaskTypeQualityCheck, commonExecution.ExecutionStatusSuccess)
	router := SetupRouter(nil, nil, nil, nil, nil, nil, db, authServer.URL, nil, modulelifecycle.NewStandalone("quality"))

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
