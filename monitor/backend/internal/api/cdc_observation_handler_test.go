package api

import (
	"github.com/addp/common/authorization"
	"net/http/httptest"
	"testing"
)

func TestCDCRoutesEnforceCurrentPlatformUserAndStrictQuery(t *testing.T) {
	for _, variant := range []string{"user", "missing", "service", "tenant", "delegated"} {
		identity := targetAPIIdentity("user", "addp-web", "first_party_access_token", "monitor.resource_observation.read")
		want := 400
		switch variant {
		case "missing":
			identity.Authorization.RoleAssignments[0].Permissions = []string{"monitor.monitoring_target.read"}
			want = 403
		case "service":
			identity = targetAPIIdentity("service_principal", "addp-monitor", "service_access_token", "monitor.resource_observation.read")
			want = 403
		case "tenant":
			identity = monitorTenantAuthContext()
			want = 403
		case "delegated":
			identity.Token.Type = "delegated_access_token"
			identity.Client.ScopeMode = "restricted"
			identity.Client.Scopes = []string{"metrics.read"}
			identity.Client.Audiences = []string{"monitor"}
			identity.Delegation = &authorization.DelegationFacts{DelegatedByClientID: "addp-web", AgentRunID: "run", ToolCallID: "call"}
			want = 403
		}
		router := resourceAPIRouter(t, identity)
		for _, path := range []string{"cdc_resource_observations?address=localhost", "cdc_resource_trends?component_id=bad&metrics=up", "cdc_resource_trends?component_id=x&metrics=x&start=2026-11-11T00:00:00Z&end=2026-11-11T00:05:00Z&metrics=y"} {
			req := httptest.NewRequest("GET", "/api/v1/monitor/platform/"+path, nil)
			req.Header.Set("Authorization", "Bearer addp_at_user")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != want || (want == 400 && res.Header().Get("Cache-Control") != "no-store") {
				t.Fatalf("%s %s: %d %s", variant, path, res.Code, res.Body.String())
			}
		}
	}
}
