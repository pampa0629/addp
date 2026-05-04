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

	// 创建任务追踪器（用于向量化任务状态管理）
	taskTracker := service.NewTaskTracker()

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
		// 向量化 API（ad-hoc 即时执行）
		embeddingHandler := NewEmbeddingHandler(embeddingService, taskTracker)

		api.POST("/embedding", embeddingHandler.CreateEmbedding) // 创建即时向量化任务

		// 向量化任务实时状态查询（内存中）
		embeddingTasksGroup := api.Group("/embedding/tasks")
		{
			embeddingTasksGroup.GET("/:task_id", embeddingHandler.GetEmbeddingTaskStatus) // 查询任务状态（内存）
		}

		// ===== 标准 TaskProvider API =====
		// GET    /api/manager/tasks                       → 任务列表
		// GET    /api/manager/tasks/:task_type/:id        → 任务详情
		// POST   /api/manager/tasks/:task_type/:id/execute → 触发执行
		// GET    /api/manager/executions/:execution_id    → 执行状态
		// POST   /api/manager/executions/:execution_id/cancel → 取消
		api.GET("/tasks", taskProviderHandler.ListTasks)
		api.GET("/tasks/:task_type/:id", taskProviderHandler.TaskDetail)
		api.POST("/tasks/:task_type/:id/execute", taskProviderHandler.TaskExecute)
		api.GET("/executions/:execution_id", taskProviderHandler.ExecutionStatus)
		api.POST("/executions/:execution_id/cancel", taskProviderHandler.ExecutionCancel)

		// MvtTask CRUD
		mvtTasksGroup := api.Group("/mvt_tasks")
		{
			mvtTasksGroup.GET("", taskProviderHandler.ListTasks) // ?task_type=mvt_generation
			mvtTasksGroup.POST("", taskProviderHandler.CreateMvtTask)
			mvtTasksGroup.GET("/:id", func(c *gin.Context) {
				c.Params = append(c.Params, gin.Param{Key: "task_type", Value: "mvt_generation"})
				taskProviderHandler.TaskDetail(c)
			})
			mvtTasksGroup.PUT("/:id", taskProviderHandler.UpdateMvtTask)
			mvtTasksGroup.DELETE("/:id", taskProviderHandler.DeleteMvtTask)
		}

		// EmbeddingTask CRUD
		embeddingTaskDefGroup := api.Group("/embedding_tasks")
		{
			embeddingTaskDefGroup.GET("", taskProviderHandler.ListTasks) // ?task_type=embedding
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
		engineConnector := service.NewEngineConnector(systemClient)
		previewRegistry := metadataService.PreviewRegistry()
		previewResolver := service.NewPreviewResolver(previewRegistry, systemClient, metaClient, engineConnector)
		explorerService := service.NewExplorerService(systemClient, metaClient, previewResolver)
		explorerHandler := NewExplorerHandler(explorerService, previewResolver, metadataService)

		api.GET("/engines", explorerHandler.ListEngines) // 获取可用引擎列表（只读）
		api.GET("/tree/:engine_id", explorerHandler.GetTree)
		api.GET("/tree/:engine_id/node", explorerHandler.GetNodeChildren) // 增量加载子节点
		api.GET("/tree/:engine_id/search", explorerHandler.SearchNodes)   // 搜索节点
		api.POST("/tree/:engine_id/refresh", explorerHandler.RefreshNode)
		api.GET("/graph-schema/:engine_id", explorerHandler.GetGraphSchema) // 图数据库 Schema（节点标签 + 关系类型）
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
			searchGroup.GET("", handler.Search) // 混合检索（全文 + 向量）
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

		// Quick View API（快显服务：准备 → 预缓存 → MVT启用）
		quickViewHandler := NewQuickViewHandler(quickViewService, redisClient)

		// GeoJSON API（轻量级几何数据获取）
		geojsonHandler := NewGeoJSONHandler(systemClient)
		engines.GET("/:id/spatial/:schema/:table/geojson", geojsonHandler.GetGeoJSON)
		engines.GET("/:id/spatial/:schema/:table/geojson/metadata", geojsonHandler.GetGeoJSONMetadata)

		// Quick View 路由（快显服务：准备 → 预缓存 → MVT启用）
		engines.POST("/:id/spatial/:schema/:table/quick-view/prepare", quickViewHandler.PrepareForCreateMVT)
		engines.POST("/:id/spatial/:schema/:table/quick-view/pre-cache", quickViewHandler.TriggerQuickView)
		engines.GET("/:id/spatial/:schema/:table/quick-view/status", quickViewHandler.GetQuickViewStatus)
		engines.PATCH("/:id/spatial/:schema/:table/quick-view/preferred-mode", quickViewHandler.UpdatePreferredMode)
		engines.POST("/:id/spatial/:schema/:table/quick-view/cancel", quickViewHandler.CancelQuickView)
		engines.POST("/:id/spatial/:schema/:table/quick-view/resume", quickViewHandler.ResumeQuickView)
		engines.DELETE("/:id/spatial/:schema/:table/quick-view", quickViewHandler.ClearQuickView)
		engines.GET("/:id/spatial/:schema/:table/quick-view/check-preparation", quickViewHandler.CheckPreparation)
	}

	// Quick View 任务列表和统计（全局）
	api.GET("/quick-view/tasks", func(c *gin.Context) {
		quickViewHandler := NewQuickViewHandler(quickViewService, redisClient)
		quickViewHandler.ListQuickViewTasks(c)
	})
	api.GET("/quick-view/statistics", func(c *gin.Context) {
		quickViewHandler := NewQuickViewHandler(quickViewService, redisClient)
		quickViewHandler.GetStatistics(c)
	})

	return router
}
