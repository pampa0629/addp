package api

import (
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/middleware/audit"
	commonAuth "github.com/addp/common/middleware/auth"
	commonCors "github.com/addp/common/middleware/cors"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/middleware/logging"
	"github.com/addp/common/middleware/requestid"
	_ "github.com/addp/transfer/docs"
	_ "github.com/addp/transfer/i18n"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// extractAuthToken 从请求中提取认证 token
func extractAuthToken(c *gin.Context) string {
	// 优先从 context 中获取（由认证中间件设置）
	if token := c.GetString("jwt_token"); token != "" {
		return token
	}

	// 从 Authorization header 提取
	if header := c.GetHeader("Authorization"); header != "" {
		if len(header) > 7 && header[:7] == "Bearer " {
			return header[7:]
		}
	}

	return ""
}

// SetupRouter 设置路由
func SetupRouter(
	taskService *service.TaskService,
	executionService *service.ExecutionService,
	objectStorageService *service.ObjectStorageService,
	systemURL string,
	metaURL string,
	redisClient *redis.Client,
	systemClient *commonClient.SystemClient,
) *gin.Engine {
	router := gin.Default()

	// Swagger 文档
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 全局中间件
	router.Use(commonCors.CORS())
	router.Use(i18nmiddleware.I18nMiddleware()) // 国际化中间件
	router.Use(requestid.RequestIDMiddleware()) // Request ID 追踪
	router.Use(logging.LoggingMiddleware())     // 结构化日志记录

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "ok",
			"service": "transfer",
		})
	})

	// API 路由组
	api := router.Group("/api/v1/transfer")

	// 公开接口（无需认证）
	public := api.Group("")
	{
		public.GET("/ping", ping)
	}

	// 受保护接口（需要 JWT 认证）
	protected := api.Group("")
	// 使用 Redis 缓存中间件 (TTL: 5分钟, 减少 System 调用 90%)
	if redisClient != nil {
		protected.Use(commonAuth.CachedSystemAuthMiddleware(systemURL, redisClient, 5*time.Minute))
	} else {
		// Fallback: 无缓存模式
		protected.Use(commonAuth.SystemAuthMiddleware(systemURL))
	}
	// 审计日志中间件（记录到 System 模块）
	if systemClient != nil {
		protected.Use(audit.AuditMiddleware("transfer", systemClient))
	}

	// 创建 Handlers
	taskHandler := NewTaskHandler(taskService)
	executionHandler := NewExecutionHandler(executionService)
	objectStorageHandler := NewObjectStorageHandler(objectStorageService)
	systemEngineHandler := NewSystemEngineHandler(systemClient)
	capabilityHandler := NewTransferCapabilityHandler()
	// DataSourceHandler 需要在请求处理时创建（因为需要 JWT token）
	// 这里先不初始化，在路由中动态创建

	// 数据源路由（为前端提供统一的数据源访问接口）
	// 注意：DataSourceHandler 需要用户的 JWT token，所以在路由中动态创建
	protected.GET("/engines", func(c *gin.Context) {
		authToken := extractAuthToken(c)
		dataSourceHandler := NewDataSourceHandler(systemClient, commonClient.NewMetaClient(metaURL, authToken))
		dataSourceHandler.GetEngines(c)
	})
	protected.GET("/engines/:engine_id/tree", func(c *gin.Context) {
		authToken := extractAuthToken(c)
		dataSourceHandler := NewDataSourceHandler(systemClient, commonClient.NewMetaClient(metaURL, authToken))
		dataSourceHandler.GetEngineTree(c)
	})
	protected.GET("/nodes/:node_id/children", func(c *gin.Context) {
		authToken := extractAuthToken(c)
		dataSourceHandler := NewDataSourceHandler(systemClient, commonClient.NewMetaClient(metaURL, authToken))
		dataSourceHandler.GetNodeChildren(c)
	})
	protected.GET("/tables/metadata", func(c *gin.Context) {
		authToken := extractAuthToken(c)
		dataSourceHandler := NewDataSourceHandler(systemClient, commonClient.NewMetaClient(metaURL, authToken))
		dataSourceHandler.DetectTableMetadata(c)
	})
	protected.GET("/system-engines", systemEngineHandler.List)
	protected.GET("/capabilities", capabilityHandler.Get)

	// 任务管理路由
	tasks := protected.Group("/tasks")
	{
		tasks.GET("", taskHandler.ListTasks)                      // TaskProvider 列表和任务列表
		tasks.GET("/:task_type/:id", taskHandler.ProviderGetTask) // TaskProvider 获取任务详情
		tasks.POST("/:task_type/:id/execute", taskHandler.ProviderExecuteTask)
	}

	taskDefinitions := protected.Group("/task-definitions")
	{
		taskDefinitions.POST("", taskHandler.CreateTask)                           // 创建任务
		taskDefinitions.GET("/statistics", taskHandler.GetTaskStatistics)          // 获取任务统计
		taskDefinitions.GET("/:id", taskHandler.GetTask)                           // 获取任务详情
		taskDefinitions.PUT("/:id", taskHandler.UpdateTask)                        // 更新任务
		taskDefinitions.DELETE("/:id", taskHandler.DeleteTask)                     // 删除任务
		taskDefinitions.POST("/:id/start", taskHandler.StartTask)                  // 启动任务
		taskDefinitions.POST("/:id/stop", taskHandler.StopTask)                    // 停止任务
		taskDefinitions.POST("/:id/pause", taskHandler.PauseTask)                  // 暂停任务
		taskDefinitions.POST("/:id/resume", taskHandler.ResumeTask)                // 恢复任务
		taskDefinitions.GET("/:id/executions", executionHandler.GetTaskExecutions) // 获取任务的执行记录
	}

	// 对象存储辅助接口
	objectStorage := protected.Group("/object-storage")
	{
		objectStorage.POST("/browse", objectStorageHandler.BrowseDirectories)
		objectStorage.POST("/list-files", objectStorageHandler.ListFiles)
	}

	// 执行记录路由
	executions := protected.Group("/executions")
	{
		executions.GET("", executionHandler.ListExecutions)                              // 获取执行记录列表
		executions.GET("/statistics", executionHandler.GetExecutionStatistics)           // 获取执行统计
		executions.GET("/:execution_id", executionHandler.GetExecution)                  // 获取执行详情
		executions.POST("/:execution_id/cancel", executionHandler.CancelExecution)       // 取消执行
		executions.POST("/:execution_id/retry", executionHandler.RetryExecution)         // 重试执行
		executions.GET("/:execution_id/progress", executionHandler.GetExecutionProgress) // 获取执行进度
		executions.GET("/:execution_id/logs", executionHandler.GetExecutionLogs)         // 获取执行日志
	}

	return router
}

// ping 服务连通性检查
// @Summary Ping
// @Tags Transfer
// @Produce json
// @Success 200 {object} map[string]string
// @Router /ping [get]
func ping(c *gin.Context) {
	c.JSON(200, gin.H{"message": "pong"})
}
