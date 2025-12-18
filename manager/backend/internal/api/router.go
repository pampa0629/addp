package api

import (
	"time"

	commonClient "github.com/addp/common/client"
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
	resourceService *service.ResourceService,
	metadataService *service.MetadataService,
	searchService *service.FullTextSearchService,
	historyService *service.SearchHistoryService,
	unifiedMVTService *service.UnifiedMVTService,
	quickViewService *service.QuickViewService,
	resourceRepo *repository.ResourceRepository,
	metadataRepo *repository.MetadataRepository,
	systemClient *commonClient.SystemClient,
	cacheManager *service.CacheManager,
	redisClient *redis.Client,
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

	// 创建算子Handler (需要在API路由组之前创建,以便在API中使用)
	operatorService := service.NewOperatorService(cacheManager)
	operatorHandler := NewOperatorHandler(operatorService)

	// API 路由组
	api := router.Group("/api")
	// 使用 Redis 缓存中间件 (TTL: 5分钟, 减少 System 调用 90%)
	if redisClient != nil {
		api.Use(auth.CachedSystemAuthMiddleware(cfg.SystemServiceURL, redisClient, 5*time.Minute))
	} else {
		// Fallback: 无缓存模式
		api.Use(auth.SystemAuthMiddleware(cfg.SystemServiceURL))
	}
	{
		// 算子API (统一算子接口)
		operators := api.Group("/operators")
		{
			operators.GET("", operatorHandler.ListOperators)              // 获取算子列表
			operators.POST("/:name/execute", operatorHandler.ExecuteOperator) // 执行算子
		}

		configGroup := api.Group("/config")
		{
			configHandler := NewConfigHandler(cfg)
			configGroup.GET("/map", configHandler.GetMapConfig)
		}

		// 数据探查
		explorer := api.Group("/data-explorer")
		{
			handler := NewDataExplorerHandler(metadataService)
			explorer.GET("/tree", handler.GetTree)
			explorer.GET("/resources", handler.ListResources)
			explorer.GET("/resources/:id/tree", handler.GetResourceTree)
			explorer.POST("/resources/:id/refresh", handler.RefreshNode)
			explorer.GET("/preview", handler.PreviewTable)
			explorer.GET("/video-stream", handler.VideoStream)
		}

		// 资源管理
		resources := api.Group("/resources")
		{
			resourceHandler := NewResourceHandler(resourceService)
			resources.GET("", resourceHandler.List)
			resources.GET("/:id", resourceHandler.GetByID)

			// 元数据扫描和管理
			metadataHandler := NewMetadataHandler(metadataService)
			resources.POST("/:id/scan", metadataHandler.ScanResource)
			resources.GET("/:id/scan-tasks", metadataHandler.ListScanTasks)
			resources.POST("/:id/scan-tasks", metadataHandler.CreateScanTask)
			resources.PUT("/:id/scan-tasks/:task_id", metadataHandler.UpdateScanTask)
			resources.DELETE("/:id/scan-tasks/:task_id", metadataHandler.DeleteScanTask)
			resources.POST("/:id/scan-tasks/:task_id/trigger", metadataHandler.TriggerScanTask)
			resources.GET("/:id/scan-runs", metadataHandler.ListScanRuns)
			resources.GET("/:id/scan-runs/:run_id", metadataHandler.GetScanRun)
			resources.POST("/:id/scan-runs/manual", metadataHandler.CreateManualScanRun)
			resources.GET("/:id/tables", metadataHandler.GetTables)

			// 要素查询（用于表格与地图关联）
			featureHandler := NewFeatureHandler(resourceRepo, metadataRepo)
			resources.GET("/:id/features/:feature_id/centroid", featureHandler.GetFeatureCentroid)
		}

		// 表管理
		tables := api.Group("/tables")
		{
			metadataHandler := NewMetadataHandler(metadataService)
			tables.POST("/:id/manage", metadataHandler.ManageTable)
			tables.POST("/:id/unmanage", metadataHandler.UnmanageTable)
		}

		searchGroup := api.Group("/search")
		{
			handler := NewSearchHandler(searchService, historyService)
			searchGroup.GET("/fulltext", handler.FullTextSearch)
			searchGroup.GET("/history", handler.ListHistory)
			searchGroup.DELETE("/history/:id", handler.DeleteHistoryItem)
			searchGroup.DELETE("/history", handler.ClearHistory)
		}

		// 瓦片配置 API（获取 MinZoom/MaxZoom）
		// 注意：必须在 tiles 路由之前注册，避免路由冲突
		tileConfigHandler := NewTileConfigHandler(quickViewService, systemClient, cfg)
		resources.GET("/:id/spatial/:schema/:table/tile-config", tileConfigHandler.GetTileConfig)

		// 资源下的空间瓦片服务（统一 MVT API，RESTful 风格）
		// GET /api/resources/{id}/spatial/tiles/{schema}/{table}/{z}/{x}/{y}
		// 内部自动处理：内存 LRU → Redis → MinIO → 实时 PG 生成
		resources.GET("/:id/spatial/tiles/:schema/:table/:z/:x/:y", func(c *gin.Context) {
			unifiedTilesHandler := NewUnifiedTilesHandler(unifiedMVTService)
			unifiedTilesHandler.GetTile(c)
		})

		// Pre-Cache API（预缓存 - 推荐使用）
		// Quick View API（快显 - 保留作为别名，向后兼容）
		quickViewHandler := NewQuickViewHandler(quickViewService)

		// 新路由：pre-cache（推荐）
		resources.POST("/:id/spatial/:schema/:table/pre-cache", quickViewHandler.TriggerQuickView)
		resources.GET("/:id/spatial/:schema/:table/pre-cache/status", quickViewHandler.GetQuickViewStatus)
		resources.DELETE("/:id/spatial/:schema/:table/pre-cache", quickViewHandler.ClearQuickView)

		// 旧路由：quick-view（别名，向后兼容）
		resources.POST("/:id/spatial/:schema/:table/quick-view", quickViewHandler.TriggerQuickView)
		resources.GET("/:id/spatial/:schema/:table/quick-view/status", quickViewHandler.GetQuickViewStatus)
		resources.DELETE("/:id/spatial/:schema/:table/quick-view", quickViewHandler.ClearQuickView)
	}

	// Pre-Cache 任务列表和统计（全局）
	// 新路由（推荐）
	api.GET("/pre-cache/tasks", func(c *gin.Context) {
		quickViewHandler := NewQuickViewHandler(quickViewService)
		quickViewHandler.ListQuickViewTasks(c)
	})
	api.GET("/pre-cache/statistics", func(c *gin.Context) {
		quickViewHandler := NewQuickViewHandler(quickViewService)
		quickViewHandler.GetStatistics(c)
	})

	// 旧路由（别名，向后兼容）
	api.GET("/quick-view/tasks", func(c *gin.Context) {
		quickViewHandler := NewQuickViewHandler(quickViewService)
		quickViewHandler.ListQuickViewTasks(c)
	})
	api.GET("/quick-view/list", func(c *gin.Context) {
		quickViewHandler := NewQuickViewHandler(quickViewService)
		quickViewHandler.ListQuickViewTasks(c)
	})
	api.GET("/quick-view/statistics", func(c *gin.Context) {
		quickViewHandler := NewQuickViewHandler(quickViewService)
		quickViewHandler.GetStatistics(c)
	})

	return router
}
