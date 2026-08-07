package api

import (
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/middleware/audit"
	"github.com/addp/common/middleware/auth"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/manager/docs"
	_ "github.com/addp/manager/i18n"
	managerauthorization "github.com/addp/manager/internal/authorization"
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
	systemServiceClient *commonClient.SystemServiceClient,
	metaClient *commonClient.MetaClient,
	cacheManager *service.CacheManager,
	redisClient *redis.Client,
	embeddingService *service.EmbeddingService,
	embeddingConfigurationService *service.EmbeddingConfigurationService,
	inferenceScenarioBindingService *service.InferenceScenarioBindingService,
	quickViewPolicyService *service.QuickViewPolicyService,
	baseMapProviderService *service.BaseMapProviderService,
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
	model3DTilesHandler *Model3DTilesHandler,
	dataProfileHandler *DataProfileHandler,
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

	platform := router.Group("/api/v1/manager")
	platform.Use(
		auth.MustNewMiddleware(auth.MiddlewareConfig{SystemURL: cfg.SystemServiceURL}),
		auth.MustNewContextGuard("platform"),
	)
	if systemClient != nil {
		platform.Use(audit.AuditMiddleware("manager", systemClient))
	}
	if embeddingConfigurationService != nil {
		handler := NewEmbeddingConfigurationHandler(embeddingConfigurationService)
		platform.GET("/settings/embedding", auth.MustNewPermissionGuard(managerauthorization.PermissionManagerConfigurationRead), handler.Get)
		platform.PUT("/settings/embedding", auth.MustNewPermissionGuard(managerauthorization.PermissionManagerConfigurationUpdate), handler.Update)
	}
	if quickViewPolicyService != nil {
		handler := NewQuickViewPolicyHandler(quickViewPolicyService)
		platform.GET("/settings/quick-view-policy", auth.MustNewPermissionGuard(managerauthorization.PermissionManagerConfigurationRead), handler.Get)
		platform.PUT("/settings/quick-view-policy", auth.MustNewPermissionGuard(managerauthorization.PermissionManagerConfigurationUpdate), handler.Update)
	}
	if inferenceScenarioBindingService != nil {
		bindingHandler := NewInferenceScenarioBindingHandler(inferenceScenarioBindingService)
		settings := router.Group("/api/v1/manager")
		settings.Use(auth.MustNewMiddleware(auth.MiddlewareConfig{SystemURL: cfg.SystemServiceURL}))
		settings.GET("/settings/inference-binding", auth.MustNewPermissionGuard(managerauthorization.PermissionManagerConfigurationRead), bindingHandler.Get)
		settings.PUT("/settings/inference-binding", auth.MustNewPermissionGuard(managerauthorization.PermissionManagerConfigurationUpdate), bindingHandler.Update)
	}
	if baseMapProviderService != nil {
		handler := NewBaseMapProviderHandler(baseMapProviderService)
		settings := router.Group("/api/v1/manager")
		settings.Use(auth.MustNewMiddleware(auth.MiddlewareConfig{SystemURL: cfg.SystemServiceURL}))
		settings.GET("/settings/base-map/providers", auth.MustNewPermissionGuard(managerauthorization.PermissionManagerConfigurationRead), handler.List)
		settings.PUT("/settings/base-map/providers", auth.MustNewPermissionGuard(managerauthorization.PermissionManagerConfigurationUpdate), handler.Update)
	}

	// API 路由组
	api := router.Group("/api/v1/manager")
	api.Use(
		auth.MustNewOptionalResourceTicketMiddleware(auth.ResourceTicketMiddlewareConfig{
			SystemURL: cfg.SystemServiceURL, Owner: "manager",
			RequiredPermissions: []string{managerauthorization.PermissionManagerContentRead},
		}, isManagerContentResourceRequest),
		auth.MustNewOptionalResourceTicketMiddleware(auth.ResourceTicketMiddlewareConfig{
			SystemURL: cfg.SystemServiceURL, Owner: "manager",
			RequiredPermissions: []string{managerauthorization.PermissionManagerDerivedArtifactRead},
		}, isManagerDerivedResourceRequest),
		auth.MustNewMiddleware(auth.MiddlewareConfig{SystemURL: cfg.SystemServiceURL}),
		auth.MustNewContextGuard("tenant"),
		auth.MustNewDelegatedPolicyGuard("manager", map[string]auth.DelegatedRoutePolicyEntry{
			"GET /api/v1/manager/search": {
				RequiredScopes:      []string{"data.search"},
				RequiredPermissions: []string{managerauthorization.PermissionManagerSearchExecute},
			},
			"GET /api/v1/manager/preview": {
				RequiredScopes:      []string{"data.preview"},
				RequiredPermissions: []string{managerauthorization.PermissionManagerContentRead},
			},
		}),
	)
	permission := func(keys ...string) gin.HandlerFunc {
		return auth.MustNewPermissionGuard(keys...)
	}
	// 审计日志中间件（记录到 System 模块）
	if systemClient != nil {
		api.Use(audit.AuditMiddleware("manager", systemClient))
	}
	{
		if dataProfileHandler != nil {
			api.GET("/data-profiles/current", permission(managerauthorization.PermissionManagerDataItemRead), dataProfileHandler.GetCurrent)
			api.POST("/data-profile-executions", permission(managerauthorization.PermissionManagerDataProfileExecute), dataProfileHandler.CreateExecution)
		}

		// 向量化 API：结果 artifact state 与一次性 execution
		embeddingHandler := NewEmbeddingHandler(embeddingService)
		api.POST("/embedding_executions", permission(managerauthorization.PermissionManagerDerivedArtifactCreate), embeddingHandler.CreateEmbeddingExecution)
		api.GET("/embeddings", permission(managerauthorization.PermissionManagerDerivedArtifactRead), embeddingHandler.ListEmbeddings)
		api.DELETE("/embeddings/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), embeddingHandler.DeleteEmbedding)
		api.GET("/items/:item_id/embedding", permission(managerauthorization.PermissionManagerDerivedArtifactRead), embeddingHandler.GetItemEmbedding)

		// ===== 标准 TaskProvider API =====
		// GET    /api/v1/manager/tasks                       → 任务列表
		// GET    /api/v1/manager/tasks/:task_type/:id        → 任务详情
		// POST   /api/v1/manager/tasks/:task_type/:id/execute → 触发执行
		// GET    /api/v1/manager/executions/:execution_id    → 执行状态
		api.GET("/tasks", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListTasks)
		api.GET("/tasks/:task_type/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.TaskDetail)
		api.POST("/tasks/:task_type/:id/execute", permission(managerauthorization.PermissionManagerDerivedArtifactCreate), taskProviderHandler.TaskExecute)
		api.GET("/executions/:execution_id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ExecutionStatus)

		// TileCacheTask CRUD
		tileCacheTasksGroup := api.Group("/vector_tile_cache_tasks")
		{
			tileCacheTasksGroup.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListTileCacheTasks)
			tileCacheTasksGroup.POST("", permission(managerauthorization.PermissionManagerDerivedArtifactCreate), taskProviderHandler.CreateTileCacheTask)
			tileCacheTasksGroup.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.GetTileCacheTask)
			tileCacheTasksGroup.PUT("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactUpdate), taskProviderHandler.UpdateTileCacheTask)
			tileCacheTasksGroup.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeleteTileCacheTask)
		}
		tileCacheGroup := api.Group("/vector_tile_cache")
		{
			tileCacheGroup.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListTileCaches)
			tileCacheGroup.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.GetTileCache)
			tileCacheGroup.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeleteTileCache)
		}
		vectorTileSetTasksGroup := api.Group("/vector_tile_set_tasks")
		{
			vectorTileSetTasksGroup.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListVectorTileSetTasks)
			vectorTileSetTasksGroup.POST("", permission(managerauthorization.PermissionManagerDerivedArtifactCreate), taskProviderHandler.CreateVectorTileSetTask)
			vectorTileSetTasksGroup.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.GetVectorTileSetTask)
			vectorTileSetTasksGroup.PUT("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactUpdate), taskProviderHandler.UpdateVectorTileSetTask)
			vectorTileSetTasksGroup.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeleteVectorTileSetTask)
		}
		rasterCOGHandler := NewRasterCOGHandler(rasterCOGRepo, spatialPreviewService)
		rasterCOGTasksGroup := api.Group("/raster_cog_tasks")
		{
			rasterCOGTasksGroup.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListRasterCOGTasks)
			rasterCOGTasksGroup.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.GetRasterCOGTask)
			rasterCOGTasksGroup.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeleteRasterCOGTask)
		}
		rasterMosaicTasksGroup := api.Group("/raster_mosaic_tasks")
		{
			rasterMosaicTasksGroup.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListRasterMosaicTasks)
			rasterMosaicTasksGroup.POST("", permission(managerauthorization.PermissionManagerDerivedArtifactCreate), taskProviderHandler.CreateRasterMosaicTask)
			rasterMosaicTasksGroup.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.GetRasterMosaicTask)
			rasterMosaicTasksGroup.PUT("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactUpdate), taskProviderHandler.UpdateRasterMosaicTask)
			rasterMosaicTasksGroup.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeleteRasterMosaicTask)
		}
		model3DTilesTasksGroup := api.Group("/model3d_tiles_tasks")
		{
			model3DTilesTasksGroup.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListModel3DTilesTasks)
			model3DTilesTasksGroup.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.GetModel3DTilesTask)
			model3DTilesTasksGroup.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeleteModel3DTilesTask)
		}
		model3DTilesAssets := api.Group("/model3d_tiles")
		{
			model3DTilesAssets.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListModel3DTilesResults)
			model3DTilesAssets.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeleteModel3DTilesResult)
			model3DTilesAssets.GET("/:id/assets/*asset_path", permission(managerauthorization.PermissionManagerDerivedArtifactRead), model3DTilesHandler.GetAsset)
		}
		model3DGLBTasksGroup := api.Group("/model_3d_glb_tasks")
		{
			model3DGLBTasksGroup.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListModel3DGLBTasks)
			model3DGLBTasksGroup.POST("", permission(managerauthorization.PermissionManagerDerivedArtifactCreate), taskProviderHandler.CreateModel3DGLBTask)
			model3DGLBTasksGroup.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.GetModel3DGLBTask)
			model3DGLBTasksGroup.PUT("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactUpdate), taskProviderHandler.UpdateModel3DGLBTask)
			model3DGLBTasksGroup.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeleteModel3DGLBTask)
		}
		gaussianSplatKSplatTasksGroup := api.Group("/gaussian_splat_ksplat_tasks")
		{
			gaussianSplatKSplatTasksGroup.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListGaussianSplatKSplatTasks)
			gaussianSplatKSplatTasksGroup.POST("", permission(managerauthorization.PermissionManagerDerivedArtifactCreate), taskProviderHandler.CreateGaussianSplatKSplatTask)
			gaussianSplatKSplatTasksGroup.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.GetGaussianSplatKSplatTask)
			gaussianSplatKSplatTasksGroup.PUT("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactUpdate), taskProviderHandler.UpdateGaussianSplatKSplatTask)
			gaussianSplatKSplatTasksGroup.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeleteGaussianSplatKSplatTask)
		}
		pointCloudCOPCTasksGroup := api.Group("/point_cloud_copc_tasks")
		{
			pointCloudCOPCTasksGroup.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListPointCloudCOPCTasks)
			pointCloudCOPCTasksGroup.POST("", permission(managerauthorization.PermissionManagerDerivedArtifactCreate), taskProviderHandler.CreatePointCloudCOPCTask)
			pointCloudCOPCTasksGroup.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.GetPointCloudCOPCTask)
			pointCloudCOPCTasksGroup.PUT("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactUpdate), taskProviderHandler.UpdatePointCloudCOPCTask)
			pointCloudCOPCTasksGroup.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeletePointCloudCOPCTask)
		}
		if rasterMosaicTileHandler != nil {
			rasterMosaicTilesGroup := api.Group("/raster_mosaic/tiles")
			{
				rasterMosaicTilesGroup.GET("/:z/:x/:y", permission(managerauthorization.PermissionManagerDerivedArtifactRead), rasterMosaicTileHandler.GetRasterMosaicTile)
			}
		}
		rasterCOGsGroup := api.Group("/raster_cog")
		{
			rasterCOGsGroup.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListRasterCOGs)
			rasterCOGsGroup.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.GetRasterCOG)
			rasterCOGsGroup.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeleteRasterCOG)
			rasterCOGsGroup.GET("/:id/content", permission(managerauthorization.PermissionManagerDerivedArtifactRead), rasterCOGHandler.GetRasterCOGContent)
		}
		model3DGLBsGroup := api.Group("/model_3d_glb")
		{
			model3DGLBsGroup.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListModel3DGLBs)
			model3DGLBsGroup.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.GetModel3DGLB)
			model3DGLBsGroup.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeleteModel3DGLB)
			if model3DGLBHandler != nil {
				model3DGLBsGroup.GET("/:id/content", permission(managerauthorization.PermissionManagerDerivedArtifactRead), model3DGLBHandler.GetModel3DGLBContent)
			}
		}
		gaussianSplatKSplatsGroup := api.Group("/gaussian_splat_ksplat")
		{
			gaussianSplatKSplatsGroup.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListGaussianSplatKSplats)
			gaussianSplatKSplatsGroup.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.GetGaussianSplatKSplat)
			gaussianSplatKSplatsGroup.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeleteGaussianSplatKSplat)
			if gaussianSplatKSplatHandler != nil {
				gaussianSplatKSplatsGroup.GET("/:id/inspect", permission(managerauthorization.PermissionManagerDerivedArtifactRead), gaussianSplatKSplatHandler.InspectGaussianSplatKSplat)
				gaussianSplatKSplatsGroup.GET("/:id/content", permission(managerauthorization.PermissionManagerDerivedArtifactRead), gaussianSplatKSplatHandler.GetGaussianSplatKSplatContent)
			}
		}
		pointCloudCOPCsGroup := api.Group("/point_cloud_copc")
		{
			pointCloudCOPCsGroup.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListPointCloudCOPCs)
			pointCloudCOPCsGroup.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.GetPointCloudCOPC)
			pointCloudCOPCsGroup.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeletePointCloudCOPC)
			if pointCloudCOPCHandler != nil {
				pointCloudCOPCsGroup.GET("/:id/content", permission(managerauthorization.PermissionManagerDerivedArtifactRead), pointCloudCOPCHandler.GetPointCloudCOPCContent)
			}
		}
		if cadPreviewHandler != nil {
			cadPreviewTasks := api.Group("/cad-preview-tasks")
			{
				cadPreviewTasks.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), cadPreviewHandler.ListTasks)
				cadPreviewTasks.POST("", permission(managerauthorization.PermissionManagerDerivedArtifactCreate), cadPreviewHandler.CreateTask)
				cadPreviewTasks.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), cadPreviewHandler.GetTask)
				cadPreviewTasks.PUT("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactUpdate), cadPreviewHandler.UpdateTask)
				cadPreviewTasks.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), cadPreviewHandler.DeleteTask)
			}
			cadPreviews := api.Group("/cad-previews")
			{
				cadPreviews.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), cadPreviewHandler.ListResults)
				cadPreviews.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), cadPreviewHandler.GetResult)
				cadPreviews.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), cadPreviewHandler.DeleteResult)
				cadPreviews.GET("/:id/manifest", permission(managerauthorization.PermissionManagerDerivedArtifactRead), cadPreviewHandler.GetManifest)
				cadPreviews.GET("/:id/tiles/:z/:x/:y", permission(managerauthorization.PermissionManagerDerivedArtifactRead), cadPreviewHandler.GetTile)
			}
		}
		vectorMaterializedViewTasksGroup := api.Group("/vector_materialized_view_tasks")
		{
			vectorMaterializedViewTasksGroup.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListVectorMaterializedViewTasks)
			vectorMaterializedViewTasksGroup.POST("", permission(managerauthorization.PermissionManagerDerivedArtifactCreate), taskProviderHandler.CreateVectorMaterializedViewTask)
			vectorMaterializedViewTasksGroup.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.GetVectorMaterializedViewTask)
			vectorMaterializedViewTasksGroup.PUT("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactUpdate), taskProviderHandler.UpdateVectorMaterializedViewTask)
			vectorMaterializedViewTasksGroup.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeleteVectorMaterializedViewTask)
		}
		vectorMaterializedViewGroup := api.Group("/vector_materialized_view")
		{
			vectorMaterializedViewGroup.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListVectorMaterializedViews)
			vectorMaterializedViewGroup.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.GetVectorMaterializedView)
			vectorMaterializedViewGroup.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeleteVectorMaterializedView)
		}

		// EmbeddingTask CRUD
		embeddingTaskDefGroup := api.Group("/embedding_tasks")
		{
			embeddingTaskDefGroup.GET("", permission(managerauthorization.PermissionManagerDerivedArtifactRead), taskProviderHandler.ListEmbeddingTasks)
			embeddingTaskDefGroup.POST("", permission(managerauthorization.PermissionManagerDerivedArtifactCreate), taskProviderHandler.CreateEmbeddingTask)
			embeddingTaskDefGroup.GET("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), func(c *gin.Context) {
				c.Params = append(c.Params, gin.Param{Key: "task_type", Value: "embedding"})
				taskProviderHandler.TaskDetail(c)
			})
			embeddingTaskDefGroup.PUT("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactUpdate), taskProviderHandler.UpdateEmbeddingTask)
			embeddingTaskDefGroup.DELETE("/:id", permission(managerauthorization.PermissionManagerDerivedArtifactDelete), taskProviderHandler.DeleteEmbeddingTask)
		}

		configGroup := api.Group("/config")
		{
			configHandler := NewConfigHandler(baseMapProviderService)
			configGroup.GET("/map", configHandler.GetMapConfig)
		}

		// Manager 用户动作 API
		api.GET("/resource-actions", permission(managerauthorization.PermissionManagerDataItemRead), resourceActionHandler.GetResourceActions)
		api.POST("/uploads", permission(managerauthorization.PermissionManagerDataItemCreate), uploadHandler.UploadFiles)
		api.POST("/imports", permission(managerauthorization.PermissionManagerDataItemCreate), importHandler.ImportData)
		api.POST("/exports", permission(managerauthorization.PermissionManagerDerivedArtifactCreate), exportHandler.CreateExport)
		api.GET("/exports/:id", permission(managerauthorization.PermissionManagerDerivedArtifactRead), exportHandler.GetExport)
		api.GET("/exports/:id/file", permission(managerauthorization.PermissionManagerDerivedArtifactRead), exportHandler.DownloadExportFile)

		// 数据探查 API（Manager 核心功能，直接挂在 /api/v1/manager 下）
		previewRegistry := metadataService.PreviewRegistry()
		previewResolver := preview.NewPreviewResolver(previewRegistry, systemClient, metaClient, systemServiceClient)
		explorerService := service.NewExplorerService(systemClient, metaClient, previewResolver)
		explorerHandler := NewExplorerHandler(explorerService, previewResolver, metadataService)
		metadataHandler := NewMetadataHandler(metadataService)
		downloadHandler := NewDownloadHandler(metadataService)

		api.GET("/engines", permission(managerauthorization.PermissionManagerDataItemRead), explorerHandler.ListEngines) // 获取可用引擎列表（只读）
		api.POST("/engines/:id/items/refresh", permission(managerauthorization.PermissionManagerDataItemUpdate), metadataHandler.RefreshItem)
		api.GET("/preview", permission(managerauthorization.PermissionManagerContentRead), explorerHandler.Preview)
		api.GET("/downloads/file", permission(managerauthorization.PermissionManagerContentRead), downloadHandler.DownloadFile)
		api.GET("/storage-stream", permission(managerauthorization.PermissionManagerContentRead), explorerHandler.StorageStream)
		api.GET("/storage-assets/:engine_id/*storage_ref", permission(managerauthorization.PermissionManagerContentRead), explorerHandler.StorageAsset)

		// ============================================================
		// 空间数据服务路由组
		// 注意：Manager 不负责引擎管理（创建、更新、删除），仅提供数据访问服务
		// 引擎管理由 System 模块负责，Manager 通过 SystemClient 查询引擎信息
		// ============================================================
		engines := api.Group("/engines")
		{
			// 要素查询（用于表格与地图关联）
			featureHandler := NewFeatureHandler(systemClient, metadataRepo, quickViewService)
			engines.GET("/:id/spatial/features/:feature_id/centroid", permission(managerauthorization.PermissionManagerContentRead), featureHandler.GetFeatureCentroid)
			engines.GET("/:id/spatial/features/:feature_id/geometry", permission(managerauthorization.PermissionManagerContentRead), featureHandler.GetFeatureGeometry)
		}

		searchGroup := api.Group("/search")
		{
			handler := NewSearchHandler(searchService, historyService)
			searchGroup.GET("", permission(managerauthorization.PermissionManagerSearchExecute), handler.Search) // 混合检索（全文 + 向量）
			searchGroup.GET("/history", handler.ListHistory)
			searchGroup.DELETE("/history/:id", handler.DeleteHistoryItem)
			searchGroup.DELETE("/history", handler.ClearHistory)
		}

		// Quick View API：统一 ResourceLocator 入口
		quickViewHandler := NewQuickViewHandler(quickViewService, previewResolver, unifiedMVTService, redisClient)
		if taskProviderHandler != nil {
			quickViewHandler.SetTileCacheTaskService(taskProviderHandler.tileCacheTaskSvc)
			quickViewHandler.SetArtifactTaskServices(taskProviderHandler.rasterCOGTaskSvc, taskProviderHandler.model3DGLBTaskSvc, taskProviderHandler.gaussianSplatKSplatTaskSvc, taskProviderHandler.pointCloudCOPCTaskSvc, taskProviderHandler.cadPreviewTaskSvc, taskProviderHandler.model3DTilesTaskSvc)
		}
		api.GET("/quick-view/capability", permission(managerauthorization.PermissionManagerDataItemRead), quickViewHandler.GetQuickViewCapabilityByLocator)
		api.POST("/quick-view/actions", permission(managerauthorization.PermissionManagerDerivedArtifactCreate), quickViewHandler.ExecuteQuickViewAction)
		api.GET("/quick-view/flatgeobuf", permission(managerauthorization.PermissionManagerContentRead), quickViewHandler.GetQuickViewFlatGeobufByLocator)
		api.GET("/quick-view/geojson", permission(managerauthorization.PermissionManagerContentRead), quickViewHandler.GetQuickViewGeoJSONByLocator)
		api.GET("/quick-view/tiles/:z/:x/:y.mvt", permission(managerauthorization.PermissionManagerDerivedArtifactRead), quickViewHandler.GetQuickViewTileByLocator)
		api.PATCH("/preview-state/preferred-mode", quickViewHandler.UpdatePreferredModeByLocator)
		api.PATCH("/preview-state/view-state", quickViewHandler.UpdateViewStateByLocator)
	}

	return router
}

