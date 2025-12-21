package api

import (
	"net/http"
	"time"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/orchestrator/internal/repository"
	"github.com/addp/orchestrator/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// SetupRouter 设置路由
func SetupRouter(
	orchRepo *repository.OrchestrationRepository,
	execRepo *repository.ExecutionRepository,
	executor *service.Executor,
	scheduler *service.Scheduler,
	moduleClient *service.ModuleClient,
	engineRegistry *service.EngineRegistry,
	taskProviderRegistry *service.TaskProviderRegistry,
	systemURL string,
	redisClient *redis.Client,
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

	// 创建 HTTP 客户端
	httpClient := &http.Client{Timeout: 30 * time.Second}

	handler := NewOrchestrationHandler(orchRepo, execRepo, executor, scheduler, moduleClient, engineRegistry, taskProviderRegistry, httpClient)

	api := router.Group("/api")

	// 使用 Redis 缓存中间件 (TTL: 5分钟, 减少 System 调用 90%)
	if redisClient != nil {
		api.Use(commonAuth.CachedSystemAuthMiddleware(systemURL, redisClient, 5*time.Minute))
	} else {
		// Fallback: 无缓存模式
		api.Use(commonAuth.SystemAuthMiddleware(systemURL))
	}

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
		api.GET("/executions", handler.ListAllExecutions)
		api.GET("/orch-executions/:id", handler.GetExecution)

		// 计算资源管理（动态从 System 获取）
		api.GET("/compute-resources", handler.ListComputeResources)

		// 模块任务列表 (用于拖拽复用，动态调用)
		api.GET("/tasks/list", handler.ListModuleTasks)
	}

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	return router
}
