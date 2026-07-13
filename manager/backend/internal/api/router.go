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
	spatialPreviewService *service.SpatialPreviewService,
	rasterCOGRepo *repository.RasterCOGRepository,
	taskProviderHandler *TaskProviderHandler,
	importHandler *ImportHandler,
	uploadHandler *UploadHandler,
	resourceActionHandler *ResourceActionHandler,
	exportHandler *ExportHandler,
	rasterMosaicTileHandler *RasterMosaicTileHandler,
	model3DGLBHandler *Model3DGLBHandler,
	gaussianSplatKSplatHandler *GaussianSplatKSplatHandler,
	pointCloudCOPCHandler *PointCloudCOPCHandler,
	cadPreviewHandler *CADPreviewHandler,
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

	internal := router.Group("/api/v1/manager/internal")
	internal.Use(managerInternalAPIKeyMiddleware(cfg))
	{
		if taskProviderHandler != nil {
			internal.POST("/executions/:execution_id/events", taskProviderHandler.RecordManagerExecutionProgressEvent)
		}
	}

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
		// GET    /api/v1/manager/tasks                       → 任务列表
		// GET    /api/v1/manager/tasks/:task_type/:id        → 任务详情
		// POST   /api/v1/manager/tasks/:task_type/:id/execute → 触发执行
		// GET    /api/v1/manager/executions/:execution_id    → 执行状态
		api.GET("/tasks", taskProviderHandler.ListTasks)
		api.GET("/tasks/:task_type/:id", taskProviderHandler.TaskDetail)
		api.POST("/tasks/:task_type/:id/execute", taskProviderHandler.TaskExecute)
		api.GET("/executions/:execution_id", taskProviderHandler.ExecutionStatus)

		// TileCacheTask CRUD
		tileCacheTasksGroup := api.Group("/vector_tile_cache_tasks")
		{
			tileCacheTasksGroup.GET("", taskProviderHandler.ListTileCacheTasks)
			tileCacheTasksGroup.POST("", taskProviderHandler.CreateTileCacheTask)
			tileCacheTasksGroup.GET("/:id", taskProviderHandler.GetTileCacheTask)
			tileCacheTasksGroup.PUT("/:id", taskProviderHandler.UpdateTileCacheTask)
			tileCacheTasksGroup.DELETE("/:id", taskProviderHandler.DeleteTileCacheTask)
		}
		tileCacheGroup := api.Group("/vector_tile_cache")
		{
			tileCacheGroup.GET("", taskProviderHandler.ListTileCaches)
			tileCacheGroup.GET("/:id", taskProviderHandler.GetTileCache)
			tileCacheGroup.DELETE("/:id", taskProviderHandler.DeleteTileCache)
		}
		rasterCOGHandler := NewRasterCOGHandler(rasterCOGRepo, spatialPreviewService)
		rasterCOGTasksGroup := api.Group("/raster_cog_tasks")
		{
			rasterCOGTasksGroup.GET("", taskProviderHandler.ListRasterCOGTasks)
			rasterCOGTasksGroup.POST("", taskProviderHandler.CreateRasterCOGTask)
			rasterCOGTasksGroup.GET("/:id", taskProviderHandler.GetRasterCOGTask)
			rasterCOGTasksGroup.PUT("/:id", taskProviderHandler.UpdateRasterCOGTask)
			rasterCOGTasksGroup.DELETE("/:id", taskProviderHandler.DeleteRasterCOGTask)
		}
		rasterMosaicTasksGroup := api.Group("/raster_mosaic_tasks")
		{
			rasterMosaicTasksGroup.GET("", taskProviderHandler.ListRasterMosaicTasks)
			rasterMosaicTasksGroup.POST("", taskProviderHandler.CreateRasterMosaicTask)
			rasterMosaicTasksGroup.GET("/:id", taskProviderHandler.GetRasterMosaicTask)
			rasterMosaicTasksGroup.PUT("/:id", taskProviderHandler.UpdateRasterMosaicTask)
			rasterMosaicTasksGroup.DELETE("/:id", taskProviderHandler.DeleteRasterMosaicTask)
		}
		model3DTilesTasksGroup := api.Group("/model_3d_tiles_tasks")
		{
			model3DTilesTasksGroup.GET("", taskProviderHandler.ListModel3DTilesTasks)
			model3DTilesTasksGroup.POST("", taskProviderHandler.CreateModel3DTilesTask)
			model3DTilesTasksGroup.GET("/:id", taskProviderHandler.GetModel3DTilesTask)
			model3DTilesTasksGroup.PUT("/:id", taskProviderHandler.UpdateModel3DTilesTask)
			model3DTilesTasksGroup.DELETE("/:id", taskProviderHandler.DeleteModel3DTilesTask)
		}
		model3DGLBTasksGroup := api.Group("/model_3d_glb_tasks")
		{
			model3DGLBTasksGroup.GET("", taskProviderHandler.ListModel3DGLBTasks)
			model3DGLBTasksGroup.POST("", taskProviderHandler.CreateModel3DGLBTask)
			model3DGLBTasksGroup.GET("/:id", taskProviderHandler.GetModel3DGLBTask)
			model3DGLBTasksGroup.PUT("/:id", taskProviderHandler.UpdateModel3DGLBTask)
			model3DGLBTasksGroup.DELETE("/:id", taskProviderHandler.DeleteModel3DGLBTask)
		}
		gaussianSplatKSplatTasksGroup := api.Group("/gaussian_splat_ksplat_tasks")
		{
			gaussianSplatKSplatTasksGroup.GET("", taskProviderHandler.ListGaussianSplatKSplatTasks)
			gaussianSplatKSplatTasksGroup.POST("", taskProviderHandler.CreateGaussianSplatKSplatTask)
			gaussianSplatKSplatTasksGroup.GET("/:id", taskProviderHandler.GetGaussianSplatKSplatTask)
			gaussianSplatKSplatTasksGroup.PUT("/:id", taskProviderHandler.UpdateGaussianSplatKSplatTask)
			gaussianSplatKSplatTasksGroup.DELETE("/:id", taskProviderHandler.DeleteGaussianSplatKSplatTask)
		}
		pointCloudCOPCTasksGroup := api.Group("/point_cloud_copc_tasks")
		{
			pointCloudCOPCTasksGroup.GET("", taskProviderHandler.ListPointCloudCOPCTasks)
			pointCloudCOPCTasksGroup.POST("", taskProviderHandler.CreatePointCloudCOPCTask)
			pointCloudCOPCTasksGroup.GET("/:id", taskProviderHandler.GetPointCloudCOPCTask)
			pointCloudCOPCTasksGroup.PUT("/:id", taskProviderHandler.UpdatePointCloudCOPCTask)
			pointCloudCOPCTasksGroup.DELETE("/:id", taskProviderHandler.DeletePointCloudCOPCTask)
		}
		if rasterMosaicTileHandler != nil {
			rasterMosaicTilesGroup := api.Group("/raster_mosaic/tiles")
			{
				rasterMosaicTilesGroup.GET("/:z/:x/:y", rasterMosaicTileHandler.GetRasterMosaicTile)
			}
		}
		rasterCOGsGroup := api.Group("/raster_cog")
		{
			rasterCOGsGroup.GET("", taskProviderHandler.ListRasterCOGs)
			rasterCOGsGroup.GET("/:id", taskProviderHandler.GetRasterCOG)
			rasterCOGsGroup.DELETE("/:id", taskProviderHandler.DeleteRasterCOG)
			rasterCOGsGroup.GET("/:id/content", rasterCOGHandler.GetRasterCOGContent)
		}
		model3DGLBsGroup := api.Group("/model_3d_glb")
		{
			model3DGLBsGroup.GET("", taskProviderHandler.ListModel3DGLBs)
			model3DGLBsGroup.GET("/:id", taskProviderHandler.GetModel3DGLB)
			model3DGLBsGroup.DELETE("/:id", taskProviderHandler.DeleteModel3DGLB)
			if model3DGLBHandler != nil {
				model3DGLBsGroup.GET("/:id/content", model3DGLBHandler.GetModel3DGLBContent)
			}
		}
		gaussianSplatKSplatsGroup := api.Group("/gaussian_splat_ksplat")
		{
			gaussianSplatKSplatsGroup.GET("", taskProviderHandler.ListGaussianSplatKSplats)
			gaussianSplatKSplatsGroup.GET("/:id", taskProviderHandler.GetGaussianSplatKSplat)
			gaussianSplatKSplatsGroup.DELETE("/:id", taskProviderHandler.DeleteGaussianSplatKSplat)
			if gaussianSplatKSplatHandler != nil {
				gaussianSplatKSplatsGroup.GET("/:id/inspect", gaussianSplatKSplatHandler.InspectGaussianSplatKSplat)
				gaussianSplatKSplatsGroup.GET("/:id/content", gaussianSplatKSplatHandler.GetGaussianSplatKSplatContent)
			}
		}
		pointCloudCOPCsGroup := api.Group("/point_cloud_copc")
		{
			pointCloudCOPCsGroup.GET("", taskProviderHandler.ListPointCloudCOPCs)
			pointCloudCOPCsGroup.GET("/:id", taskProviderHandler.GetPointCloudCOPC)
			pointCloudCOPCsGroup.DELETE("/:id", taskProviderHandler.DeletePointCloudCOPC)
			if pointCloudCOPCHandler != nil {
				pointCloudCOPCsGroup.GET("/:id/content", pointCloudCOPCHandler.GetPointCloudCOPCContent)
			}
		}
		if cadPreviewHandler != nil {
			cadPreviewTasks := api.Group("/cad-preview-tasks")
			{
				cadPreviewTasks.GET("", cadPreviewHandler.ListTasks)
				cadPreviewTasks.POST("", cadPreviewHandler.CreateTask)
				cadPreviewTasks.GET("/:id", cadPreviewHandler.GetTask)
				cadPreviewTasks.PUT("/:id", cadPreviewHandler.UpdateTask)
				cadPreviewTasks.DELETE("/:id", cadPreviewHandler.DeleteTask)
			}
			cadPreviews := api.Group("/cad-previews")
			{
				cadPreviews.GET("", cadPreviewHandler.ListResults)
				cadPreviews.GET("/:id", cadPreviewHandler.GetResult)
				cadPreviews.DELETE("/:id", cadPreviewHandler.DeleteResult)
				cadPreviews.GET("/:id/manifest", cadPreviewHandler.GetManifest)
				cadPreviews.GET("/:id/tiles/:z/:x/:y", cadPreviewHandler.GetTile)
			}
		}
		vectorMaterializedViewTasksGroup := api.Group("/vector_materialized_view_tasks")
		{
			vectorMaterializedViewTasksGroup.GET("", taskProviderHandler.ListVectorMaterializedViewTasks)
			vectorMaterializedViewTasksGroup.POST("", taskProviderHandler.CreateVectorMaterializedViewTask)
			vectorMaterializedViewTasksGroup.GET("/:id", taskProviderHandler.GetVectorMaterializedViewTask)
			vectorMaterializedViewTasksGroup.PUT("/:id", taskProviderHandler.UpdateVectorMaterializedViewTask)
			vectorMaterializedViewTasksGroup.DELETE("/:id", taskProviderHandler.DeleteVectorMaterializedViewTask)
		}
		vectorMaterializedViewGroup := api.Group("/vector_materialized_view")
		{
			vectorMaterializedViewGroup.GET("", taskProviderHandler.ListVectorMaterializedViews)
			vectorMaterializedViewGroup.GET("/:id", taskProviderHandler.GetVectorMaterializedView)
			vectorMaterializedViewGroup.DELETE("/:id", taskProviderHandler.DeleteVectorMaterializedView)
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

		// Manager 用户动作 API
		api.GET("/resource-actions", resourceActionHandler.GetResourceActions)
		api.POST("/uploads", uploadHandler.UploadFiles)
		api.POST("/imports", importHandler.ImportData)
		api.POST("/exports", exportHandler.CreateExport)
		api.GET("/exports/:id", exportHandler.GetExport)
		api.GET("/exports/:id/file", exportHandler.DownloadExportFile)

		// 数据探查 API（Manager 核心功能，直接挂在 /api/v1/manager 下）
		previewRegistry := metadataService.PreviewRegistry()
		previewResolver := preview.NewPreviewResolver(previewRegistry, systemClient, metaClient)
		explorerService := service.NewExplorerService(systemClient, metaClient, previewResolver)
		explorerHandler := NewExplorerHandler(explorerService, previewResolver, metadataService)
		metadataHandler := NewMetadataHandler(metadataService)
		downloadHandler := NewDownloadHandler(metadataService)

		api.GET("/engines", explorerHandler.ListEngines) // 获取可用引擎列表（只读）
		api.POST("/engines/:id/items/refresh", metadataHandler.RefreshItem)
		api.GET("/preview", explorerHandler.Preview)
		api.GET("/downloads/file", downloadHandler.DownloadFile)
		api.GET("/storage-stream", explorerHandler.StorageStream)
		api.GET("/storage-assets/:engine_id/*storage_ref", explorerHandler.StorageAsset)

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
		if taskProviderHandler != nil {
			quickViewHandler.SetTileCacheTaskService(taskProviderHandler.tileCacheTaskSvc)
			quickViewHandler.SetArtifactTaskServices(taskProviderHandler.rasterCOGTaskSvc, taskProviderHandler.model3DGLBTaskSvc, taskProviderHandler.gaussianSplatKSplatTaskSvc, taskProviderHandler.pointCloudCOPCTaskSvc, taskProviderHandler.cadPreviewTaskSvc)
		}
		api.GET("/quick-view/capability", quickViewHandler.GetQuickViewCapabilityByLocator)
		api.POST("/quick-view/actions", quickViewHandler.ExecuteQuickViewAction)
		api.GET("/quick-view/flatgeobuf", quickViewHandler.GetQuickViewFlatGeobufByLocator)
		api.GET("/quick-view/geojson", quickViewHandler.GetQuickViewGeoJSONByLocator)
		api.GET("/quick-view/tiles/:z/:x/:y.mvt", quickViewHandler.GetQuickViewTileByLocator)
		api.PATCH("/preview-state/preferred-mode", quickViewHandler.UpdatePreferredModeByLocator)
		api.PATCH("/preview-state/view-state", quickViewHandler.UpdateViewStateByLocator)
	}

	return router
}
