package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/monitor/internal/service"
)

func TestProcessAlertRulePermissionsAndStrictInput(t *testing.T) {
	read := "monitor.platform_resource_rule.read"
	for _, tc := range []struct {
		name, method, path, body, context, principal string
		keys                                         []string
		delegated                                    bool
		want                                         int
	}{
		{name: "statistics read does not grant rules", method: "GET", keys: []string{"monitor.resource_observation.read"}, want: 403},
		{name: "malformed query rejected", method: "GET", path: "?page=%", keys: []string{read}, want: 400},
		{name: "event read does not grant rules", method: "GET", path: "?page=0", keys: []string{"monitor.platform_alert.read"}, want: 403},
		{name: "rule read reaches validation", method: "GET", path: "?page=0", keys: []string{read}, want: 400},
		{name: "unknown query rejected", method: "GET", path: "?endpoint=private", keys: []string{read}, want: 400},
		{name: "duplicate query rejected", method: "GET", path: "?page=1&page=2", keys: []string{read}, want: 400},
		{name: "read alone cannot create", method: "POST", body: "{}", keys: []string{read}, want: 403},
		{name: "create alone lacks read", method: "POST", body: "{}", keys: []string{"monitor.platform_resource_rule.create"}, want: 403},
		{name: "explicit create validates", method: "POST", body: "{}", keys: []string{read, "monitor.platform_resource_rule.create"}, want: 400},
		{name: "unknown body rejected", method: "POST", body: `{"endpoint":"https://private"}`, keys: []string{read, "monitor.platform_resource_rule.create"}, want: 400},
		{name: "explicit update validates", method: "PUT", path: "/not-a-uuid", body: "{}", keys: []string{read, "monitor.platform_resource_rule.update"}, want: 400},
		{name: "create cannot update", method: "PUT", path: "/not-a-uuid", body: "{}", keys: []string{read, "monitor.platform_resource_rule.create"}, want: 403},
		{name: "explicit delete validates", method: "DELETE", path: "/not-a-uuid", body: `{"version":1}`, keys: []string{read, "monitor.platform_resource_rule.delete"}, want: 400},
		{name: "delete does not accept suppression", method: "DELETE", path: "/not-a-uuid", body: `{"version":1,"suppressed_until":null}`, keys: []string{read, "monitor.platform_resource_rule.delete"}, want: 400},
		{name: "read cannot delete", method: "DELETE", path: "/not-a-uuid", body: `{"version":1}`, keys: []string{read}, want: 403},
		{name: "tenant rejected", method: "GET", path: "?page=0", context: "tenant", keys: []string{read}, want: 403},
		{name: "service rejected", method: "GET", path: "?page=0", principal: "service_principal", keys: []string{read}, want: 403},
		{name: "delegated rejected", method: "GET", path: "?page=0", delegated: true, keys: []string{read}, want: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity := monitorTenantAuthContext()
			sort.Strings(tc.keys)
			identity.Authorization.RoleAssignments[0].Permissions = tc.keys
			if tc.context != "tenant" {
				identity.Context = authorization.AuthSessionContext{Type: "platform"}
				identity.Authentication.AssuranceLevel = "aal2"
				identity.Authorization.RoleAssignments[0].RoleKey = "platform.system_administrator"
				identity.Authorization.RoleAssignments[0].Scope = authorization.AssignmentScope{Type: "platform"}
			}
			if tc.principal != "" {
				identity.Principal.Type = tc.principal
				identity.Authentication.Methods = []string{"service_secret"}
				identity.Authentication.AssuranceLevel = "not_applicable"
				identity.Token.Type = "service_access_token"
				identity.Client.ScopeMode = "restricted"
				identity.Client.Scopes = []string{"addp.api"}
			}
			if tc.delegated {
				identity.Client.ScopeMode = "restricted"
				identity.Client.Scopes = []string{"metrics.discover"}
				identity.Client.Audiences = []string{"monitor"}
				identity.Token.Type = "delegated_access_token"
				identity.Delegation = &authorization.DelegationFacts{DelegatedByClientID: "addp-web", AgentRunID: "fixture", ToolCallID: "fixture"}
			}
			if e := authorization.ValidateAuthContext(identity); e != nil {
				t.Fatal(e)
			}
			owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(identity)
			}))
			defer owner.Close()
			rules := service.NewProcessAlertRuleService(nil, nil, nil, nil)
			router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, owner.URL, nil, nil, modulelifecycle.NewStandalone("monitor"), nil, nil, nil, nil, nil, nil, nil, nil, rules)
			var body *bytes.Buffer
			if tc.body != "" {
				body = bytes.NewBufferString(tc.body)
			}
			req := httptest.NewRequest(tc.method, "/api/v1/monitor/platform/process-resource-alert-rules"+tc.path, nil)
			if body != nil {
				req = httptest.NewRequest(tc.method, "/api/v1/monitor/platform/process-resource-alert-rules"+tc.path, body)
			}
			req.Header.Set("Authorization", "Bearer addp_at_fixture")
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Fatalf("status %d want %d: %s", response.Code, tc.want, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" && response.Code != 403 {
				t.Fatal("rule response cached")
			}
		})
	}
}
