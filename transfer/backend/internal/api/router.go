package api

import (
	"github.com/addp/transfer/internal/middleware"
	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

// SetupRouter 设置路由
func SetupRouter(
	taskService *service.TaskService,
	executionService *service.ExecutionService,
	jwtSecret string,
) *gin.Engine {
	router := gin.Default()

	// 全局中间件
	router.Use(middleware.CORS())
	router.Use(middleware.Logger())
	router.Use(middleware.Recovery())

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "ok",
			"service": "transfer",
		})
	})

	// API 路由组
	api := router.Group("/api")

	// 公开接口（无需认证）
	public := api.Group("")
	{
		public.GET("/ping", func(c *gin.Context) {
			c.JSON(200, gin.H{"message": "pong"})
		})
	}

	// 受保护接口（需要 JWT 认证）
	protected := api.Group("")
	protected.Use(middleware.Auth(jwtSecret))

	// 创建 Handlers
	taskHandler := NewTaskHandler(taskService)
	executionHandler := NewExecutionHandler(executionService)

	// 任务管理路由
	tasks := protected.Group("/tasks")
	{
		tasks.POST("", taskHandler.CreateTask)                     // 创建任务
		tasks.GET("", taskHandler.ListTasks)                       // 获取任务列表
		tasks.GET("/statistics", taskHandler.GetTaskStatistics)    // 获取任务统计
		tasks.GET("/:id", taskHandler.GetTask)                     // 获取任务详情
		tasks.PUT("/:id", taskHandler.UpdateTask)                  // 更新任务
		tasks.DELETE("/:id", taskHandler.DeleteTask)               // 删除任务
		tasks.POST("/:id/start", taskHandler.StartTask)            // 启动任务
		tasks.POST("/:id/stop", taskHandler.StopTask)              // 停止任务
		tasks.POST("/:id/pause", taskHandler.PauseTask)            // 暂停任务
		tasks.POST("/:id/resume", taskHandler.ResumeTask)          // 恢复任务
		tasks.GET("/:id/executions", executionHandler.GetTaskExecutions) // 获取任务的执行记录
		tasks.POST("/:id/mappings", taskHandler.CreateDataMapping) // 创建字段映射
		tasks.GET("/:id/mappings", taskHandler.GetTaskMappings)    // 获取任务的字段映射
	}

	// 字段映射路由
	mappings := protected.Group("/mappings")
	{
		mappings.DELETE("/:id", taskHandler.DeleteDataMapping) // 删除字段映射
	}

	// 执行记录路由
	executions := protected.Group("/executions")
	{
		executions.GET("", executionHandler.ListExecutions)              // 获取执行记录列表
		executions.GET("/statistics", executionHandler.GetExecutionStatistics) // 获取执行统计
		executions.GET("/:id", executionHandler.GetExecution)            // 获取执行详情
		executions.POST("/:id/cancel", executionHandler.CancelExecution) // 取消执行
		executions.POST("/:id/retry", executionHandler.RetryExecution)   // 重试执行
		executions.GET("/:id/progress", executionHandler.GetExecutionProgress) // 获取执行进度
		executions.GET("/:id/logs", executionHandler.GetExecutionLogs)   // 获取执行日志
	}

	return router
}
