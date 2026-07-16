package api

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSetupRouterRegistersExecutionTreeRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, "", nil, nil)
	for _, route := range router.Routes() {
		if route.Method == "GET" && route.Path == "/api/v1/monitor/executions/:id/tree" {
			return
		}
	}
	t.Fatal("execution tree route is not registered")
}

func TestSetupRouterRegistersExecutionIDRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, "", nil, nil)
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

	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, "", nil, nil)
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

	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, "", nil, nil)
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

	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, "", nil, nil)
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

	router := SetupRouter(nil, nil, nil, nil, nil, nil, nil, "", nil, nil)
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
