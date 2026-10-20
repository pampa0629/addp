package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEngineResourceRoutesRejectUnboundedInputAndUnauthorizedIdentities(t *testing.T) {
	for _, path := range []string{"engine_resource_observations", "engine_resource_summaries", "engine_resource_trends"} {
		for _, kind := range []string{"service_principal", "user"} {
			identity := targetAPIIdentity(kind, "addp-web", "first_party_access_token", "monitor.resource_observation.read")
			router := resourceAPIRouter(t, identity)
			for _, query := range []string{"", "deployment_id=x&metrics=up&private=sentinel", "deployment_ids=x&deployment_ids=y"} {
				req := httptest.NewRequest("GET", "/api/v1/monitor/platform/"+path+"?"+query, nil)
				req.Header.Set("Authorization", "Bearer addp_at_human")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				want := 400
				if kind == "service_principal" {
					want = 403
				}
				if w.Code != want || strings.Contains(w.Body.String(), "sentinel") || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("%s %s: %d %s", kind, path, w.Code, w.Body.String())
				}
			}
		}
	}
}
