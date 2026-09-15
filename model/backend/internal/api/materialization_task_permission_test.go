package api

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
)

func TestMaterializationTaskProviderCallerBoundary(t *testing.T) {
	permissions := []string{"model.task_provider.read", "model.task_provider.execute", "model.materialization.execute"}
	sort.Strings(permissions)
	serviceAuth := authtest.NewTenantServiceAuthContextServer(t, "7", map[string]authtest.TenantServiceIdentity{
		"Bearer orchestrator":  {ClientID: "addp-orchestrator", Permissions: permissions},
		"Bearer other":         {ClientID: "addp-develop", Permissions: permissions},
		"Bearer no-permission": {ClientID: "addp-orchestrator", Permissions: []string{"model.entity.read"}},
	})
	defer serviceAuth.Close()
	userAuth := authtest.NewTenantUserAuthContextServer(t, "7", map[string][]string{"Bearer user": permissions})
	defer userAuth.Close()
	for _, identity := range []struct {
		token, url string
		want       int
	}{
		{"orchestrator", serviceAuth.URL, http.StatusBadRequest},
		{"other", serviceAuth.URL, http.StatusForbidden},
		{"no-permission", serviceAuth.URL, http.StatusForbidden},
		{"user", userAuth.URL, http.StatusForbidden},
	} {
		router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, identity.url, nil, modulelifecycle.NewStandalone("model"))
		for _, route := range []struct{ method, path string }{
			{http.MethodGet, "/api/v1/model/task-provider/tasks?page=0"},
			{http.MethodGet, "/api/v1/model/task-provider/tasks/wrong/1"},
			{http.MethodPost, "/api/v1/model/task-provider/tasks/logical_table_materialization/1/execute"},
		} {
			req := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer "+identity.token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != identity.want {
				t.Fatalf("%s %s: status=%d body=%s", identity.token, route.path, response.Code, response.Body.String())
			}
		}
		if identity.token == "orchestrator" {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/model/logical-tables/invalid/materialized-target", nil)
			req.Header.Set("Authorization", "Bearer orchestrator")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != http.StatusForbidden {
				t.Fatalf("service caller reached manual endpoint: %d", response.Code)
			}
		}
	}
}
