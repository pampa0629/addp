package api

import (
	"net/http"
	"strconv"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/middleware/audit"
	commonAuth "github.com/addp/common/middleware/auth"
	commonCors "github.com/addp/common/middleware/cors"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/service"
	_ "github.com/addp/develop/backend/i18n"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	_ "github.com/addp/develop/backend/docs"
)

// internalAPIKeyMiddleware 处理内部 API 认证（X-Internal-API-Key）
// 如果请求包含有效的内部 API Key，则设置上下文并标记为已认证
func internalAPIKeyMiddleware(expectedKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 检查是否有内部 API Key
		apiKey := c.GetHeader("X-Internal-API-Key")
		if apiKey != "" && apiKey == expectedKey {
			// 内部 API 调用，从 X-Tenant-ID header 读取租户 ID
			tenantID := uint(0)
			if tenantIDStr := c.GetHeader("X-Tenant-ID"); tenantIDStr != "" {
				if tid, err := strconv.ParseUint(tenantIDStr, 10, 32); err == nil {
					tenantID = uint(tid)
				}
			}
			c.Set(commonAuth.ContextUserIDKey, uint(1))
			c.Set(commonAuth.ContextUsernameKey, "internal-api-call")
			c.Set(commonAuth.ContextTenantIDKey, tenantID)
			c.Set("internal_api_authenticated", true) // 标记为已通过内部认证
		}
		c.Next()
	}
}

// systemAuthMiddlewareWrapper 包装 SystemAuthMiddleware，支持跳过已通过内部认证的请求
func systemAuthMiddlewareWrapper(systemURL string) gin.HandlerFunc {
	authMiddleware := commonAuth.SystemAuthMiddleware(systemURL)

	return func(c *gin.Context) {
		// 如果已经通过内部 API 认证，跳过 JWT 认证
		if authenticated, exists := c.Get("internal_api_authenticated"); exists && authenticated.(bool) {
			c.Next()
			return
		}

		// 否则，执行正常的 JWT 认证
		authMiddleware(c)
	}
}

