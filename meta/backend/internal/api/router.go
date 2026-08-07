package api

import (
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	"github.com/addp/common/middleware/audit"
	auth "github.com/addp/common/middleware/auth"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/meta/docs"
	_ "github.com/addp/meta/i18n"
	metaauthorization "github.com/addp/meta/internal/authorization"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"
)

func SetupRouter(cfg *config.Config, db *gorm.DB, engineService *service.EngineService, scanService *service.ScanService, taskService *service.ScanTaskService, executionService *service.ScanExecutionService, redisClient *redis.Client, systemClient *commonClient.SystemServiceClient, lineageServices ...*service.LineageService) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(i18nmiddleware.I18nMiddleware())
	router.Use(requestLogger())

	// Swagger 文档
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 注意：CORS 由 Gateway 统一处理，此处无需设置 CORS 中间件

	if engineService == nil || scanService == nil {
		panic("engineService and scanService must be provided to SetupRouter")
	}

	metadataQueryService := service.NewMetadataQueryService(db)
	lineageService := service.NewLineageService(db, engineService)
	if len(lineageServices) > 0 && lineageServices[0] != nil {
		lineageService = lineageServices[0]
	}
	inspectService := service.NewInspectService(cfg)

	// 创建Handler
	handler := NewHandler(engineService, scanService, taskService, executionService, metadataQueryService, inspectService, lineageService)
	assetDiscHandler := newAssetDiscoverableHandler(db)

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})

	// API路由组（需要认证）
	api := router.Group("/api/v1/meta")
	api.Use(
		auth.MustNewMiddleware(auth.MiddlewareConfig{SystemURL: cfg.SystemServiceURL}),
		auth.MustNewContextGuard("tenant"),
		auth.MustNewDelegatedPolicyGuard("meta", map[string]auth.DelegatedRoutePolicyEntry{
			"GET /api/v1/meta/resource-tree/:engine_id/ancestors": {
				RequiredScopes:      []string{"resource.ancestors.get"},
				RequiredPermissions: []string{metaauthorization.PermissionMetaCatalogRead},
			},
		}),
	)
	permission := func(keys ...string) gin.HandlerFunc {
		return auth.MustNewPermissionGuard(keys...)
	}
	// 审计日志中间件（记录到 System 模块）
	if systemClient != nil {
		api.Use(audit.ServiceAuditMiddleware("meta", systemClient))
	}
	{
		// 资产发现接口（供 Asset 模块调用）
		api.GET(
			"/assets/discoverable",
			auth.MustNewServiceClientGuard("addp-asset"),
			permission(metaauthorization.PermissionMetaCatalogRead),
			assetDiscHandler.listDiscoverableAssets,
		)

		// 资源相关
		api.GET("/engines", permission(metaauthorization.PermissionMetaCatalogRead), handler.GetEngines)

		// 扫描相关
		api.POST("/scan/run/unscanned", permission(metaauthorization.PermissionMetaScanTaskExecute), handler.CreateUnscannedScanRuns)
		api.POST("/scan/run/manual", permission(metaauthorization.PermissionMetaScanTaskExecute), handler.CreateManualScanRun)
		api.POST("/inspect", permission(metaauthorization.PermissionMetaInspectExecute), handler.InspectAttributes)
		api.GET("/scan/runs", permission(metaauthorization.PermissionMetaScanTaskRead), handler.ListScanRuns)
		api.GET("/executions/:execution_id", permission(metaauthorization.PermissionMetaScanTaskRead), handler.GetExecution)
		api.GET("/tasks", permission(metaauthorization.PermissionMetaScanTaskRead), handler.ListProviderScanTasks)
		api.GET("/tasks/:task_type/:id", permission(metaauthorization.PermissionMetaScanTaskRead), handler.ProviderGetScanTask)
		api.POST("/tasks/:task_type/:id/execute", permission(metaauthorization.PermissionMetaScanTaskExecute), handler.ProviderExecuteScanTask)
		api.GET("/scan/tasks", permission(metaauthorization.PermissionMetaScanTaskRead), handler.ListScanTasks)
		api.POST("/scan/tasks", permission(metaauthorization.PermissionMetaScanTaskCreate), handler.CreateScanTask)
		api.PUT("/scan/tasks/engines/:engine_id", permission(metaauthorization.PermissionMetaScanTaskUpdate), handler.UpsertEngineScanTask)
		api.DELETE("/scan/tasks/engines/:engine_id", permission(metaauthorization.PermissionMetaScanTaskDelete), handler.DeleteEngineScanTask)
		api.PUT("/scan/tasks/:task_id", permission(metaauthorization.PermissionMetaScanTaskUpdate), handler.UpdateScanTask)
		api.DELETE("/scan/tasks/:task_id", permission(metaauthorization.PermissionMetaScanTaskDelete), handler.DeleteScanTask)
		api.POST("/scan/tasks/:task_id/trigger", permission(metaauthorization.PermissionMetaScanTaskExecute), handler.TriggerScanTask)

		// 元数据相关
		api.GET("/engines/:engine_id/items", permission(metaauthorization.PermissionMetaCatalogRead), handler.ListEngineItems)

		// 新增：用于 Manager 模块的元数据查询接口
		api.GET("/engines/:engine_id/tree", permission(metaauthorization.PermissionMetaCatalogRead), handler.GetMetadataTree)
		api.GET("/resource-tree/:engine_id", permission(metaauthorization.PermissionMetaCatalogRead), handler.GetResourceTree)
		api.GET("/resource-tree/:engine_id/node", permission(metaauthorization.PermissionMetaCatalogRead), handler.GetResourceTreeNode)
		api.GET("/resource-tree/:engine_id/ancestors", permission(metaauthorization.PermissionMetaCatalogRead), handler.GetResourceTreeAncestors)
		api.GET("/resource-tree/:engine_id/search", permission(metaauthorization.PermissionMetaCatalogRead), handler.SearchResourceTree)
		api.POST("/resource-tree/:engine_id/refresh", permission(metaauthorization.PermissionMetaScanTaskExecute), handler.RefreshResourceTreeNode)
		api.GET("/nodes/:node_id", permission(metaauthorization.PermissionMetaCatalogRead), handler.GetMetaNodeByID)
		api.GET("/nodes/:node_id/ancestors", permission(metaauthorization.PermissionMetaCatalogRead), handler.GetNodeAncestors)
		api.GET("/nodes/:node_id/children", permission(metaauthorization.PermissionMetaCatalogRead), handler.GetNodeChildren)
		api.GET("/nodes/:node_id/items", permission(metaauthorization.PermissionMetaCatalogRead), handler.GetNodeItems)
		api.GET("/nodes/by-catalog-path", permission(metaauthorization.PermissionMetaCatalogRead), handler.QueryNodeByCatalogPath)
		api.GET("/items/by-catalog-path", permission(metaauthorization.PermissionMetaCatalogRead), handler.QueryItemByCatalogPath)
		api.GET("/items/:item_id/fields", permission(metaauthorization.PermissionMetaCatalogRead), handler.GetItemFieldsByID)
		api.GET("/items/:item_id/spatial", permission(metaauthorization.PermissionMetaCatalogRead), handler.GetItemSpatialMetadataByID)
		api.GET("/items/:item_id/ancestors", permission(metaauthorization.PermissionMetaCatalogRead), handler.GetItemAncestors)
		api.POST("/items/:item_id/refresh", permission(metaauthorization.PermissionMetaScanTaskExecute), handler.RefreshItem)
		api.GET("/items/:item_id", permission(metaauthorization.PermissionMetaCatalogRead), handler.GetItemByID)

		// 统计接口
		api.GET("/stats", permission(metaauthorization.PermissionMetaCatalogRead), handler.GetStats)
		api.GET("/lineage/graph", permission(metaauthorization.PermissionMetaLineageRead), handler.GetLineageGraph)
		api.POST("/lineage/services", auth.MustNewServiceClientGuard("addp-service"), permission(metaauthorization.PermissionMetaLineageCreate), handler.RecordServicePublication)
	}

	return router
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		log := logger.With(
			"component", "http_server",
			"method", c.Request.Method,
			"path", path,
			"status", status,
			"latency_ms", latency.Milliseconds(),
			"client_ip", c.ClientIP(),
		)

		if len(c.Errors) > 0 {
			log = log.With("errors", c.Errors.String())
		}

		switch {
		case status >= 500:
			log.Error("请求处理完成")
		case status >= 400:
			log.Warn("请求处理完成")
		default:
			log.Info("请求处理完成")
		}
	}
}
