package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
	commonexecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSetupRouterRegistersExecutionTreeRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, "http://system.invalid", nil, nil)
	for _, route := range router.Routes() {
		if route.Method == "GET" && route.Path == "/api/v1/monitor/executions/:id/tree" {
			return
		}
	}
	t.Fatal("execution tree route is not registered")
}

func TestSetupRouterRegistersExecutionIDRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, "http://system.invalid", nil, nil)
	routes := map[string]bool{}
	for _, route := range router.Routes() {
		if route.Method == "GET" {
			routes[route.Path] = true
		}
	}
	if !routes["/api/v1/monitor/executions/by-execution-id/:execution_id"] {
		t.Fatal("execution_id detail route is not registered")
	}
	if !routes["/api/v1/monitor/executions/by-execution-id/:execution_id/tree"] {
		t.Fatal("execution_id tree route is not registered")
	}
}

func TestSetupRouterRegistersProviderHealthRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, "http://system.invalid", nil, nil)
	routes := map[string]bool{}
	for _, route := range router.Routes() {
		if route.Method == "GET" {
			routes[route.Path] = true
		}
	}
	if !routes["/api/v1/monitor/providers/health"] {
		t.Fatal("provider health collection route is not registered")
	}
	if !routes["/api/v1/monitor/providers/:module/health"] {
		t.Fatal("provider health detail route is not registered")
	}
}

func TestSetupRouterRegistersWebhookRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, "http://system.invalid", nil, nil)
	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"GET /api/v1/monitor/webhook-destinations",
		"POST /api/v1/monitor/webhook-destinations",
		"PATCH /api/v1/monitor/webhook-destinations/:id",
		"POST /api/v1/monitor/webhook-destinations/:id/test",
		"DELETE /api/v1/monitor/webhook-destinations/:id",
		"GET /api/v1/monitor/webhook-deliveries",
		"POST /api/v1/monitor/webhook-deliveries/:delivery_id/retry",
	} {
		if !routes[route] {
			t.Fatalf("webhook route %s is not registered", route)
		}
	}
}

func TestSetupRouterRegistersEmailRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, "http://system.invalid", nil, nil)
	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"GET /api/v1/monitor/email-destinations",
		"POST /api/v1/monitor/email-destinations",
		"PATCH /api/v1/monitor/email-destinations/:id",
		"POST /api/v1/monitor/email-destinations/:id/test",
		"DELETE /api/v1/monitor/email-destinations/:id",
		"GET /api/v1/monitor/email-deliveries",
		"POST /api/v1/monitor/email-deliveries/:delivery_id/retry",
	} {
		if !routes[route] {
			t.Fatalf("email route %s is not registered", route)
		}
	}
}

func TestSetupRouterRegistersAlertRuleRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, "http://system.invalid", nil, nil)
	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"GET /api/v1/monitor/alert-rule-targets",
		"GET /api/v1/monitor/alert-rules",
		"POST /api/v1/monitor/alert-rules",
		"PATCH /api/v1/monitor/alert-rules/:id",
		"DELETE /api/v1/monitor/alert-rules/:id",
	} {
		if !routes[route] {
			t.Fatalf("alert rule route %s is not registered", route)
		}
	}
}

func TestListExecutionsUsesCanonicalTenantAuthContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(monitorTenantAuthContext()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer systemServer.Close()

	db, err := gorm.Open(sqlite.Open("file:monitor-api-auth-context?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
	}

	repository := commonexecution.NewTaskExecutionRepository(db)
	queryService := service.NewExecutionQueryService(repository)
	router := SetupRouter(queryService, nil, nil, nil, nil, nil, nil, nil, nil, systemServer.URL, nil, nil)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/monitor/executions?page_size=1", nil)
	request.Header.Set("Authorization", "Bearer addp_at_monitor")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
}

func monitorTenantAuthContext() commonauth.AuthContext {
	tenantID := "7"
	membershipID := "9"
	clientID := "addp-web"
	issuedAt := time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC)
	return commonauth.AuthContext{
		SchemaVersion: commonauth.AuthContextSchemaVersion,
		Principal:     commonauth.AuthPrincipal{Type: "user", ID: "12"},
		Context: commonauth.AuthSessionContext{
			Type:               "tenant",
			TenantID:           &tenantID,
			TenantMembershipID: &membershipID,
		},
		Authentication: commonauth.AuthenticationFacts{
			Methods:         []string{"password"},
			AssuranceLevel:  "aal1",
			AuthenticatedAt: issuedAt.Add(-time.Minute),
		},
		Client: commonauth.ClientConstraints{
			ClientID:  &clientID,
			Audiences: []string{"addp.api"},
			ScopeMode: "unrestricted",
			Scopes:    []string{},
		},
		Organization: commonauth.OrganizationContext{
			Departments:   []commonauth.DepartmentMembership{},
			ProjectGroups: []commonauth.ProjectGroupMembership{},
		},
		Authorization: commonauth.AuthorizationFacts{
			AuthorizationVersion: "3",
			RoleAssignments: []commonauth.RoleAssignment{{
				AssignmentID: "21",
				RoleKey:      "tenant.monitoring_operator",
				Scope:        commonauth.AssignmentScope{Type: "tenant", TenantID: &tenantID},
				Permissions:  []string{"monitor.execution.read"},
				SourceType:   "manual",
				ValidFrom:    issuedAt.Add(-time.Hour),
			}},
		},
		Token: commonauth.TokenFacts{
			Type:      "first_party_access_token",
			IssuedAt:  issuedAt,
			ExpiresAt: issuedAt.Add(15 * time.Minute),
		},
	}
}
