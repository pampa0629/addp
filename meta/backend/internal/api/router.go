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
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"
)

func SetupRouter(cfg *config.Config, db *gorm.DB, engineService *service.EngineService, scanService *service.ScanService, taskService *service.ScanTaskService, executionService *service.ScanExecutionService, redisClient *redis.Client, systemClient *commonClient.SystemClient) *gin.Engine {
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
	inspectService := service.NewInspectService(cfg)

	// 创建Handler
	handler := NewHandler(engineService, scanService, taskService, executionService, metadataQueryService, inspectService)
	assetDiscHandler := newAssetDiscoverableHandler(db)

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})

	// API路由组（需要认证）
	api := router.Group("/api/v1/meta")
	// 使用 Redis 缓存中间件 (TTL: 5分钟, 减少 System 调用 90%)
	if redisClient != nil {
		api.Use(auth.CachedSystemAuthMiddleware(cfg.SystemServiceURL, redisClient, 5*time.Minute))
	} else {
		// Fallback: 无缓存模式
		api.Use(auth.SystemAuthMiddleware(cfg.SystemServiceURL))
	}
	// 审计日志中间件（记录到 System 模块）
	if systemClient != nil {
		api.Use(audit.AuditMiddleware("meta", systemClient))
	}
	api.Use(auth.TenantIsolationMiddleware()) // 租户隔离
	{
		// 资产发现接口（供 Asset 模块调用）
		api.GET("/assets/discoverable", assetDiscHandler.listDiscoverableAssets)

		// 资源相关
		api.GET("/engines", handler.GetEngines)

		// 扫描相关
		api.POST("/scan/run/unscanned", handler.CreateUnscannedScanRuns)
		api.POST("/scan/run/manual", handler.CreateManualScanRun)
		api.POST("/inspect", handler.InspectAttributes)
		api.GET("/scan/runs", handler.ListScanRuns)
		api.GET("/executions/:execution_id", handler.GetExecution)
		api.GET("/tasks", handler.ListProviderScanTasks)
		api.GET("/tasks/:task_type/:id", handler.ProviderGetScanTask)
		api.POST("/tasks/:task_type/:id/execute", handler.ProviderExecuteScanTask)
		api.GET("/scan/tasks", handler.ListScanTasks)
		api.POST("/scan/tasks", handler.CreateScanTask)
		api.PUT("/scan/tasks/engines/:engine_id", handler.UpsertEngineScanTask)
		api.DELETE("/scan/tasks/engines/:engine_id", handler.DeleteEngineScanTask)
		api.PUT("/scan/tasks/:task_id", handler.UpdateScanTask)
		api.DELETE("/scan/tasks/:task_id", handler.DeleteScanTask)
		api.POST("/scan/tasks/:task_id/trigger", handler.TriggerScanTask)

		// 元数据相关
		api.GET("/engines/:engine_id/items", handler.ListEngineItems)

		// 新增：用于 Manager 模块的元数据查询接口
		api.GET("/engines/:engine_id/tree", handler.GetMetadataTree)
		api.GET("/resource-tree/:engine_id", handler.GetResourceTree)
		api.GET("/resource-tree/:engine_id/node", handler.GetResourceTreeNode)
		api.GET("/resource-tree/:engine_id/ancestors", handler.GetResourceTreeAncestors)
		api.GET("/resource-tree/:engine_id/search", handler.SearchResourceTree)
		api.POST("/resource-tree/:engine_id/refresh", handler.RefreshResourceTreeNode)
		api.GET("/nodes/:node_id", handler.GetMetaNodeByID)
		api.GET("/nodes/:node_id/ancestors", handler.GetNodeAncestors)
		api.GET("/nodes/:node_id/children", handler.GetNodeChildren)
		api.GET("/nodes/:node_id/items", handler.GetNodeItems)
		api.GET("/nodes/by-catalog-path", handler.QueryNodeByCatalogPath)
		api.GET("/items/by-catalog-path", handler.QueryItemByCatalogPath)
		api.GET("/items/:item_id/fields", handler.GetItemFieldsByID)
		api.GET("/items/:item_id/spatial", handler.GetItemSpatialMetadataByID)
		api.GET("/items/:item_id/ancestors", handler.GetItemAncestors)
		api.POST("/items/:item_id/refresh", handler.RefreshItem)
		api.GET("/items/:item_id", handler.GetItemByID)

		// 统计接口
		api.GET("/stats", handler.GetStats)

		// 缓存管理
		api.DELETE("/cache/engines/:engine_id", handler.ClearResourceCache)
		api.POST("/cache/refresh", handler.RefreshResourceCache)
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