// SetupRouter 设置路由（Phase 3 完整版本）
func SetupRouter(
	cfg *config.Config,
	db *gorm.DB,
	devItemHandler *DevItemHandler,
	devExecutionHandler *DevExecutionHandler,
	operatorHandler *OperatorHandler,
	engineHandler *EngineHandler,
	queryHandler *QueryHandler,
	notebookHandler *NotebookHandler,
	jupyterInstanceHandler *JupyterInstanceHandler, // Jupyter 实例管理 (Docker 方案,已废弃)
	jupyterVenvHandler *JupyterVenvHandler,         // Jupyter 虚拟环境管理 (新方案)
	devTaskService interface{},                     // 添加 devTaskService 参数
	systemClient *commonClient.SystemClient,        // 用于审计日志
	duckdbHandler *DuckDBHandler,                   // DuckDB 联邦查询
) *gin.Engine {
	router := gin.Default()

	// Swagger 文档
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 全局中间件
	router.Use(commonCors.CORS())
	router.Use(i18nmiddleware.I18nMiddleware())

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
	publicAPI := router.Group("/api/v1/develop")
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
	api := router.Group("/api/v1/develop")
	// 添加内部 API 认证中间件（支持 X-Internal-API-Key）
	api.Use(internalAPIKeyMiddleware(cfg.InternalAPIKey))
	// 使用包装后的认证中间件（支持跳过已通过内部认证的请求）
	api.Use(systemAuthMiddlewareWrapper(cfg.SystemServiceURL))
	// 审计日志中间件（记录到 System 模块）
	if systemClient != nil {
		api.Use(audit.AuditMiddleware("develop", systemClient))
	}
	assetDiscHandler := newAssetDiscoverableHandler(db)
	{
		// ========== 资产发现接口（供 Asset 模块调用）==========
		api.GET("/assets/discoverable", assetDiscHandler.listDiscoverableAssets)

		// ========== 任务列表 API（供 Orchestrator 使用）==========
		taskListHandler := NewTaskListHandler(devTaskService.(*service.DevTaskService))
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
			engines.GET("", engineHandler.ListEngines)           // 获取引擎列表
			engines.GET("/nfs", engineHandler.ListNfsEngines)     // 获取 NFS 引擎列表
			engines.GET("/:id/schemas", engineHandler.ListSchemas) // 获取 schemas 列表
			engines.GET("/:id/tables", engineHandler.ListTables)   // 获取表列表
		}

		// ========== 工作流引擎管理 ==========
		api.GET("/workflow-engines", engineHandler.ListWorkflowEngines) // 获取工作流引擎列表
		api.GET("/spark-runtimes", engineHandler.ListSparkRuntimes)     // 获取 Spark 运行时列表

		// ========== 查询开发 ==========
		api.GET("/test/:id", queryHandler.TestConnection)           // 测试数据源连接
		api.GET("/engines/:id/sample-query", queryHandler.GetSampleQuery) // 获取样例查询
		api.POST("/execute", queryHandler.ExecuteQuery)              // 执行查询

		// 查询任务管理
		queryTasks := api.Group("/query/tasks")
		{
			queryTasks.POST("", queryHandler.SaveQueryTask)       // 保存查询任务
			queryTasks.GET("", queryHandler.ListQueryTasks)       // 获取查询任务列表
			queryTasks.GET("/:id", queryHandler.GetQueryTask)     // 获取查询任务详情
			queryTasks.PUT("/:id", queryHandler.UpdateQueryTask)  // 更新查询任务
			queryTasks.DELETE("/:id", queryHandler.DeleteQueryTask) // 删除查询任务
		}

		// ========== Notebook 开发 ==========
		notebooks := api.Group("/notebooks")
		{
			notebooks.POST("/jupyter-url", notebookHandler.GetJupyterURL)     // 获取 Jupyter Lab URL
			notebooks.POST("/execute", notebookHandler.ExecuteNotebook)       // 执行 Notebook（临时）
			notebooks.GET("/kernels", notebookHandler.ListKernels)            // 列出可用 Kernel
			notebooks.GET("/health", notebookHandler.HealthCheck)             // 健康检查

			// 新增：Notebook 管理 API
			notebooks.POST("/upload", notebookHandler.UploadNotebook)         // 上传 Notebook
			notebooks.GET("", notebookHandler.ListNotebooks)                  // 列出 Notebooks
			notebooks.GET("/:id/download", notebookHandler.DownloadNotebook)  // 下载 Notebook
			notebooks.DELETE("/:id", notebookHandler.DeleteNotebook)          // 删除 Notebook
		}

		// ========== Jupyter 实例管理 (Docker 方案,已废弃) ==========
		if jupyterInstanceHandler != nil {
			jupyter := api.Group("/jupyter")
			{
				jupyter.POST("/instance/start", jupyterInstanceHandler.StartInstance)     // 启动 Jupyter 实例
				jupyter.POST("/instance/stop", jupyterInstanceHandler.StopInstance)       // 停止 Jupyter 实例
				jupyter.GET("/instance/status", jupyterInstanceHandler.GetInstanceStatus) // 获取实例状态
				jupyter.GET("/instances", jupyterInstanceHandler.ListInstances)           // 列出所有实例（管理员）
			}
		}

		// ========== Jupyter 虚拟环境管理 (新方案,开发环境) ==========
		if jupyterVenvHandler != nil {
			jupyter := api.Group("/jupyter")
			{
				// 租户虚拟环境管理
				jupyter.GET("/venv/status", jupyterVenvHandler.GetVenvStatus)       // 获取租户虚拟环境状态
				jupyter.POST("/venv/init", jupyterVenvHandler.InitVenv)            // 初始化租户虚拟环境
				jupyter.DELETE("/venv", jupyterVenvHandler.DeleteVenv)             // 删除租户虚拟环境

				// 管理员接口
				jupyter.GET("/venvs", jupyterVenvHandler.ListVenvs)                // 列出所有租户虚拟环境
				jupyter.POST("/venv/:tenant_id/init", jupyterVenvHandler.InitVenvByID) // 为指定租户初始化虚拟环境

				// Jupyter Server 状态
				jupyter.GET("/server/status", jupyterVenvHandler.GetJupyterServerStatus) // 获取 Jupyter Server 状态
			}
		}

		// ========== DuckDB 联邦查询 ==========
		if duckdbHandler != nil {
			duckdb := api.Group("/duckdb")
			{
				duckdb.POST("/query", duckdbHandler.ExecuteFederatedQuery)   // 执行联邦查询
				duckdb.GET("/sources", duckdbHandler.GetFederatedSources)    // 获取可查询数据源
				duckdb.GET("/test", duckdbHandler.TestConnection)            // 测试 DuckDB 引擎可用性
				duckdb.GET("/sample-query", duckdbHandler.GetSampleQuery)    // 获取样例查询
			}
		}
	}

	return router
}
