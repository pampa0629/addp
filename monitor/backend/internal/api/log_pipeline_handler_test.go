package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/monitor/internal/service"
)

func TestLogObservationRequiresPlatformObserverServiceToken(t *testing.T) {
	cases := []struct {
		name, context, principal, client, token string
		permission                              bool
		want                                    int
	}{
		{"tenant", "tenant", "user", "addp-web", "first_party_access_token", true, 403},
		{"platform user cannot observe", "platform", "user", "addp-web", "first_party_access_token", true, 403},
		{"other service cannot observe", "platform", "service_principal", "addp-monitor", "service_access_token", true, 403},
		{"missing permission", "platform", "service_principal", "addp-log-observer", "service_access_token", false, 403},
		{"wrong token type", "platform", "service_principal", "addp-log-observer", "oauth_access_token", true, 403},
		{"observer validates payload", "platform", "service_principal", "addp-log-observer", "service_access_token", true, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			identity := monitorTenantAuthContext()
			identity.Principal.Type = tc.principal
			identity.Client.ClientID = &tc.client
			identity.Token.Type = tc.token
			if tc.principal == "service_principal" {
				identity.Authentication.Methods = []string{"service_secret"}
				identity.Authentication.AssuranceLevel = "not_applicable"
				identity.Client.ScopeMode = "restricted"
				identity.Client.Scopes = []string{"addp.api"}
			}
			if tc.context == "platform" {
				identity.Context = authorization.AuthSessionContext{Type: "platform"}
				if tc.principal == "user" {
					identity.Authentication.AssuranceLevel = "aal2"
				}
				identity.Authorization.RoleAssignments[0].RoleKey = "platform.log_observer_runtime"
				if tc.principal == "user" {
					identity.Authorization.RoleAssignments[0].RoleKey = "platform.system_administrator"
				}
				identity.Authorization.RoleAssignments[0].Scope = authorization.AssignmentScope{Type: "platform"}
			}
			if tc.permission {
				identity.Authorization.RoleAssignments[0].Permissions = []string{"monitor.log_observation.create"}
			} else {
				identity.Authorization.RoleAssignments[0].Permissions = []string{"system.runtime_registry.read"}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(identity)
			}))
			defer server.Close()
			pipeline := service.NewLogPipelineService(nil, "node", nil, nil)
			notifications := service.NewPlatformLogNotifications(nil, nil, false, nil, nil, 3, 0, 0, 0)
			router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, server.URL, nil, nil, modulelifecycle.NewStandalone("monitor"), pipeline, notifications)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/monitor/platform/log-observations", bytes.NewBufferString(`{}`))
			req.Header.Set("Authorization", "Bearer addp_at_fixture")
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.want, response.Body.String())
			}
		})
	}
}

func TestPlatformLogDestinationsRejectEscalatedSubscriptions(t *testing.T) {
	identity := monitorTenantAuthContext()
	identity.Context = authorization.AuthSessionContext{Type: "platform"}
	identity.Authentication.AssuranceLevel = "aal2"
	identity.Authorization.RoleAssignments[0].RoleKey = "platform.system_administrator"
	identity.Authorization.RoleAssignments[0].Scope = authorization.AssignmentScope{Type: "platform"}
	identity.Authorization.RoleAssignments[0].Permissions = []string{"monitor.log_notification.update"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(identity)
	}))
	defer server.Close()
	notifications := service.NewPlatformLogNotifications(nil, nil, false, nil, nil, 3, 0, 0, 0)
	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, server.URL, nil, nil, modulelifecycle.NewStandalone("monitor"), service.NewLogPipelineService(nil, "node", nil, nil), notifications)
	for _, tc := range []struct {
		method, path string
		version      uint64
		enabled      bool
	}{
		{http.MethodPost, "/api/v1/monitor/platform/log-notification-destinations", 0, false},
		{http.MethodPut, "/api/v1/monitor/platform/log-notification-destinations/1", 1, true},
	} {
		payload, err := json.Marshal(service.LogDestinationInput{Version: tc.version, Name: "platform fixture", Channel: "wecom", EventTypes: []string{"opened", "escalated", "resolved"}, Enabled: tc.enabled})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer addp_at_fixture")
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		var body map[string]any
		if response.Code != http.StatusBadRequest || json.Unmarshal(response.Body.Bytes(), &body) != nil || body["error"] == nil || body["error_code"] != "platform_log_invalid" {
			t.Fatalf("%s accepted unsupported subscriptions: status=%d body=%s", tc.method, response.Code, response.Body.String())
		}
	}
}
