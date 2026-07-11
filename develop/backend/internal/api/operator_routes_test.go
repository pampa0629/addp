package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOperatorRoutesUseWorkflowEngineInstancePathOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	publicAPI := router.Group("/api/v1/develop")
	publicAPI.GET("/workflow-engines/:id/operators", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	assertRouteStatus(t, router, http.MethodGet, "/api/v1/develop/workflow-engines/12/operators", http.StatusOK)

	for _, path := range []string{
		"/api/v1/develop/operators",
		"/api/v1/develop/operators/buffer",
		"/api/v1/develop/operators/cache/info",
		"/api/v1/develop/operators/modules/geopython_workflow",
		"/api/v1/develop/operators/engine-types/geopython_workflow",
	} {
		assertRouteStatus(t, router, http.MethodGet, path, http.StatusNotFound)
	}
	assertRouteStatus(t, router, http.MethodPost, "/api/v1/develop/operators/refresh", http.StatusNotFound)
}

func assertRouteStatus(t *testing.T, router *gin.Engine, method string, path string, want int) {
	t.Helper()

	req := httptest.NewRequest(method, path, nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != want {
		t.Fatalf("%s status = %d, want %d", path, resp.Code, want)
	}
}
