package api

import (
	"github.com/addp/orchestrator/internal/repository"
	"github.com/addp/orchestrator/internal/service"
	"github.com/gin-gonic/gin"
)

// SetupRouter 设置路由
func SetupRouter(
	orchRepo *repository.OrchestrationRepository,
	execRepo *repository.ExecutionRepository,
	executor *service.Executor,
	scheduler *service.Scheduler,
) *gin.Engine {
	router := gin.Default()

	// CORS 中间件
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	handler := NewOrchestrationHandler(orchRepo, execRepo, executor, scheduler)

	api := router.Group("/api")
	{
		// 编排管理
		api.POST("/orchestrations", handler.Create)
		api.GET("/orchestrations", handler.List)
		api.GET("/orchestrations/:id", handler.Get)
		api.PUT("/orchestrations/:id", handler.Update)
		api.DELETE("/orchestrations/:id", handler.Delete)

		// 执行管理
		api.POST("/orchestrations/:id/execute", handler.Execute)
		api.GET("/orchestrations/:id/executions", handler.ListExecutions)
		api.GET("/orch-executions/:id", handler.GetExecution)
	}

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	return router
}
