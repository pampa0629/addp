package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	monitorModels "github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
)

type apiResourcePolicies struct {
	row monitorModels.ResourceQueryPolicy
}

func (s *apiResourcePolicies) Get(context.Context) (monitorModels.ResourceQueryPolicy, error) {
	return s.row, nil
}
func (s *apiResourcePolicies) Save(_ context.Context, v monitorModels.ResourceQueryPolicy, version uint64) (monitorModels.ResourceQueryPolicy, error) {
	if version != s.row.Version {
		return v, resourcequery.ErrConflict
	}
	v.Version = version + 1
	s.row = v
	return v, nil
}
func resourceAPIRouter(t *testing.T, identity authorization.AuthContext) *gin.Engine {
	t.Helper()
	system := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(identity)
	}))
	t.Cleanup(system.Close)
	svc := service.NewResourceObservationService(&apiResourcePolicies{row: monitorModels.ResourceQueryPolicy{Budget: resourcequery.DefaultBudget()}}, nil, nil, nil, false, nil)
	return SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, system.URL, nil, nil, modulelifecycle.NewStandalone("monitor"), nil, nil, nil, nil, svc)
}
func TestResourceRoutesRejectTenantMachineDelegatedAndMissingPermission(t *testing.T) {
	paths := []string{"/platform/resource_observations", "/platform/resource_trends", "/settings/resource-query-policy"}
	for _, variant := range []string{"tenant", "service", "delegated", "missing", "anonymous", "user", "oauth"} {
		for _, path := range paths {
			t.Run(variant+path, func(t *testing.T) {
				permission := "monitor.resource_observation.read"
				if strings.HasPrefix(path, "/settings/") {
					permission = "monitor.configuration.read"
				}
				identity := targetAPIIdentity("user", "addp-web", "first_party_access_token", permission)
				want := 400
				switch variant {
				case "tenant":
					identity = monitorTenantAuthContext()
					want = 403
				case "service":
					identity = targetAPIIdentity("service_principal", "addp-prometheus", "service_access_token", permission)
					want = 403
				case "missing":
					identity.Authorization.RoleAssignments[0].Permissions = []string{"monitor.monitoring_target.read"}
					want = 403
				case "delegated":
					identity.Token.Type = "delegated_access_token"
					identity.Client.ScopeMode = "restricted"
					identity.Client.Scopes = []string{"metrics.read"}
					identity.Client.Audiences = []string{"monitor"}
					identity.Delegation = &authorization.DelegationFacts{DelegatedByClientID: "addp-web", AgentRunID: "run", ToolCallID: "call"}
					want = 403
				case "anonymous":
					want = 401
				case "oauth":
					identity.Token.Type = "oauth_access_token"
					identity.Client.ScopeMode = "restricted"
					identity.Client.Scopes = []string{"addp.api"}
				}
				if (variant == "user" || variant == "oauth") && strings.HasPrefix(path, "/settings/") {
					want = 200
				}
				router := resourceAPIRouter(t, identity)
				req := httptest.NewRequest("GET", "/api/v1/monitor"+path, nil)
				if variant != "anonymous" {
					req.Header.Set("Authorization", "Bearer addp_at_resource_user")
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				if response.Code != want {
					t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body.String())
				}
			})
		}
	}
}
func TestResourceQueryParsingStrictAndPolicyCAS(t *testing.T) {
	identity := targetAPIIdentity("user", "addp-web", "first_party_access_token", "monitor.resource_observation.read")
	router := resourceAPIRouter(t, identity)
	for _, query := range []string{"node_id=x&metrics=y&query=up", "node_id=x&metrics=y&node_id=z", "node_id=x&metrics=y&step=1", "node_id=x&metrics=y&start=bad", "node_id=x;metrics=y"} {
		req := httptest.NewRequest("GET", "/api/v1/monitor/platform/resource_observations?"+query, nil)
		req.Header.Set("Authorization", "Bearer addp_at_user")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_resource_query") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	identity = targetAPIIdentity("user", "addp-web", "first_party_access_token", "monitor.configuration.update")
	router = resourceAPIRouter(t, identity)
	input := service.ResourceQueryPolicyInput{Budget: resourcequery.DefaultBudget()}
	body, e := json.Marshal(input)
	if e != nil {
		t.Fatal(e)
	}
	for _, want := range []int{200, 409} {
		req := httptest.NewRequest("PUT", "/api/v1/monitor/settings/resource-query-policy", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer addp_at_user")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
		if want == 200 && !strings.Contains(w.Body.String(), `"pending_restart":false`) {
			t.Fatal("budget demands restart")
		}
	}
	for _, body := range []string{`{"version":1,"unknown":1}`, `{}`, `null`} {
		req := httptest.NewRequest("PUT", "/api/v1/monitor/settings/resource-query-policy", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer addp_at_user")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func TestResourceQueryPolicyAuditHasNoBodyOrQuerySecrets(t *testing.T) {
	var received models.AuditLogCreateRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/platform/audit/events" {
			t.Error(r.URL.Path)
		}
		if e := json.NewDecoder(r.Body).Decode(&received); e != nil {
			t.Error(e)
		}
		w.WriteHeader(201)
	}))
	defer server.Close()
	identity := targetAPIIdentity("user", "addp-web", "first_party_access_token", "monitor.configuration.update")
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if e := auth.SetAuthContextForGin(c, identity); e != nil {
			t.Fatal(e)
		}
		c.Next()
	}, logPipelineAudit(client.NewSystemServiceClient(server.URL, retryAuditTokens{}, server.Client())))
	router.PUT("/api/v1/monitor/settings/resource-query-policy", func(c *gin.Context) { c.Set("resource_query_policy_audit_version", uint64(2)); c.Status(200) })
	req := httptest.NewRequest("PUT", "/api/v1/monitor/settings/resource-query-policy?private=secret", strings.NewReader(`{"secret":"private"}`))
	router.ServeHTTP(httptest.NewRecorder(), req)
	encoded, e := json.Marshal(received)
	if e != nil {
		t.Fatal(e)
	}
	if received.EventName != "platform.resource_query_policy.update" || received.RiskLevel != "high" || received.Details["saved_version"] != float64(2) || strings.Contains(string(encoded), "secret") {
		t.Fatalf("audit=%s", encoded)
	}
}
