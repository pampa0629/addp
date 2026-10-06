package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestPlatformLogRetryRequiresPlatformUserAndStrictInput(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		platform, permission, user bool
		body                       string
		want                       int
	}{
		{"tenant forbidden", false, true, true, `{}`, 403},
		{"read only forbidden", true, false, true, `{}`, 403},
		{"service forbidden", true, true, false, `{}`, 403},
		{"missing count invalid", true, true, true, `{"destination_version":1}`, 400},
		{"destination override invalid", true, true, true, `{"destination_version":1,"expected_manual_retry_count":0,"url":"https://override.invalid"}`, 400},
		{"trailing payload invalid", true, true, true, `{} {}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity := monitorTenantAuthContext()
			if tc.platform {
				identity.Context = authorization.AuthSessionContext{Type: "platform"}
				identity.Authentication.AssuranceLevel = "aal2"
				identity.Authorization.RoleAssignments[0].Scope = authorization.AssignmentScope{Type: "platform"}
				identity.Authorization.RoleAssignments[0].RoleKey = "platform.system_administrator"
			}
			identity.Authorization.RoleAssignments[0].Permissions = []string{"monitor.log_notification.read"}
			if tc.permission {
				identity.Authorization.RoleAssignments[0].Permissions = append(identity.Authorization.RoleAssignments[0].Permissions, "monitor.log_notification.update")
			}
			if !tc.user {
				identity.Principal.Type = "service_principal"
				identity.Token.Type = "service_access_token"
				identity.Authentication.Methods = []string{"service_secret"}
				identity.Authentication.AssuranceLevel = "not_applicable"
				identity.Client.ScopeMode = "restricted"
				identity.Client.Scopes = []string{"addp.api"}
				id := "addp-monitor"
				identity.Client.ClientID = &id
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(identity)
			}))
			defer server.Close()
			pipeline := service.NewLogPipelineService(nil, "node", nil, nil)
			notifications := service.NewPlatformLogNotifications(nil, nil, false, nil, nil, 3, 0, 0, 0)
			router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, server.URL, nil, nil, modulelifecycle.NewStandalone("monitor"), pipeline, notifications, nil, nil)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/monitor/platform/log-notification-deliveries/"+uuid.NewString()+"/retry", bytes.NewBufferString(tc.body))
			request.Header.Set("Authorization", "Bearer addp_at_fixture")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.want, response.Body.String())
			}
		})
	}
}

type retryAuditTokens struct{}

func (retryAuditTokens) Token(context.Context, uint) (string, error)   { return "audit-fixture", nil }
func (retryAuditTokens) PlatformToken(context.Context) (string, error) { return "audit-fixture", nil }

func TestPlatformLogRetryAuditIncludesOnlySafeFacts(t *testing.T) {
	var received models.AuditLogCreateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/platform/audit/events" || r.Header.Get("Authorization") != "Bearer audit-fixture" {
			t.Error("wrong audit request")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	identity := monitorTenantAuthContext()
	identity.Context = authorization.AuthSessionContext{Type: "platform"}
	identity.Authentication.AssuranceLevel = "aal2"
	identity.Authorization.RoleAssignments[0].Scope = authorization.AssignmentScope{Type: "platform"}
	identity.Authorization.RoleAssignments[0].RoleKey = "platform.system_administrator"
	identity.Authorization.RoleAssignments[0].Permissions = []string{"monitor.log_notification.update"}
	deliveryID, eventID := uuid.NewString(), uuid.NewString()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if err := commonAuth.SetAuthContextForGin(c, identity); err != nil {
			t.Fatal(err)
		}
		c.Next()
	}, logPipelineAudit(client.NewSystemServiceClient(server.URL, retryAuditTokens{}, server.Client())))
	router.POST("/api/v1/monitor/platform/log-notification-deliveries/:id/retry", func(c *gin.Context) {
		c.Set(logRetryAuditKey, logRetryAuditFacts{DeliveryID: deliveryID, EventID: eventID, DestinationID: 7, Before: 0, After: 1})
		c.JSON(200, gin.H{"status": "pending"})
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/monitor/platform/log-notification-deliveries/"+deliveryID+"/retry", bytes.NewBufferString(`{"secret":"do-not-audit","payload":"do-not-audit"}`)))
	if response.Code != 200 || received.Result != "succeeded" || received.EntityID != deliveryID || received.EntityType != "platform_log_delivery" || received.Details["source_principal_id"] != identity.Principal.ID || received.Details["event_id"] != eventID || received.Details["manual_retry_count_before"] != float64(0) || received.Details["manual_retry_count_after"] != float64(1) || len(received.Details) != 8 {
		t.Fatalf("unexpected audit metadata %#v", received)
	}
	for _, field := range []string{"secret", "payload", "url", "recipients"} {
		if _, exists := received.Details[field]; exists {
			t.Fatalf("audit exposes %s", field)
		}
	}
}
