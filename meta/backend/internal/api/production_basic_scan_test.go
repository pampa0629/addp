package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/service"
	"github.com/google/uuid"
)

func TestProductionBasicScanRouteIsFixedTenantTransferAndBodyless(t *testing.T) {
	identity := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer transfer":     {ClientID: "addp-transfer", Permissions: []string{"meta.scan_task.execute"}},
		"Bearer system":       {ClientID: "addp-system", Permissions: []string{"meta.scan_task.execute"}},
		"Bearer unprivileged": {ClientID: "addp-transfer", Permissions: []string{"meta.catalog.read"}},
	})
	defer identity.Close()
	cfg := &config.Config{}
	cfg.SystemServiceURL = identity.URL
	engines := service.NewEngineService(nil, nil)
	router := SetupRouter(cfg, nil, engines, service.NewScanService(nil, engines), nil, nil, nil, nil, modulelifecycle.NewStandalone("meta"))
	id := uuid.NewString()
	base := "/api/v1/meta/runtime/transfer-productions/" + id + "/basic-scan"
	for _, tc := range []struct {
		token, path, body string
		status            int
	}{
		{"", base, "", 401}, {"system", base, "", 403}, {"unprivileged", base, "", 403}, {"transfer", base, "", 503},
		{"transfer", base + "?tenant_id=9", "", 400}, {"transfer", base, `{"engine_id":9,"created_and_committed":true}`, 400},
		{"transfer", strings.Replace(base, id, "invalid", 1), "", 400},
	} {
		req := httptest.NewRequest("POST", tc.path, nil)
		if tc.body != "" {
			req = httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		}
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != tc.status {
			t.Fatalf("%s status=%d want=%d body=%s", tc.token, response.Code, tc.status, response.Body.String())
		}
	}
	user := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{"Bearer user": {"meta.scan_task.execute"}})
	defer user.Close()
	cfg.SystemServiceURL = user.URL
	router = SetupRouter(cfg, nil, engines, service.NewScanService(nil, engines), nil, nil, nil, nil, modulelifecycle.NewStandalone("meta"))
	req := httptest.NewRequest(http.MethodPost, base, nil)
	req.Header.Set("Authorization", "Bearer user")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != 403 {
		t.Fatal("User entered completed-production scan adapter", response.Code)
	}
}
