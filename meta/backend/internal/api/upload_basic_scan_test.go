package api

import (
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/service"
	"github.com/google/uuid"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadIdentificationSeparatesManagerMutationAndSystemRead(t *testing.T) {
	identity := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer manager":       {ClientID: "addp-manager", Permissions: []string{"meta.scan_task.execute"}},
		"Bearer system":        {ClientID: "addp-system", Permissions: []string{"meta.task_provider.read"}},
		"Bearer wrong_manager": {ClientID: "addp-manager", Permissions: []string{"meta.task_provider.read"}},
		"Bearer wrong_system":  {ClientID: "addp-system", Permissions: []string{"meta.scan_task.execute"}},
	})
	defer identity.Close()
	cfg := &config.Config{}
	cfg.SystemServiceURL = identity.URL
	engines := service.NewEngineService(nil, nil)
	router := SetupRouter(cfg, nil, engines, service.NewScanService(nil, engines), nil, nil, nil, nil, modulelifecycle.NewStandalone("meta"))
	id := uuid.NewString()
	path := "/api/v1/meta/runtime/manager-uploads/" + id + "/basic-scan"
	for _, c := range []struct {
		method, token, url, body string
		want                     int
	}{
		{"POST", "manager", path, "", 503}, {"GET", "system", path, "", 503},
		{"POST", "wrong_system", path, "", 403}, {"GET", "wrong_manager", path, "", 403},
		{"POST", "system", path, "", 403}, {"GET", "manager", path, "", 403},
		{"POST", "manager", path + "?engine_id=9", "", 400}, {"POST", "manager", path, `{"created":true}`, 400},
		{"GET", "system", strings.Replace(path, id, "bad", 1), "", 400},
	} {
		request := httptest.NewRequest(c.method, c.url, strings.NewReader(c.body))
		request.Header.Set("Authorization", "Bearer "+c.token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, request)
		if w.Code != c.want {
			t.Fatalf("%s %s status=%d want=%d body=%s", c.method, c.token, w.Code, c.want, w.Body.String())
		}
	}
}
