package api

import (
	"net/http"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/middleware/audit"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/orchestrator/docs"
	"github.com/addp/orchestrator/internal/repository"
	"github.com/addp/orchestrator/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// SetupRouter 设置路由
func SetupRouter(
	orchRepo *repository.OrchestrationRepository,
	executionService *service.ExecutionService,
	executor *service.Executor,
	scheduler *service.Scheduler,
	taskProviderRegistry *service.TaskProviderRegistry,
	systemURL string,
	redisClient *redis.Client,
	systemClient *commonClient.SystemClient, // 用于审计日志
) *gin.Engine {
	router := gin.Default()

	// Swagger 文档
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

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

	handler := NewOrchestrationHandler(orchRepo, executionService, executor, scheduler, taskProviderRegistry, httpClient)

	api := router.Group("/api/v1/orchestrator")

	// i18n 中间件（解析 Accept-Language 请求头）
	api.Use(commoni18n.I18nMiddleware())

	// 使用 Redis 缓存中间件 (TTL: 5分钟, 减少 System 调用 90%)
	if redisClient != nil {
		api.Use(commonAuth.CachedSystemAuthMiddleware(systemURL, redisClient, 5*time.Minute))
	} else {
		// Fallback: 无缓存模式
		api.Use(commonAuth.SystemAuthMiddleware(systemURL))
	}
	// 审计日志中间件（记录到 System 模块）
	if systemClient != nil {
		api.Use(audit.AuditMiddleware("orchestrator", systemClient))
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
		api.GET("/executions/:execution_id", handler.GetProviderExecution)
		api.GET("/orch-executions/:id", handler.GetExecution)

		// 任务提供者发现（动态从 System 获取）
		api.GET("/task-providers", handler.ListTaskProviders)

		// 模块任务列表 (用于拖拽复用，动态调用)
		api.GET("/tasks", handler.ListModuleTasks)
		api.GET("/tasks/:task_type/:id", handler.GetProviderOrchestrationTask)
		api.POST("/tasks/:task_type/:id/execute", handler.ExecuteProviderOrchestrationTask)
	}

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	return router
}
