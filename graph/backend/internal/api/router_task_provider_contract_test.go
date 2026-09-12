package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	graphauthorization "github.com/addp/graph/internal/authorization"
	"github.com/addp/graph/internal/config"
)

func TestGraphTaskProviderRoutesRequireOrchestratorRuntimeIdentity(t *testing.T) {
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer orchestrator-read": {ClientID: "addp-orchestrator", Permissions: []string{graphauthorization.PermissionGraphTaskProviderRead}},
		"Bearer wrong-client":      {ClientID: "addp-manager", Permissions: []string{graphauthorization.PermissionGraphTaskProviderRead}},
		"Bearer human-task-read":   {ClientID: "addp-orchestrator", Permissions: []string{graphauthorization.PermissionGraphBuildTaskRead}},
	})
	defer authServer.Close()

	cfg := &config.Config{}
	cfg.SystemServiceURL = authServer.URL
	router := SetupRouter(cfg, nil, nil, nil, nil, NewTaskProviderHandler(nil, nil), nil, nil, modulelifecycle.NewStandalone("graph"))

	for _, test := range []struct {
		name, path, token string
		want              int
	}{
		{name: "orchestrator reaches provider handler", path: "/api/v1/graph/task-provider/tasks/unsupported/1", token: "orchestrator-read", want: http.StatusBadRequest},
		{name: "wrong client rejected", path: "/api/v1/graph/task-provider/tasks/unsupported/1", token: "wrong-client", want: http.StatusForbidden},
		{name: "human task permission rejected", path: "/api/v1/graph/task-provider/tasks/unsupported/1", token: "human-task-read", want: http.StatusForbidden},
		{name: "provider permission cannot call human execution API", path: "/api/v1/graph/executions/execution-1", token: "orchestrator-read", want: http.StatusForbidden},
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
	router.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/api/v1/graph/tasks/kg_build/1", nil))
	if legacy.Code != http.StatusNotFound {
		t.Fatalf("legacy mixed route status = %d, want %d", legacy.Code, http.StatusNotFound)
	}
}
