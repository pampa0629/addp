package api

import (
	"net/http"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/middleware/audit"
	commonAuth "github.com/addp/common/middleware/auth"
	commonCors "github.com/addp/common/middleware/cors"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/develop/backend/docs"
	_ "github.com/addp/develop/backend/i18n"
	developauthorization "github.com/addp/develop/backend/internal/authorization"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"
)

// SetupRouter 设置路由（Phase 3 完整版本）
func SetupRouter(
	cfg *config.Config,
	db *gorm.DB,
	devTaskHandler *DevTaskHandler,
	executionHandler *ExecutionHandler,
	toolApprovalHandler *ToolApprovalHandler,
	operatorHandler *OperatorHandler,
	engineHandler *EngineHandler,
	queryHandler *QueryHandler,
	notebookHandler *NotebookHandler,
	devTaskService interface{}, // 添加 devTaskService 参数
	systemClient *commonClient.SystemServiceClient, // 用于审计日志
	duckdbHandler *DuckDBHandler, // DuckDB 联邦查询
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

	taskListHandler := NewTaskListHandler(devTaskService.(*service.DevTaskService))
	assetDiscHandler := newAssetDiscoverableHandler(db)

	// 用户 API 只接受 canonical Bearer AuthContext。
	api := router.Group("/api/v1/develop")
	api.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: cfg.SystemServiceURL}),
		commonAuth.MustNewContextGuard("tenant"),
		commonAuth.MustNewDelegatedPolicyGuard("develop", map[string]commonAuth.DelegatedRoutePolicyEntry{
			"GET /api/v1/develop/workflow-engines/:id/operators": {
				RequiredScopes: []string{"workflow.operators.list"}, RequiredPermissions: []string{developauthorization.PermissionDevelopTaskRead},
			},
			"POST /api/v1/develop/workflow-validations": {
				RequiredScopes: []string{"workflow.validate"}, RequiredPermissions: []string{developauthorization.PermissionDevelopTaskRead},
			},
			"POST /api/v1/develop/executions": {
				RequiredScopes: []string{"workflow.run"}, RequiredPermissions: []string{developauthorization.PermissionDevelopTaskExecute},
			},
			"GET /api/v1/develop/executions/:execution_id": {
				RequiredScopes: []string{"execution.get"}, RequiredPermissions: []string{developauthorization.PermissionDevelopTaskRead},
			},
		}),
	)
	permission := func(keys ...string) gin.HandlerFunc {
		return commonAuth.MustNewPermissionGuard(keys...)
	}
	// 审计日志中间件（记录到 System 模块）
	if systemClient != nil {
		api.Use(audit.ServiceAuditMiddleware("develop", systemClient))
	}
	{
		taskProvider := api.Group("/task-provider")
		taskProvider.Use(commonAuth.MustNewServiceClientGuard("addp-orchestrator"))
		{
			taskProvider.GET("/tasks", permission(developauthorization.PermissionDevelopTaskProviderRead), taskListHandler.ListTasks)
			taskProvider.GET("/tasks/:task_type/:id", permission(developauthorization.PermissionDevelopTaskProviderRead), devTaskHandler.ProviderGetDevTask)
			taskProvider.POST("/tasks/:task_type/:id/execute", permission(developauthorization.PermissionDevelopTaskProviderExecute), executionHandler.ProviderExecuteDevTask)
			taskProvider.GET("/executions/:execution_id", permission(developauthorization.PermissionDevelopTaskProviderRead), executionHandler.ProviderGetExecution)
		}

		api.GET(
			"/assets/discoverable",
			commonAuth.MustNewServiceClientGuard("addp-asset"),
			permission(developauthorization.PermissionDevelopTaskRead),
			assetDiscHandler.listDiscoverableAssets,
		)

		// ========== 开发任务定义管理 ==========
		taskDefinitions := api.Group("/task-definitions")
		{
			taskDefinitions.POST("", permission(developauthorization.PermissionDevelopTaskCreate), devTaskHandler.CreateDevTask)
			taskDefinitions.GET("", permission(developauthorization.PermissionDevelopTaskRead), devTaskHandler.ListDevTasks)
			taskDefinitions.GET("/statistics", permission(developauthorization.PermissionDevelopTaskRead), devTaskHandler.GetDevTaskStatistics)
			taskDefinitions.GET("/:id", permission(developauthorization.PermissionDevelopTaskRead), devTaskHandler.GetDevTask)
			taskDefinitions.PUT("/:id", permission(developauthorization.PermissionDevelopTaskUpdate), devTaskHandler.UpdateDevTask)
			taskDefinitions.DELETE("/:id", permission(developauthorization.PermissionDevelopTaskDelete), devTaskHandler.DeleteDevTask)
			taskDefinitions.POST("/:id/execute", permission(developauthorization.PermissionDevelopTaskExecute), executionHandler.ExecuteDevTask)
		}

		// ========== 执行管理 ==========
		executions := api.Group("/executions")
		{
			executions.POST("", permission(developauthorization.PermissionDevelopTaskExecute), executionHandler.ExecuteContent)
			executions.GET("", permission(developauthorization.PermissionDevelopTaskRead), executionHandler.ListExecutions)
			executions.GET("/statistics", permission(developauthorization.PermissionDevelopTaskRead), executionHandler.GetExecutionStatistics)
			executions.GET("/:execution_id", permission(developauthorization.PermissionDevelopTaskRead), executionHandler.GetExecution)
			executions.GET("/:execution_id/logs", permission(developauthorization.PermissionDevelopTaskRead), executionHandler.GetExecutionLogs)
			executions.POST("/:execution_id/retry", permission(developauthorization.PermissionDevelopTaskExecute), executionHandler.RetryExecution)
		}

		// ========== 委托 Tool 审批 ==========
		approvals := api.Group("/approvals")
		{
			approvals.GET("/:id", permission(developauthorization.PermissionDevelopTaskRead), toolApprovalHandler.GetApproval)
			approvals.POST("/:id/decision", permission(developauthorization.PermissionDevelopTaskExecute), toolApprovalHandler.DecideApproval)
		}

		// ========== 引擎管理 ==========
		engines := api.Group("/engines")
		{
			engines.GET("", permission(developauthorization.PermissionDevelopTaskRead), engineHandler.ListEngines)
		}
		api.GET("/query-modes", permission(developauthorization.PermissionDevelopTaskRead), engineHandler.ListQueryModes)

		// ========== 工作流引擎管理 ==========
		api.GET("/workflow-engines", permission(developauthorization.PermissionDevelopTaskRead), engineHandler.ListWorkflowEngines)
		api.GET("/workflow-engines/:id/operators", permission(developauthorization.PermissionDevelopTaskRead), operatorHandler.ListOperatorsByWorkflowEngine)
		api.POST("/workflow-validations", permission(developauthorization.PermissionDevelopTaskRead), operatorHandler.ValidateWorkflow)
		api.GET("/spark-runtimes", permission(developauthorization.PermissionDevelopTaskRead), engineHandler.ListSparkRuntimes)

		// ========== 查询开发 ==========
		api.GET(
			"/test/:id",
			permission(
				developauthorization.PermissionDevelopTaskExecute,
				developauthorization.PermissionDevelopDataReadExecute,
			),
			queryHandler.TestConnection,
		)
		api.GET("/engines/:id/sample-query", permission(developauthorization.PermissionDevelopTaskRead), queryHandler.GetSampleQuery)
		api.POST("/execute", permission(developauthorization.PermissionDevelopTaskExecute), queryHandler.ExecuteQuery)

		// ========== Notebook 开发 ==========
		api.GET("/notebook-engines", permission(developauthorization.PermissionDevelopNotebookRead), notebookHandler.ListNotebookEngines)
		api.GET("/notebook-engines/:id/kernels", permission(developauthorization.PermissionDevelopNotebookRead), notebookHandler.ListKernels)
		notebooks := api.Group("/notebooks")
		{
			notebooks.POST("/upload", permission(developauthorization.PermissionDevelopNotebookCreate), notebookHandler.UploadNotebook)
			notebooks.GET("", permission(developauthorization.PermissionDevelopNotebookRead), notebookHandler.ListNotebooks)
			notebooks.PUT("/:id/runtime-binding", permission(developauthorization.PermissionDevelopNotebookUpdate), notebookHandler.UpdateRuntimeBinding)
			notebooks.GET("/:id/download", permission(developauthorization.PermissionDevelopNotebookRead), notebookHandler.DownloadNotebook)
			notebooks.DELETE("/:id", permission(developauthorization.PermissionDevelopNotebookDelete), notebookHandler.DeleteNotebook)
		}

		// ========== DuckDB 联邦查询 ==========
		if duckdbHandler != nil {
			duckdb := api.Group("/duckdb")
			{
				duckdb.GET("/sources", permission(developauthorization.PermissionDevelopTaskRead), duckdbHandler.GetFederatedSources)
				duckdb.GET("/test", permission(developauthorization.PermissionDevelopTaskRead), duckdbHandler.TestConnection)
				duckdb.GET("/sample-query", permission(developauthorization.PermissionDevelopTaskRead), duckdbHandler.GetSampleQuery)
			}
		}
	}

	return router
}