func isManagerContentResourceRequest(c *gin.Context) bool {
	path := strings.TrimPrefix(c.Request.URL.Path, "/api/v1/manager")
	if path == "/storage-stream" || path == "/downloads/file" ||
		path == "/quick-view/flatgeobuf" || path == "/quick-view/geojson" {
		return true
	}
	return strings.HasPrefix(path, "/storage-assets/")
}

func isManagerDerivedResourceRequest(c *gin.Context) bool {
	path := strings.TrimPrefix(c.Request.URL.Path, "/api/v1/manager")
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 3 && segments[0] == "exports" && segments[2] == "file" {
		return true
	}
	if len(segments) >= 4 && segments[0] == "model3d_tiles" && segments[2] == "assets" {
		return true
	}
	if len(segments) == 5 && segments[0] == "raster_mosaic" && segments[1] == "tiles" {
		return true
	}
	if len(segments) == 3 && segments[2] == "content" {
		switch segments[0] {
		case "raster_cog", "model_3d_glb", "gaussian_splat_ksplat", "point_cloud_copc":
			return true
		}
	}
	if len(segments) == 3 && segments[0] == "cad-previews" && segments[2] == "manifest" {
		return true
	}
	if len(segments) == 6 && segments[0] == "cad-previews" && segments[2] == "tiles" {
		return true
	}
	return len(segments) == 5 && segments[0] == "quick-view" && segments[1] == "tiles"
}
