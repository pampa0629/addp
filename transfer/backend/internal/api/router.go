package api

import (
	"net/http"

	"github.com/addp/common/buildinfo"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/middleware/audit"
	commonAuth "github.com/addp/common/middleware/auth"
	commonCors "github.com/addp/common/middleware/cors"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/middleware/logging"
	"github.com/addp/common/middleware/requestid"
	_ "github.com/addp/transfer/docs"
	_ "github.com/addp/transfer/i18n"
	transferauthorization "github.com/addp/transfer/internal/authorization"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// SetupRouter 设置路由
func SetupRouter(
	taskService *service.TaskService,
	executionService *service.ExecutionService,
	continuousPolicyService *service.ContinuousPolicyService,
	systemURL string,
	metaURL string,
	redisClient *redis.Client,
	systemClient *commonClient.SystemClient,
	systemServiceClient *commonClient.SystemServiceClient,
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
		c.JSON(http.StatusOK, buildinfo.Health("transfer"))
	})

	// API 路由组
	api := router.Group("/api/v1/transfer")
	platform := api.Group("")
	platform.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: systemURL}),
		commonAuth.MustNewContextGuard("platform"),
	)
	if systemClient != nil {
		platform.Use(audit.ServiceAuditMiddleware("transfer", systemServiceClient))
	}
	if continuousPolicyService != nil {
		handler := NewContinuousPolicyHandler(continuousPolicyService)
		platform.GET("/settings/continuous-policy", commonAuth.MustNewPermissionGuard(transferauthorization.PermissionTransferConfigurationRead), handler.Get)
		platform.PUT("/settings/continuous-policy", commonAuth.MustNewPermissionGuard(transferauthorization.PermissionTransferConfigurationUpdate), handler.Update)
	}

	// 公开接口（无需认证）
	public := api.Group("")
	{
		public.GET("/ping", ping)
	}

	// 受保护接口（需要 JWT 认证）
	protected := api.Group("")
	protected.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: systemURL}),
		commonAuth.MustNewContextGuard("tenant"),
	)
	permission := func(keys ...string) gin.HandlerFunc {
		return commonAuth.MustNewPermissionGuard(keys...)
	}
	// 审计日志中间件（记录到 System 模块）
	if systemClient != nil {
		protected.Use(audit.ServiceAuditMiddleware("transfer", systemServiceClient))
	}

	// 创建 Handlers
	taskHandler := NewTaskHandler(taskService)
	executionHandler := NewExecutionHandler(executionService)
	systemEngineHandler := NewSystemEngineHandler(systemClient)
	capabilityHandler := NewTransferCapabilityHandler()
	fieldDefinitionRecommendationHandler := NewFieldDefinitionRecommendationHandler(
		service.NewFieldDefinitionRecommendationService(systemClient),
	)

	protected.GET("/system-engines", permission(transferauthorization.PermissionTransferTaskRead), systemEngineHandler.List)
	protected.GET("/capabilities", permission(transferauthorization.PermissionTransferTaskRead), capabilityHandler.Get)
	protected.POST("/field-definition-recommendations", permission(transferauthorization.PermissionTransferTaskCreate), fieldDefinitionRecommendationHandler.Create)
	// 任务管理路由
	tasks := protected.Group("/tasks")
	{
		tasks.GET("", permission(transferauthorization.PermissionTransferTaskRead), taskHandler.ListTasks)                      // TaskProvider 列表和任务列表
		tasks.GET("/:task_type/:id", permission(transferauthorization.PermissionTransferTaskRead), taskHandler.ProviderGetTask) // TaskProvider 获取任务详情
		tasks.POST("/:task_type/:id/execute", permission(transferauthorization.PermissionTransferTaskExecute), taskHandler.ProviderExecuteTask)
	}

	taskDefinitions := protected.Group("/task-definitions")
	{
		taskDefinitions.POST("", permission(transferauthorization.PermissionTransferTaskCreate), taskHandler.CreateTask)                                    // 创建任务
		taskDefinitions.GET("/statistics", permission(transferauthorization.PermissionTransferTaskRead), taskHandler.GetTaskStatistics)                     // 获取任务统计
		taskDefinitions.GET("/:id", permission(transferauthorization.PermissionTransferTaskRead), taskHandler.GetTask)                                      // 获取任务详情
		taskDefinitions.PUT("/:id", permission(transferauthorization.PermissionTransferTaskUpdate), taskHandler.UpdateTask)                                 // 更新任务
		taskDefinitions.DELETE("/:id", permission(transferauthorization.PermissionTransferTaskDelete), taskHandler.DeleteTask)                              // 删除任务
		taskDefinitions.POST("/:id/start", permission(transferauthorization.PermissionTransferTaskExecute), taskHandler.StartTask)                          // 启动任务
		taskDefinitions.POST("/:id/replay", permission(transferauthorization.PermissionTransferTaskExecute), taskHandler.ReplayTask)                        // 创建 bounded replay execution
		taskDefinitions.GET("/:id/schema-change", permission(transferauthorization.PermissionTransferTaskRead), taskHandler.GetSchemaChange)                // 查询当前 CDC schema change request
		taskDefinitions.POST("/:id/schema-change/approve", permission(transferauthorization.PermissionTransferTaskUpdate), taskHandler.ApproveSchemaChange) // 审批 additive schema migration
		taskDefinitions.GET("/:id/dead-letters", permission(transferauthorization.PermissionTransferTaskRead), taskHandler.ListDeadLetters)                 // 查询 DLQ 控制索引
		taskDefinitions.GET("/:id/dead-letters/:identity", permission(transferauthorization.PermissionTransferTaskRead), taskHandler.GetDeadLetter)         // 查询单条 DLQ 控制索引
		taskDefinitions.POST("/:id/pause", permission(transferauthorization.PermissionTransferTaskUpdate), taskHandler.PauseTask)                           // 暂停任务
		taskDefinitions.POST("/:id/resume", permission(transferauthorization.PermissionTransferTaskUpdate), taskHandler.ResumeTask)                         // 恢复任务
		taskDefinitions.POST("/:id/stop", permission(transferauthorization.PermissionTransferTaskUpdate), taskHandler.StopTask)                             // 停止 continuous runtime
		taskDefinitions.GET("/:id/executions", permission(transferauthorization.PermissionTransferTaskRead), executionHandler.GetTaskExecutions)            // 获取任务的执行记录
	}

	// 执行记录路由
	executions := protected.Group("/executions")
	{
		executions.GET("", permission(transferauthorization.PermissionTransferTaskRead), executionHandler.ListExecutions)                              // 获取执行记录列表
		executions.GET("/statistics", permission(transferauthorization.PermissionTransferTaskRead), executionHandler.GetExecutionStatistics)           // 获取执行统计
		executions.GET("/:execution_id", permission(transferauthorization.PermissionTransferTaskRead), executionHandler.GetExecution)                  // 获取执行详情
		executions.POST("/:execution_id/retry", permission(transferauthorization.PermissionTransferTaskExecute), executionHandler.RetryExecution)      // 重试执行
		executions.GET("/:execution_id/progress", permission(transferauthorization.PermissionTransferTaskRead), executionHandler.GetExecutionProgress) // 获取执行进度
		executions.GET("/:execution_id/logs", permission(transferauthorization.PermissionTransferTaskRead), executionHandler.GetExecutionLogs)         // 获取执行日志
	}

	return router
}

// ping 服务连通性检查
// @Summary Ping
// @Tags Transfer
// @Produce json
// @Success 200 {object} map[string]string
// @x-addp-auth-mode "public"
// @Router /ping [get]
func ping(c *gin.Context) {
	c.JSON(200, gin.H{"message": "pong"})
}
