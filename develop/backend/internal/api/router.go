package api

import (
	"net/http"

	"github.com/addp/common/buildinfo"
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
	queryPolicyHandlers ...*QueryPolicyHandler,
) *gin.Engine {
	router := gin.Default()

	// Swagger 文档
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 全局中间件
	router.Use(commonCors.CORS())
	router.Use(i18nmiddleware.I18nMiddleware())

	// 健康检查（无需认证）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, buildinfo.Health("develop"))
	})

	taskListHandler := NewTaskListHandler(devTaskService.(*service.DevTaskService))
	assetDiscHandler := newAssetDiscoverableHandler(db)
	var queryPolicyHandler *QueryPolicyHandler
	if len(queryPolicyHandlers) > 0 {
		queryPolicyHandler = queryPolicyHandlers[0]
	}
	if queryPolicyHandler != nil {
		settings := router.Group("/api/v1/develop")
		settings.Use(commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: cfg.SystemServiceURL}))
		settings.GET("/settings/query-policy", commonAuth.MustNewPermissionGuard(developauthorization.PermissionDevelopConfigurationRead), queryPolicyHandler.Get)
		settings.PUT("/settings/query-policy", commonAuth.MustNewPermissionGuard(developauthorization.PermissionDevelopConfigurationUpdate), queryPolicyHandler.Update)
	}

	// Kernel Capability 只允许访问独立的只读引擎发现端点。
	router.GET("/api/v1/develop/notebook-kernel-sessions/:session_id/engine-descriptors", notebookHandler.ListSessionEngineDescriptors)
	router.POST("/api/v1/develop/notebook-kernel-sessions/:session_id/catalog/children", notebookHandler.ListSessionCatalogChildren)
	router.POST("/api/v1/develop/notebook-kernel-sessions/:session_id/table-scans", notebookHandler.StreamSessionTable)
	router.POST("/api/v1/develop/notebook-kernel-sessions/:session_id/record-scans", notebookHandler.StreamSessionRecords)
	router.POST("/api/v1/develop/notebook-kernel-sessions/:session_id/queries", notebookHandler.StreamSessionQuery)
	router.POST("/api/v1/develop/notebook-kernel-sessions/:session_id/graph-samples", notebookHandler.SampleSessionGraph)
	router.POST("/api/v1/develop/notebook-kernel-sessions/:session_id/graph-queries", notebookHandler.QuerySessionGraph)
	router.POST("/api/v1/develop/notebook-kernel-sessions/:session_id/content-reads", notebookHandler.StreamSessionContent)
	router.POST("/api/v1/develop/notebook-kernel-sessions/:session_id/change-streams", notebookHandler.StreamSessionChanges)

	// Notebook 原生交互协议使用单会话、单路径 HttpOnly 能力 Cookie。
	notebookCopilot := router.Group("/api/v1/develop/notebook-copilot-sessions/:session_id")
	notebookCopilot.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: cfg.SystemServiceURL}),
		commonAuth.MustNewContextGuard("tenant"),
		commonAuth.MustNewPermissionGuard(
			developauthorization.PermissionDevelopNotebookUpdate,
			developauthorization.PermissionDevelopTaskRead,
		),
	)
	notebookCopilot.POST("/generate", notebookHandler.GenerateSessionNotebookCell)
	router.Any("/api/v1/develop/notebook-sessions/:session_id", notebookHandler.ProxySession)
	router.Any("/api/v1/develop/notebook-sessions/:session_id/*path", notebookHandler.ProxySession)

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
			taskDefinitions.GET("/:id/storage-engine-bindings", permission(developauthorization.PermissionDevelopTaskRead), devTaskHandler.ListWorkflowStorageEngineBindings)
			taskDefinitions.PUT("/:id/storage-engine-bindings/:source_engine_id", permission(developauthorization.PermissionDevelopTaskUpdate), devTaskHandler.RebindWorkflowStorageEngine)
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

		// ========== 工作流引擎管理 ==========
		api.GET("/workflow-engines", permission(developauthorization.PermissionDevelopTaskRead), engineHandler.ListWorkflowEngines)
		api.GET("/workflow-engines/:id/operators", permission(developauthorization.PermissionDevelopTaskRead), operatorHandler.ListOperatorsByWorkflowEngine)
		api.POST("/workflow-validations", permission(developauthorization.PermissionDevelopTaskRead), operatorHandler.ValidateWorkflow)
		api.GET("/spark-runtimes", permission(developauthorization.PermissionDevelopTaskRead), engineHandler.ListSparkRuntimes)

		// ========== 查询开发 ==========
		api.POST(
			"/query-preflight",
			permission(developauthorization.PermissionDevelopTaskExecute),
			queryHandler.PreflightQuery,
		)
		api.GET(
			"/test/:id",
			permission(
				developauthorization.PermissionDevelopTaskExecute,
				developauthorization.PermissionDevelopDataReadExecute,
			),
			queryHandler.TestConnection,
		)
		api.GET(
			"/engines/:id/sample-query",
			permission(
				developauthorization.PermissionDevelopTaskExecute,
				developauthorization.PermissionDevelopDataReadExecute,
			),
			queryHandler.GetSampleQuery,
		)
		// ========== Notebook 开发 ==========
		api.GET("/notebook-engines", permission(developauthorization.PermissionDevelopNotebookRead), notebookHandler.ListNotebookEngines)
		api.GET("/notebook-engines/:id/kernels", permission(developauthorization.PermissionDevelopNotebookRead), notebookHandler.ListKernels)
		notebooks := api.Group("/notebooks")
		{
			notebooks.POST("", permission(developauthorization.PermissionDevelopNotebookCreate), notebookHandler.CreateNotebook)
			notebooks.POST("/upload", permission(developauthorization.PermissionDevelopNotebookCreate), notebookHandler.UploadNotebook)
			notebooks.GET("", permission(developauthorization.PermissionDevelopNotebookRead), notebookHandler.ListNotebooks)
			notebooks.POST("/:id/sessions", permission(
				developauthorization.PermissionDevelopNotebookUpdate,
				developauthorization.PermissionDevelopTaskRead,
			), notebookHandler.CreateSession)
			notebooks.DELETE("/:id/sessions/:session_id", permission(developauthorization.PermissionDevelopNotebookUpdate), notebookHandler.CloseSession)
			notebooks.PUT("/:id/runtime-binding", permission(developauthorization.PermissionDevelopNotebookUpdate), notebookHandler.UpdateRuntimeBinding)
			notebooks.GET("/:id/download", permission(developauthorization.PermissionDevelopNotebookRead), notebookHandler.DownloadNotebook)
			notebooks.DELETE("/:id", permission(developauthorization.PermissionDevelopNotebookDelete), notebookHandler.DeleteNotebook)
		}

	}

	return router
}
