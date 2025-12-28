package api

import (
	"net/http"

	commonAuth "github.com/addp/common/middleware/auth"
	commonCors "github.com/addp/common/middleware/cors"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// SetupRouter 设置路由（Phase 3 完整版本）
func SetupRouter(
	cfg *config.Config,
	devItemHandler *DevItemHandler,
	devExecutionHandler *DevExecutionHandler,
	operatorHandler *OperatorHandler,
	resourceHandler *ResourceHandler,
	sqlHandler *SQLHandler,
	notebookHandler *NotebookHandler,
	devItemService interface{}, // 添加 devItemService 参数
) *gin.Engine {
	router := gin.Default()

	// 全局中间件
	router.Use(commonCors.CORS())

	// 健康检查（无需认证）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "develop",
			"message": "Develop 数据开发模块运行正常",
			"phase":   "3",
		})
	})

	router.GET("/api/develop/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "develop",
			"message": "Develop 数据开发模块运行正常",
			"phase":   "3",
		})
	})

	// 公开 API 路由组（无需认证）- 用于算子发现等公开信息
	publicAPI := router.Group("/api/develop")
	{
		// ========== 算子发现（公开）==========
		operators := publicAPI.Group("/operators")
		{
			operators.GET("", operatorHandler.ListAllOperators)                                 // 获取所有算子
			operators.GET("/cache/info", operatorHandler.GetCacheInfo)                          // 获取缓存信息
			operators.GET("/modules/:module", operatorHandler.ListOperatorsByModule)            // 按模块获取算子
			operators.GET("/engine-types/:engineType", operatorHandler.ListOperatorsByEngineType) // 按引擎类型获取算子（新增）
			operators.GET("/:name", operatorHandler.GetOperatorDetail)                          // 获取算子详情
			operators.POST("/refresh", operatorHandler.RefreshCache)                            // 刷新缓存（内部使用）
		}
	}

	// API 路由组（需要认证）
	api := router.Group("/api/develop")
	api.Use(commonAuth.SystemAuthMiddleware(cfg.SystemServiceURL))
	{
		// ========== 任务列表 API（供 Orchestrator 使用）==========
		taskListHandler := NewTaskListHandler(devItemService.(*service.DevItemService))
		api.GET("/tasks/list", taskListHandler.ListTasks)

		// ========== 开发项管理 ==========
		items := api.Group("/items")
		{
			items.POST("", devItemHandler.CreateDevItem)                  // 创建开发项
			items.GET("", devItemHandler.ListDevItems)                    // 查询开发项列表
			items.GET("/statistics", devItemHandler.GetDevItemStatistics) // 获取统计信息
			items.GET("/:id", devItemHandler.GetDevItem)                  // 获取开发项详情
			items.PUT("/:id", devItemHandler.UpdateDevItem)               // 更新开发项
			items.DELETE("/:id", devItemHandler.DeleteDevItem)            // 删除开发项
			items.POST("/:id/execute", devExecutionHandler.ExecuteDevItem) // 执行开发项
		items.POST("/:id/execute-with-params", devExecutionHandler.ExecuteWithParams) // 参数化执行开发项（供 Orchestrator 调用）
		}

		// ========== 执行管理 ==========
		executions := api.Group("/executions")
		{
			executions.POST("", devExecutionHandler.ExecuteContent)                 // 执行临时内容
			executions.GET("", devExecutionHandler.ListExecutions)                  // 查询执行列表
			executions.GET("/statistics", devExecutionHandler.GetExecutionStatistics) // 获取执行统计
			executions.GET("/:id", devExecutionHandler.GetExecution)                // 获取执行详情
			executions.GET("/:id/logs", devExecutionHandler.GetExecutionLogs)       // 获取执行日志
			executions.POST("/:id/cancel", devExecutionHandler.CancelExecution)     // 取消执行
			executions.POST("/:id/retry", devExecutionHandler.RetryExecution)       // 重试执行
		}

		// ========== 引擎管理 ==========
		engines := api.Group("/engines")
		{
			engines.GET("", resourceHandler.ListEngines)           // 获取引擎列表
			engines.GET("/:id/schemas", resourceHandler.ListSchemas) // 获取 schemas 列表
			engines.GET("/:id/tables", resourceHandler.ListTables)   // 获取表列表
		}

		// ========== 工作流引擎管理 ==========
		api.GET("/workflow-engines", resourceHandler.ListWorkflowEngines) // 获取工作流引擎列表
		api.GET("/spark-runtimes", resourceHandler.ListSparkRuntimes)     // 获取 Spark 运行时列表

		// ========== SQL 开发 ==========
		api.GET("/test/:id", sqlHandler.TestConnection) // 测试数据源连接
		api.POST("/execute", sqlHandler.ExecuteSQL)     // 执行 SQL

		// SQL 任务管理
		sqlTasks := api.Group("/sql/tasks")
		{
			sqlTasks.POST("", sqlHandler.SaveSQLTask)       // 保存 SQL 任务
			sqlTasks.GET("", sqlHandler.ListSQLTasks)       // 获取 SQL 任务列表
			sqlTasks.GET("/:id", sqlHandler.GetSQLTask)     // 获取 SQL 任务详情
			sqlTasks.PUT("/:id", sqlHandler.UpdateSQLTask)  // 更新 SQL 任务
			sqlTasks.DELETE("/:id", sqlHandler.DeleteSQLTask) // 删除 SQL 任务
		}

		// ========== Notebook 开发 ==========
		notebooks := api.Group("/notebooks")
		{
			notebooks.POST("/jupyter-url", notebookHandler.GetJupyterURL)   // 获取 Jupyter Lab URL
			notebooks.POST("/execute", notebookHandler.ExecuteNotebook)     // 执行 Notebook
			notebooks.GET("/kernels", notebookHandler.ListKernels)          // 列出可用 Kernel
			notebooks.GET("/health", notebookHandler.HealthCheck)           // 健康检查
		}
	}

	return router
}
