package api

import (
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	"github.com/addp/common/middleware/audit"
	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func SetupRouter(cfg *config.Config, db *gorm.DB, engineService *service.EngineService, scanService *service.ScanService, taskService *service.ScanTaskService, redisClient *redis.Client, systemClient *commonClient.SystemClient) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger())

	// 注意：CORS 由 Gateway 统一处理，此处无需设置 CORS 中间件

	if engineService == nil || scanService == nil {
		panic("engineService and scanService must be provided to SetupRouter")
	}

	// 创建Handler
	handler := NewHandler(engineService, scanService, taskService)
	assetDiscHandler := newAssetDiscoverableHandler(db)

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})

	// API路由组（需要认证）
	api := router.Group("/api/meta")
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

		// Schema相关
		api.GET("/engines/:engine_id/schemas", handler.GetSchemas)
		api.GET("/engines/:engine_id/schemas/available", handler.ListAvailableSchemas)
		api.GET("/engines/:engine_id/storage/nodes", handler.ListObjectStorageNodes)

		// 扫描相关
		api.POST("/scan/auto", handler.AutoScan)
		api.POST("/scan/engine", handler.ScanEngine)
		api.POST("/scan/run/manual", handler.CreateManualScanRun)
		api.GET("/scan/runs", handler.ListScanRuns)
		api.GET("/scan/runs/:run_id", handler.GetScanRun)
		api.POST("/scan/runs/:run_id/cancel", handler.CancelScanRun)
		api.GET("/scan/tasks", handler.ListScanTasks)
		api.POST("/scan/tasks", handler.CreateScanTask)
		api.PUT("/scan/tasks/:task_id", handler.UpdateScanTask)
		api.DELETE("/scan/tasks/:task_id", handler.DeleteScanTask)
		api.POST("/scan/tasks/:task_id/trigger", handler.TriggerScanTask)

		// 元数据相关
		api.GET("/metadata/object", handler.GetObjectMetadata)
		api.POST("/metadata/extract", handler.ExtractObjectMetadata)
		api.GET("/metadata/tables", handler.GetTables)
		api.GET("/metadata/fields", handler.GetTableFields)

		// 新增：用于 Manager 模块的元数据查询接口
		api.GET("/engines/:engine_id/tree", handler.GetMetadataTree)
		api.GET("/nodes/:node_id", handler.GetMetaNodeByID)
		api.GET("/nodes/:node_id/children", handler.GetNodeChildren)
		api.GET("/nodes/:node_id/items", handler.GetNodeItems)
		api.GET("/nodes/by-path", handler.QueryNodeByPath)
		api.GET("/items/by-path", handler.QueryItemByPath)
		api.GET("/metadata/tables/spatial", handler.GetTableSpatialMetadata)

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
