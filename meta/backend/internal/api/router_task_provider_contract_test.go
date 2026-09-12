package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	metaauthorization "github.com/addp/meta/internal/authorization"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/service"
)

func TestMetaTaskProviderRoutesRequireOrchestratorRuntimeIdentity(t *testing.T) {
	authServer := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer orchestrator-read": {ClientID: "addp-orchestrator", Permissions: []string{metaauthorization.PermissionMetaTaskProviderRead}},
		"Bearer wrong-client":      {ClientID: "addp-manager", Permissions: []string{metaauthorization.PermissionMetaTaskProviderRead}},
		"Bearer human-task-read":   {ClientID: "addp-orchestrator", Permissions: []string{metaauthorization.PermissionMetaScanTaskRead}},
	})
	defer authServer.Close()

	db := metatest.OpenMetadataDB(t)
	engineService := service.NewEngineService(db, nil)
	scanService := service.NewScanService(db, engineService)
	cfg := &config.Config{}
	cfg.SystemServiceURL = authServer.URL
	router := SetupRouter(cfg, db, engineService, scanService, nil, nil, nil, nil, modulelifecycle.NewStandalone("meta"))

	for _, test := range []struct {
		name, path, token string
		want              int
	}{
		{name: "orchestrator reaches provider handler", path: "/api/v1/meta/task-provider/tasks/unsupported/1", token: "orchestrator-read", want: http.StatusServiceUnavailable},
		{name: "wrong client rejected", path: "/api/v1/meta/task-provider/tasks/unsupported/1", token: "wrong-client", want: http.StatusForbidden},
		{name: "human task permission rejected", path: "/api/v1/meta/task-provider/tasks/unsupported/1", token: "human-task-read", want: http.StatusForbidden},
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
	router.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/api/v1/meta/tasks/scan/1", nil))
	if legacy.Code != http.StatusNotFound {
		t.Fatalf("legacy mixed route status = %d, want %d", legacy.Code, http.StatusNotFound)
	}
}
