package api

import (
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/middleware/audit"
	"github.com/addp/common/middleware/auth"
	"github.com/addp/common/middleware/cors"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func SetupRouter(
	cfg *config.Config,
	metadataService *service.MetadataService,
	searchService *service.HybridSearchService,
	historyService *service.SearchHistoryService,
	unifiedMVTService *service.UnifiedMVTService,
	quickViewService *service.QuickViewService,
	metadataRepo *repository.MetadataRepository,
	systemClient *commonClient.SystemClient,
	metaClient *commonClient.MetaClient,
	cacheManager *service.CacheManager,
	redisClient *redis.Client,
	embeddingService *service.EmbeddingService,
) *gin.Engine {
	router := gin.Default()

	// CORS
	router.Use(cors.CORS())

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "ok",
			"service": "manager",
		})
	})

	// 根路由
	router.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Manager 数据管理服务",
			"version": "1.0.0",
		})
	})

	// 创建任务追踪器（用于向量化任务状态管理）
	taskTracker := service.NewTaskTracker()

	// API 路由组
	api := router.Group("/api/manager")
	// 使用 Redis 缓存中间件 (TTL: 5分钟, 减少 System 调用 90%)
	if redisClient != nil {
		api.Use(auth.CachedSystemAuthMiddleware(cfg.SystemServiceURL, redisClient, 5*time.Minute))
	} else {
		// Fallback: 无缓存模式
		api.Use(auth.SystemAuthMiddleware(cfg.SystemServiceURL))
	}
	// 审计日志中间件（记录到 System 模块）
	if systemClient != nil {
		api.Use(audit.AuditMiddleware("manager", systemClient))
	}
	{
		// 向量化 API
		embeddingHandler := NewEmbeddingHandler(embeddingService, taskTracker)
		taskHandler := NewEmbeddingTaskHandler(embeddingService)

		api.POST("/embedding", embeddingHandler.CreateEmbedding) // 创建向量化任务

		// 向量化任务 API（RESTful 风格）
		embeddingTasksGroup := api.Group("/embedding/tasks")
		{
			embeddingTasksGroup.GET("", taskHandler.ListTasks)                            // 查询任务列表
			embeddingTasksGroup.GET("/:task_id", embeddingHandler.GetEmbeddingTaskStatus) // 查询任务状态（内存）
		}

		configGroup := api.Group("/config")
		{
			configHandler := NewConfigHandler(cfg)
			configGroup.GET("/map", configHandler.GetMapConfig)
		}

		// 数据探查 API（Manager 核心功能，直接挂在 /api/manager 下）
		engineConnector := service.NewEngineConnector(systemClient)
		previewRegistry := metadataService.PreviewRegistry()
		previewResolver := service.NewPreviewResolver(previewRegistry, systemClient, metaClient, engineConnector)
		explorerService := service.NewExplorerService(systemClient, metaClient, previewResolver)
		explorerHandler := NewExplorerHandler(explorerService, previewResolver, metadataService)

		api.GET("/engines", explorerHandler.ListEngines)       // 获取可用引擎列表（只读）
		api.GET("/tree/:engine_id", explorerHandler.GetTree)
		api.POST("/tree/:engine_id/refresh", explorerHandler.RefreshNode)
		api.GET("/preview", explorerHandler.Preview)
		api.GET("/video-stream", explorerHandler.VideoStream)

		// ============================================================
		// 空间数据服务路由组
		// 注意：Manager 不负责引擎管理（创建、更新、删除），仅提供数据访问服务
		// 引擎管理由 System 模块负责，Manager 通过 SystemClient 查询引擎信息
		// ============================================================
		engines := api.Group("/engines")
		{
			// 要素查询（用于表格与地图关联）
			featureHandler := NewFeatureHandler(systemClient, metadataRepo)
			engines.GET("/:id/spatial/features/:feature_id/centroid", featureHandler.GetFeatureCentroid)
			engines.GET("/:id/spatial/features/:feature_id/geometry", featureHandler.GetFeatureGeometry)
		}

		searchGroup := api.Group("/search")
		{
			handler := NewSearchHandler(searchService, historyService)
			searchGroup.GET("", handler.Search)  // 混合检索（全文 + 向量）
			searchGroup.GET("/history", handler.ListHistory)
			searchGroup.DELETE("/history/:id", handler.DeleteHistoryItem)
			searchGroup.DELETE("/history", handler.ClearHistory)
		}

		// 瓦片配置 API（获取 MinZoom/MaxZoom）
		// 注意：必须在 tiles 路由之前注册，避免路由冲突
		tileConfigHandler := NewTileConfigHandler(quickViewService, systemClient, cfg)
		engines.GET("/:id/spatial/:schema/:table/tile-config", tileConfigHandler.GetTileConfig)

		// 引擎下的空间瓦片服务（统一 MVT API，RESTful 风格）
		// GET /api/engines/{id}/spatial/tiles/{schema}/{table}/{z}/{x}/{y}
		// 内部自动处理：内存 LRU → Redis → MinIO → 实时 PG 生成
		engines.GET("/:id/spatial/tiles/:schema/:table/:z/:x/:y", func(c *gin.Context) {
			unifiedTilesHandler := NewUnifiedTilesHandler(unifiedMVTService)
			unifiedTilesHandler.GetTile(c)
		})

		// Pre-Cache API（预缓存 - 推荐使用）
		// Quick View API（快显）
		quickViewHandler := NewQuickViewHandler(quickViewService, redisClient)

		// GeoJSON API（轻量级几何数据获取）
		geojsonHandler := NewGeoJSONHandler(systemClient)
		engines.GET("/:id/spatial/:schema/:table/geojson", geojsonHandler.GetGeoJSON)
		engines.GET("/:id/spatial/:schema/:table/geojson/metadata", geojsonHandler.GetGeoJSONMetadata)

		// Pre-Cache 路由
		engines.POST("/:id/spatial/:schema/:table/pre-cache", quickViewHandler.TriggerQuickView)
		engines.GET("/:id/spatial/:schema/:table/pre-cache/status", quickViewHandler.GetQuickViewStatus)
		engines.PATCH("/:id/spatial/:schema/:table/pre-cache/mode", quickViewHandler.UpdatePreferredMode)
		engines.POST("/:id/spatial/:schema/:table/pre-cache/cancel", quickViewHandler.CancelQuickView)
		engines.POST("/:id/spatial/:schema/:table/pre-cache/resume", quickViewHandler.ResumeQuickView)
		engines.DELETE("/:id/spatial/:schema/:table/pre-cache", quickViewHandler.ClearQuickView)
	}

	// Pre-Cache 任务列表和统计（全局）
	api.GET("/pre-cache/tasks", func(c *gin.Context) {
		quickViewHandler := NewQuickViewHandler(quickViewService, redisClient)
		quickViewHandler.ListQuickViewTasks(c)
	})
	api.GET("/pre-cache/statistics", func(c *gin.Context) {
		quickViewHandler := NewQuickViewHandler(quickViewService, redisClient)
		quickViewHandler.GetStatistics(c)
	})

	return router
}
