package api

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestExecutionRoutesUseExecutionIDWildcard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	executions := router.Group("/api/v1/transfer/executions")

	executions.GET("/:execution_id", func(c *gin.Context) {})
	executions.POST("/:execution_id/cancel", func(c *gin.Context) {})
	executions.POST("/:execution_id/retry", func(c *gin.Context) {})
	executions.GET("/:execution_id/progress", func(c *gin.Context) {})
	executions.GET("/:execution_id/logs", func(c *gin.Context) {})
}
