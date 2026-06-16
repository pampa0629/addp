package api

import (
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/middleware/audit"
	"github.com/addp/common/middleware/auth"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/manager/docs"
	_ "github.com/addp/manager/i18n"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/preview"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
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
	taskProviderHandler *TaskProviderHandler,
	importHandler *ImportHandler,
) *gin.Engine {
	router := gin.Default()

	// i18n 中间件（解析 Accept-Language 请求头）
	router.Use(i18nmiddleware.I18nMiddleware())

	// Swagger 文档
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 注意：CORS 由 Gateway 统一处理，此处无需设置 CORS 中间件

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

	// API 路由组
	api := router.Group("/api/v1/manager")
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
		// 向量化 API：结果 artifact state 与一次性 execution
		embeddingHandler := NewEmbeddingHandler(embeddingService)
		api.POST("/embedding_executions", embeddingHandler.CreateEmbeddingExecution)
		api.GET("/embeddings", embeddingHandler.ListEmbeddings)
		api.DELETE("/embeddings/:id", embeddingHandler.DeleteEmbedding)
		api.GET("/items/:item_id/embedding", embeddingHandler.GetItemEmbedding)

		// ===== 标准 TaskProvider API =====
		// GET    /api/manager/tasks                       → 任务列表
		// GET    /api/manager/tasks/:task_type/:id        → 任务详情
		// POST   /api/manager/tasks/:task_type/:id/execute → 触发执行
		// GET    /api/manager/executions/:execution_id    → 执行状态
		api.GET("/tasks", taskProviderHandler.ListTasks)
		api.GET("/tasks/:task_type/:id", taskProviderHandler.TaskDetail)
		api.POST("/tasks/:task_type/:id/execute", taskProviderHandler.TaskExecute)
		api.GET("/executions/:execution_id", taskProviderHandler.ExecutionStatus)

		// TileCacheTask CRUD
		tileCacheTasksGroup := api.Group("/tile_cache_tasks")
		{
			tileCacheTasksGroup.GET("", taskProviderHandler.ListTileCacheTasks)
			tileCacheTasksGroup.POST("", taskProviderHandler.CreateTileCacheTask)
			tileCacheTasksGroup.GET("/:id", taskProviderHandler.GetTileCacheTask)
			tileCacheTasksGroup.PUT("/:id", taskProviderHandler.UpdateTileCacheTask)
			tileCacheTasksGroup.DELETE("/:id", taskProviderHandler.DeleteTileCacheTask)
		}
		tileCacheGroup := api.Group("/tile_cache")
		{
			tileCacheGroup.GET("", taskProviderHandler.ListTileCaches)
			tileCacheGroup.GET("/:id", taskProviderHandler.GetTileCache)
			tileCacheGroup.DELETE("/:id", taskProviderHandler.DeleteTileCache)
		}
		quickViewOptimizationTasksGroup := api.Group("/quick_view_optimization_tasks")
		{
			quickViewOptimizationTasksGroup.GET("", taskProviderHandler.ListQuickViewOptimizationTasks)
			quickViewOptimizationTasksGroup.POST("", taskProviderHandler.CreateQuickViewOptimizationTask)
			quickViewOptimizationTasksGroup.GET("/:id", taskProviderHandler.GetQuickViewOptimizationTask)
			quickViewOptimizationTasksGroup.PUT("/:id", taskProviderHandler.UpdateQuickViewOptimizationTask)
			quickViewOptimizationTasksGroup.DELETE("/:id", taskProviderHandler.DeleteQuickViewOptimizationTask)
		}
		quickViewOptimizationGroup := api.Group("/quick_view_optimization")
		{
			quickViewOptimizationGroup.GET("", taskProviderHandler.ListQuickViewOptimizations)
			quickViewOptimizationGroup.GET("/:id", taskProviderHandler.GetQuickViewOptimization)
			quickViewOptimizationGroup.DELETE("/:id", taskProviderHandler.DeleteQuickViewOptimization)
		}

		// EmbeddingTask CRUD
		embeddingTaskDefGroup := api.Group("/embedding_tasks")
		{
			embeddingTaskDefGroup.GET("", taskProviderHandler.ListEmbeddingTasks)
			embeddingTaskDefGroup.POST("", taskProviderHandler.CreateEmbeddingTask)
			embeddingTaskDefGroup.GET("/:id", func(c *gin.Context) {
				c.Params = append(c.Params, gin.Param{Key: "task_type", Value: "embedding"})
				taskProviderHandler.TaskDetail(c)
			})
			embeddingTaskDefGroup.PUT("/:id", taskProviderHandler.UpdateEmbeddingTask)
			embeddingTaskDefGroup.DELETE("/:id", taskProviderHandler.DeleteEmbeddingTask)
		}

		configGroup := api.Group("/config")
		{
			configHandler := NewConfigHandler(cfg)
			configGroup.GET("/map", configHandler.GetMapConfig)
		}

		// 数据导入 API
		api.POST("/import", importHandler.ImportData)

		// 数据探查 API（Manager 核心功能，直接挂在 /api/manager 下）
		previewRegistry := metadataService.PreviewRegistry()
		previewResolver := preview.NewPreviewResolver(previewRegistry, systemClient, metaClient)
		explorerService := service.NewExplorerService(systemClient, metaClient, previewResolver)
		explorerHandler := NewExplorerHandler(explorerService, previewResolver, metadataService)
		metadataHandler := NewMetadataHandler(metadataService)

		api.GET("/engines", explorerHandler.ListEngines) // 获取可用引擎列表（只读）
		api.GET("/tree/:engine_id", explorerHandler.GetTree)
		api.GET("/tree/:engine_id/node", explorerHandler.GetNodeChildren) // 增量加载子节点
		api.GET("/tree/:engine_id/search", explorerHandler.SearchNodes)   // 搜索节点
		api.POST("/tree/:engine_id/refresh", explorerHandler.RefreshNode)
		api.POST("/engines/:id/items/refresh", metadataHandler.RefreshItem)
		api.GET("/preview", explorerHandler.Preview)
		api.GET("/storage-download", explorerHandler.StorageDownload)
		api.GET("/storage-stream", explorerHandler.StorageStream)

		// ============================================================
		// 空间数据服务路由组
		// 注意：Manager 不负责引擎管理（创建、更新、删除），仅提供数据访问服务
		// 引擎管理由 System 模块负责，Manager 通过 SystemClient 查询引擎信息
		// ============================================================
		engines := api.Group("/engines")
		{
			// 要素查询（用于表格与地图关联）
			featureHandler := NewFeatureHandler(systemClient, metadataRepo, quickViewService)
			engines.GET("/:id/spatial/features/:feature_id/centroid", featureHandler.GetFeatureCentroid)
			engines.GET("/:id/spatial/features/:feature_id/geometry", featureHandler.GetFeatureGeometry)
		}

		searchGroup := api.Group("/search")
		{
			handler := NewSearchHandler(searchService, historyService)
			searchGroup.GET("", handler.Search) // 混合检索（全文 + 向量）
			searchGroup.GET("/history", handler.ListHistory)
			searchGroup.DELETE("/history/:id", handler.DeleteHistoryItem)
			searchGroup.DELETE("/history", handler.ClearHistory)
		}

		// Quick View API：统一 ResourceLocator 入口
		quickViewHandler := NewQuickViewHandler(quickViewService, previewResolver, unifiedMVTService, redisClient)
		api.GET("/quick-view/capability", quickViewHandler.GetQuickViewCapabilityByLocator)
		api.GET("/quick-view/geojson", quickViewHandler.GetQuickViewGeoJSONByLocator)
		api.GET("/quick-view/tiles/:z/:x/:y.mvt", quickViewHandler.GetQuickViewTileByLocator)
		api.PATCH("/quick-view/preferred-mode", quickViewHandler.UpdatePreferredModeByLocator)
	}

	return router
}
