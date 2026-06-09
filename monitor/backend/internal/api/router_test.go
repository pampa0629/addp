package api

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSetupRouterRegistersExecutionTreeRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := SetupRouter(nil, nil, nil, "", nil)
	for _, route := range router.Routes() {
		if route.Method == "GET" && route.Path == "/api/v1/monitor/executions/:id/tree" {
			return
		}
	}
	t.Fatal("execution tree route is not registered")
}

func TestSetupRouterRegistersExecutionIDRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := SetupRouter(nil, nil, nil, "", nil)
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

	router := SetupRouter(nil, nil, nil, "", nil)
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
