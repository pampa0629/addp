package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPQualityRejectsTenantMachineMissingPermissionAndArbitraryQueries(t *testing.T) {
	for _, scope := range []string{"user", "tenant", "machine", "missing"} {
		identity := targetAPIIdentity("user", "addp-web", "first_party_access_token", "monitor.http_observation.read")
		want := 400
		switch scope {
		case "tenant":
			identity = monitorTenantAuthContext()
			want = 403
		case "machine":
			identity = targetAPIIdentity("service_principal", "addp-monitor", "service_access_token", "monitor.http_observation.read")
			want = 403
		case "missing":
			identity = targetAPIIdentity("user", "addp-web", "first_party_access_token", "monitor.resource_observation.read")
			want = 403
		}
		router := resourceAPIRouter(t, identity)
		for _, path := range []string{"http_observations", "http_interfaces", "http_trends"} {
			req := httptest.NewRequest("GET", "/api/v1/monitor/platform/"+path+"?query=up", nil)
			req.Header.Set("Authorization", "Bearer addp_at_user")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != want || (want == 400 && response.Header().Get("Cache-Control") != "no-store") {
				t.Fatal(scope, path, response.Code, response.Body.String())
			}
		}
	}
	router := resourceAPIRouter(t, targetAPIIdentity("user", "addp-web", "first_party_access_token", "monitor.http_observation.read"))
	for _, query := range []string{"instance_ids=1&instance_ids=2", "instance_ids=01", "instance_ids=1&window_seconds=61", "instance_ids=1&query=up", "instance_ids=1&method=GET", "instance_ids=1&route=/private"} {
		req := httptest.NewRequest("GET", "/api/v1/monitor/platform/http_observations?"+query, nil)
		req.Header.Set("Authorization", "Bearer addp_at_user")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != 400 || !strings.Contains(response.Body.String(), "invalid_resource_query") {
			t.Fatal(query, response.Code, response.Body.String())
		}
	}
}
