package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	transferauthorization "github.com/addp/transfer/internal/authorization"
	"github.com/gin-gonic/gin"
)

func TestTransferTaskProviderRoutesRequireOrchestratorRuntimeIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer orchestrator-read": {
			ClientID:    "addp-orchestrator",
			Permissions: []string{transferauthorization.PermissionTransferTaskProviderRead},
		},
		"Bearer wrong-client": {
			ClientID:    "addp-develop",
			Permissions: []string{transferauthorization.PermissionTransferTaskProviderRead},
		},
		"Bearer user-task-read": {
			ClientID:    "addp-orchestrator",
			Permissions: []string{transferauthorization.PermissionTransferTaskRead},
		},
	})
	defer authServer.Close()

	router := SetupRouter(nil, nil, nil, authServer.URL, "", nil, nil, nil, modulelifecycle.NewStandalone("transfer"))

	for _, test := range []struct {
		name  string
		path  string
		token string
		want  int
	}{
		{name: "orchestrator reaches provider handler", path: "/api/v1/transfer/task-provider/tasks/unsupported/1", token: "orchestrator-read", want: http.StatusBadRequest},
		{name: "wrong client rejected", path: "/api/v1/transfer/task-provider/tasks/unsupported/1", token: "wrong-client", want: http.StatusForbidden},
		{name: "broad user permission rejected", path: "/api/v1/transfer/task-provider/tasks/unsupported/1", token: "user-task-read", want: http.StatusForbidden},
		{name: "provider permission cannot read user task definitions", path: "/api/v1/transfer/task-definitions/1", token: "orchestrator-read", want: http.StatusForbidden},
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

	oldRoute := httptest.NewRecorder()
	router.ServeHTTP(oldRoute, httptest.NewRequest(http.MethodGet, "/api/v1/transfer/tasks/sync/1", nil))
	if oldRoute.Code != http.StatusNotFound {
		t.Fatalf("legacy mixed route status = %d, want %d", oldRoute.Code, http.StatusNotFound)
	}
}
