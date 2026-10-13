package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/gin-gonic/gin"
)

func TestResourceOverviewIdentityAndStrictNoInput(t *testing.T) {
	for _, variant := range []string{"tenant", "machine", "delegated", "missing", "anonymous", "user", "oauth"} {
		identity := targetAPIIdentity("user", "addp-web", "first_party_access_token", "monitor.resource_observation.read")
		want := 503
		switch variant {
		case "tenant":
			identity = monitorTenantAuthContext()
			want = 403
		case "machine":
			identity = targetAPIIdentity("service_principal", "addp-prometheus", "service_access_token", "monitor.resource_observation.read")
			want = 403
		case "delegated":
			identity.Token.Type = "delegated_access_token"
			identity.Client.ScopeMode = "restricted"
			identity.Client.Scopes = []string{"metrics.read"}
			identity.Client.Audiences = []string{"monitor"}
			identity.Delegation = &authorization.DelegationFacts{DelegatedByClientID: "addp-web", AgentRunID: "run", ToolCallID: "call"}
			want = 403
		case "missing":
			identity.Authorization.RoleAssignments[0].Permissions = []string{"monitor.monitoring_target.read"}
			want = 403
		case "anonymous":
			want = 401
		case "oauth":
			identity.Token.Type = "oauth_access_token"
			identity.Client.ScopeMode = "restricted"
			identity.Client.Scopes = []string{"addp.api"}
		}
		router := resourceAPIRouter(t, identity)
		req := httptest.NewRequest("GET", "/api/v1/monitor/platform/resource_overview", nil)
		if variant != "anonymous" {
			req.Header.Set("Authorization", "Bearer addp_at_resource_user")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != want || ((variant == "user" || variant == "oauth") && response.Header().Get("Cache-Control") != "no-store") {
			t.Fatal(variant, response.Code, response.Body.String())
		}
	}
	router := gin.New()
	router.GET("/overview", NewResourceObservationHandler(nil).Overview)
	for _, query := range []string{"?page=1", "?node_ids=private", "?metrics=node.cpu.busy_percent", "?refresh=15", "?x=1&x=2"} {
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest("GET", "/overview"+query, nil))
		if res.Code != 400 {
			t.Fatal(query, res.Code)
		}
	}
	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest("GET", "/overview", strings.NewReader("{}")))
	if res.Code != 400 {
		t.Fatal(res.Code)
	}
}
