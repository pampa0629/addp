package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
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
		{"collection disabled", "platform", "service_principal", "addp-log-observer", "service_access_token", true, 409},
		{"deployment invalid", "platform", "service_principal", "addp-log-observer", "service_access_token", true, 503},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ADDP_OBSERVABILITY_LOGS_ENABLED", "true")
			if tc.name == "collection disabled" {
				t.Setenv("ADDP_OBSERVABILITY_LOGS_ENABLED", "false")
			}
			if tc.name == "deployment invalid" {
				t.Setenv("ADDP_OBSERVABILITY_LOGS_ENABLED", "invalid")
			}
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
			notifications := service.NewPlatformAlertService(nil, nil, false, nil, nil, 3, 0, 0, 0)
			router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, server.URL, nil, nil, modulelifecycle.NewStandalone("monitor"), pipeline, notifications, nil, nil, nil, nil)
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

func TestPlatformAlertDestinationsRejectEscalatedSubscriptions(t *testing.T) {
	identity := monitorTenantAuthContext()
	identity.Context = authorization.AuthSessionContext{Type: "platform"}
	identity.Authentication.AssuranceLevel = "aal2"
	identity.Authorization.RoleAssignments[0].RoleKey = "platform.system_administrator"
	identity.Authorization.RoleAssignments[0].Scope = authorization.AssignmentScope{Type: "platform"}
	identity.Authorization.RoleAssignments[0].Permissions = []string{"monitor.platform_alert_notification.read", "monitor.platform_alert_notification.update"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(identity)
	}))
	defer server.Close()
	notifications := service.NewPlatformAlertService(nil, nil, false, nil, nil, 3, 0, 0, 0)
	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, server.URL, nil, nil, modulelifecycle.NewStandalone("monitor"), service.NewLogPipelineService(nil, "node", nil, nil), notifications, nil, nil, nil, nil)
	for _, tc := range []struct {
		method, path string
		version      uint64
		enabled      bool
	}{
		{http.MethodPost, "/api/v1/monitor/platform/alert-notification-destinations", 0, false},
		{http.MethodPut, "/api/v1/monitor/platform/alert-notification-destinations/1", 1, true},
	} {
		payload, err := json.Marshal(service.PlatformAlertDestinationInput{Categories: []string{"log_pipeline"}, Version: tc.version, Name: "platform fixture", Channel: "wecom", EventTypes: []string{"opened", "escalated", "resolved"}, Enabled: tc.enabled})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer addp_at_fixture")
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		var body map[string]any
		if response.Code != http.StatusBadRequest || json.Unmarshal(response.Body.Bytes(), &body) != nil || body["error"] == nil || body["error_code"] != "platform_alert_invalid" {
			t.Fatalf("%s accepted unsupported subscriptions: status=%d body=%s", tc.method, response.Code, response.Body.String())
		}
	}
}

func TestPlatformAlertManagementUsesNewExplicitUserPermissions(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, context, principal string
		permissions                            []string
		want                                   int
		delegated                              bool
	}{
		{"old diagnostic read", "GET", "/alerts?page=0", "platform", "user", []string{"monitor.log_pipeline.read"}, 403, false},
		{"old notification read", "GET", "/alert-notification-deliveries?page=0", "platform", "user", []string{"monitor.log_notification.read"}, 403, false},
		{"explicit event read", "GET", "/alerts?page=0", "platform", "user", []string{"monitor.platform_alert.read"}, 400, false},
		{"event update lacks read", "POST", "/alerts/0/acknowledge", "platform", "user", []string{"monitor.platform_alert.update"}, 403, false},
		{"event read lacks update", "POST", "/alerts/0/acknowledge", "platform", "user", []string{"monitor.platform_alert.read"}, 403, false},
		{"explicit event action", "POST", "/alerts/0/acknowledge", "platform", "user", []string{"monitor.platform_alert.read", "monitor.platform_alert.update"}, 400, false},
		{"tenant cannot read platform", "GET", "/alerts?page=0", "tenant", "user", []string{"monitor.platform_alert.read"}, 403, false},
		{"platform service cannot manage", "GET", "/alerts?page=0", "platform", "service_principal", []string{"monitor.platform_alert.read"}, 403, false},
		{"delegated user cannot manage", "GET", "/alerts?page=0", "platform", "user", []string{"monitor.platform_alert.read"}, 403, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity := monitorTenantAuthContext()
			identity.Principal.Type = tc.principal
			identity.Authorization.RoleAssignments[0].Permissions = tc.permissions
			if tc.context == "platform" {
				identity.Context = authorization.AuthSessionContext{Type: "platform"}
				identity.Authentication.AssuranceLevel = "aal2"
				identity.Authorization.RoleAssignments[0].RoleKey = "platform.system_administrator"
				identity.Authorization.RoleAssignments[0].Scope = authorization.AssignmentScope{Type: "platform"}
			}
			if tc.principal == "service_principal" {
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
				identity.Delegation = &authorization.DelegationFacts{DelegatedByClientID: "addp-web", AgentRunID: "run-test", ToolCallID: "call-test"}
			}
			if err := authorization.ValidateAuthContext(identity); err != nil {
				t.Fatal("invalid identity fixture", err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(identity)
			}))
			defer server.Close()
			alerts := service.NewPlatformAlertService(nil, nil, false, nil, nil, 3, 0, 0, 0)
			router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, server.URL, nil, nil, modulelifecycle.NewStandalone("monitor"), nil, alerts, nil, nil, nil, nil)
			req := httptest.NewRequest(tc.method, "/api/v1/monitor/platform"+tc.path, bytes.NewBufferString(`{}`))
			req.Header.Set("Authorization", "Bearer addp_at_fixture")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.want, response.Body.String())
			}
			// Replaced management routes are gone, including for a named administrator.
			req = httptest.NewRequest("GET", "/api/v1/monitor/platform/log-notification-destinations", nil)
			response = httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != 404 {
				t.Fatal("removed notification route remains", response.Code)
			}
		})
	}
}

func TestPlatformAlertUnexpectedFailureUsesOwnDomainAndHidesDetails(t *testing.T) {
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	platformAlertRespond(ctx, nil, errors.New("database detail with private credential"))
	var result map[string]any
	if response.Code != http.StatusInternalServerError || json.Unmarshal(response.Body.Bytes(), &result) != nil || result["error_code"] != "platform_alert_operation_failed" || bytes.Contains(response.Body.Bytes(), []byte("private credential")) {
		t.Fatalf("unexpected alert error response: status=%d body=%s", response.Code, response.Body.String())
	}
}
