package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	orchestratorauthorization "github.com/addp/orchestrator/internal/authorization"
)

func TestOrchestratorTaskProviderRoutesRequireRuntimeIdentity(t *testing.T) {
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer orchestrator-read": {ClientID: "addp-orchestrator", Permissions: []string{orchestratorauthorization.PermissionOrchestratorTaskProviderRead}},
		"Bearer wrong-client":      {ClientID: "addp-manager", Permissions: []string{orchestratorauthorization.PermissionOrchestratorTaskProviderRead}},
		"Bearer human-task-read":   {ClientID: "addp-orchestrator", Permissions: []string{orchestratorauthorization.PermissionOrchestratorWorkflowRead}},
	})
	defer authServer.Close()

	router := SetupRouter(nil, nil, nil, nil, authServer.URL, nil, nil, nil, nil, modulelifecycle.NewStandalone("orchestrator"))

	for _, test := range []struct {
		name, path, token string
		want              int
	}{
		{name: "orchestrator reaches provider handler", path: "/api/v1/orchestrator/task-provider/tasks/unsupported/1", token: "orchestrator-read", want: http.StatusBadRequest},
		{name: "wrong client rejected", path: "/api/v1/orchestrator/task-provider/tasks/unsupported/1", token: "wrong-client", want: http.StatusForbidden},
		{name: "human workflow permission rejected", path: "/api/v1/orchestrator/task-provider/tasks/unsupported/1", token: "human-task-read", want: http.StatusForbidden},
		{name: "provider permission cannot call human task catalog", path: "/api/v1/orchestrator/tasks", token: "orchestrator-read", want: http.StatusForbidden},
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

	legacy := httptest.NewRecorder()
	router.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/tasks/orchestration/1", nil))
	if legacy.Code != http.StatusNotFound {
		t.Fatalf("legacy mixed route status = %d, want %d", legacy.Code, http.StatusNotFound)
	}
}
