package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	managerauthorization "github.com/addp/manager/internal/authorization"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/service"
)

func TestManagerTaskProviderRoutesRequireOrchestratorRuntimeIdentity(t *testing.T) {
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer orchestrator-read": {ClientID: "addp-orchestrator", Permissions: []string{managerauthorization.PermissionManagerTaskProviderRead}},
		"Bearer wrong-client":      {ClientID: "addp-meta", Permissions: []string{managerauthorization.PermissionManagerTaskProviderRead}},
		"Bearer human-task-read":   {ClientID: "addp-orchestrator", Permissions: []string{managerauthorization.PermissionManagerDerivedArtifactRead}},
	})
	defer authServer.Close()

	cfg := &config.Config{}
	cfg.SystemServiceURL = authServer.URL
	taskHandler := NewTaskProviderHandler(nil, nil, nil, nil, nil)
	metadataService := service.NewMetadataService(nil, nil, nil, nil, nil)
	router := SetupRouter(
		cfg, metadataService, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, taskHandler, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, modulelifecycle.NewStandalone("manager"), nil, nil,
	)

	for _, test := range []struct {
		name, path, token string
		want              int
	}{
		{name: "orchestrator reaches provider handler", path: "/api/v1/manager/task-provider/tasks/unsupported/1", token: "orchestrator-read", want: http.StatusBadRequest},
		{name: "wrong client rejected", path: "/api/v1/manager/task-provider/tasks/unsupported/1", token: "wrong-client", want: http.StatusForbidden},
		{name: "human task permission rejected", path: "/api/v1/manager/task-provider/tasks/unsupported/1", token: "human-task-read", want: http.StatusForbidden},
		{name: "provider permission cannot call human task API", path: "/api/v1/manager/tasks/unsupported/1", token: "orchestrator-read", want: http.StatusForbidden},
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
