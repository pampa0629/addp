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

func TestTracePermissionIdentityAndBoundedQuery(t *testing.T) {
	read := "monitor.trace.read"
	for _, tc := range []struct {
		name, method, path, body, context, principal string
		keys                                         []string
		delegated                                    bool
		want                                         int
	}{
		{name: "module read does not grant traces", method: "GET", keys: []string{"platform.module.read"}, want: 403},
		{name: "valid trace read reaches unavailable backend", method: "GET", path: "?instance_id=1", keys: []string{read}, want: 503},
		{name: "unknown query rejected", method: "GET", path: "?instance_id=1&query=private", keys: []string{read}, want: 400},
		{name: "duplicate query rejected", method: "GET", path: "?instance_id=1&instance_id=2", keys: []string{read}, want: 400},
		{name: "noncanonical instance rejected", method: "GET", path: "?instance_id=01", keys: []string{read}, want: 400},
		{name: "detail ID rejected", method: "GET", path: "/private?instance_id=1", keys: []string{read}, want: 400},
		{name: "unbounded range rejected", method: "GET", path: "?instance_id=1&start=2026-10-19T00:00:00Z&end=2026-10-19T02:00:00Z", keys: []string{read}, want: 400},
		{name: "body rejected", method: "GET", path: "?instance_id=1", body: "{}", keys: []string{read}, want: 400},
		{name: "tenant rejected", method: "GET", path: "?instance_id=1", context: "tenant", keys: []string{read}, want: 403},
		{name: "service rejected", method: "GET", path: "?instance_id=1", principal: "service_principal", keys: []string{read}, want: 403},
		{name: "delegated rejected", method: "GET", path: "?instance_id=1", delegated: true, keys: []string{read}, want: 403},
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
			traces := service.NewTraceService(nil, nil)
			router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, owner.URL, nil, nil, modulelifecycle.NewStandalone("monitor"), nil, nil, nil, nil, nil, nil, nil, traces, nil, nil)
			var body *bytes.Buffer
			if tc.body != "" {
				body = bytes.NewBufferString(tc.body)
			}
			req := httptest.NewRequest(tc.method, "/api/v1/monitor/platform/traces"+tc.path, nil)
			if body != nil {
				req = httptest.NewRequest(tc.method, "/api/v1/monitor/platform/traces"+tc.path, body)
			}
			req.Header.Set("Authorization", "Bearer addp_at_fixture")
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Fatalf("status %d want %d: %s", response.Code, tc.want, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" && response.Code != 403 {
				t.Fatal("trace response cached")
			}
		})
	}
}
