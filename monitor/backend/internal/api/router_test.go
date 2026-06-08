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
