package main

import (
	"context"
	"fmt"
	commonExecution "github.com/addp/common/execution"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	"github.com/addp/common/utils"
	"github.com/addp/manager/internal/api"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/mvt"
	"github.com/addp/manager/internal/objectcontent"
	"github.com/addp/manager/internal/preview"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"

	// 导入 general 引擎插件以触发自动注册
	_ "github.com/addp/common/engine/plugins/builtin/extension"
	_ "github.com/addp/common/engine/plugins/builtin/general"

	// 导入格式解析器以触发自动注册
	_ "github.com/addp/common/format/builtin"
)

// @title           ADDP Manager API
// @version         1.0
// @host      localhost:8081
// @BasePath  /api/v1/manager
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	// 加载根目录统一的环境变量
	commonConfig.LoadEnv()

	// 加载配置
	cfg := config.Load()
	commonConfig.InitLogger("manager-backend.log", &commonConfig.LoggerOptions{
		Level:     cfg.LogLevel,
		Format:    cfg.LogFormat,
		AddSource: &cfg.LogAddSource,
		File:      cfg.LogFile,
	})

	// 检查端口是否可用
	if err := utils.CheckPortAvailable(cfg.Port); err != nil {
		logger.L().Error("端口检查失败", "error", err, "port", cfg.Port)
		os.Exit(1)
	}
	logger.L().Info("端口检查通过", "port", cfg.Port)

	// 初始化数据库
	logger.L().Info("开始初始化数据库")
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		logger.L().Error("数据库初始化失败", "error", err)
		os.Exit(1)
	}
	logger.L().Info("数据库初始化完成")

	// 初始化 repositories
	logger.L().Info("开始初始化 Manager repositories")
	searchHistoryRepo := repository.NewSearchHistoryRepository(db)
	metadataRepo := repository.NewMetadataRepository(db)
	embeddingRepo := repository.NewEmbeddingRepository(db)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	vectorMaterializedViewRepo := repository.NewVectorMaterializedViewRepository(db)
	rasterCOGRepo := repository.NewRasterCOGRepository(db)
	rasterMosaicRepo := repository.NewRasterMosaicRepository(db)
	vectorTileSetRepo := repository.NewVectorTileSetRepository(db)
	model3DGLBRepo := repository.NewModel3DGLBRepository(db)
	gaussianSplatKSplatRepo := repository.NewGaussianSplatKSplatRepository(db)
	pointCloudCOPCRepo := repository.NewPointCloudCOPCRepository(db)
	cadPreviewRepo := repository.NewCADPreviewRepository(db)
	model3DTilesRepo := repository.NewModel3DTilesRepository(db)
	exportSessionRepo := repository.NewExportSessionRepository(db)
	dataProfileRepo := repository.NewDataProfileRepository(db)
	dataProfileExecutionRepo := repository.NewDataProfileExecutionRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	logger.L().Info("Manager repositories 初始化完成")

	logger.L().Info("Manager 配置加载完成",
		"enable_integration", cfg.EnableIntegration,
		"enable_meta_integration", cfg.EnableMetaIntegration,
		"internal_api_key_set", cfg.InternalAPIKey != "",
		"meta_service_url", cfg.MetaServiceURL,
	)

	// 初始化 Redis 客户端（可选，用于资源变更事件同步）
	var redisClient *redis.Client
	if cfg.RedisHost != "" {
		redisAddr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)
		redisClient = redis.NewClient(&redis.Options{
			Addr:     redisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		logger.L().Info("Redis 客户端已初始化", "addr", redisAddr)
	} else {
		logger.L().Warn("Redis 未配置，资源变更事件同步功能将被禁用")
	}

	// 初始化资源缓存服务（带 Redis 事件订阅）
	engineCacheService := service.NewEngineCacheService(cfg.SystemServiceURL, cfg.InternalAPIKey, redisClient)
	_ = engineCacheService // TODO: 集成到 metadataService 中使用

	// 初始化缓存管理器和扫描事件处理器（用于 Meta 扫描完成后自动刷新缓存）
	var cacheManager *service.CacheManager
	var scanEventHandler *service.ScanEventHandler
	if redisClient != nil {
		cacheManager = service.NewCacheManager(metadataRepo, redisClient)
		scanEventHandler = service.NewScanEventHandler(cacheManager, redisClient)
		logger.L().Info("扫描事件订阅已启动，将自动清理扫描完成的资源缓存")
	}

	// 初始化 System 客户端（用于拉取解密的资源连接信息）
	var systemClient *commonClient.SystemClient
	if cfg.EnableIntegration && cfg.InternalAPIKey != "" {
		systemClient = commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
	}

	// 初始化 Meta 客户端（用于查询元数据）
	var metaClient *commonClient.MetaClient
	if cfg.EnableMetaIntegration {
		if cfg.ServiceClientSecret == "" || cfg.MetaServiceURL == "" || cfg.SystemServiceURL == "" {
			logger.L().Error("Meta 集成已启用，但 Service Client Credential 或服务 URL 未配置")
			os.Exit(1)
		}
		tokenSource, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemServiceURL, "addp-manager", cfg.ServiceClientSecret, nil)
		if err != nil {
			logger.L().Error("Service Token Source 初始化失败", "error", err)
			os.Exit(1)
		}
		metaClient = commonClient.NewMetaClient(cfg.MetaServiceURL, tokenSource)
		logger.L().Info("MetaClient 已初始化",
			"meta_url", cfg.MetaServiceURL)
	} else {
		logger.L().Info("Meta 集成未启用")
	}

	contentRegistry := objectcontent.NewObjectContentRegistry()
	pluginDirs := preview.ParsePluginDirSpec(cfg.PreviewPluginDir)
	contentPluginSpec := buildPluginDirSpec(pluginDirs)
	objectcontent.LoadObjectContentPlugins(contentRegistry, contentPluginSpec)
	logger.L().Info("数据预览: 已激活内容插件")

	previewRegistry := preview.NewPreviewRegistry()

	preview.LoadPreviewPlugins(previewRegistry, metadataRepo, metaClient, contentRegistry, buildPluginDirSpec(pluginDirs))
	preview.ConfigureCADPreviewRepository(previewRegistry, cadPreviewRepo)
	logger.L().Info("数据预览: 已激活预览插件", "providers", previewRegistry.Providers())
	profilePreviewResolver := preview.NewPreviewResolver(previewRegistry, systemClient, metaClient)
	dataProfileSampler := service.NewPreviewDataProfileSampleProvider(profilePreviewResolver, metaClient)
	dataProfileService := service.NewDataProfileService(dataProfileRepo, dataProfileExecutionRepo, dataProfileSampler)
	dataProfileHandler := api.NewDataProfileHandler(dataProfileService)

	// 初始化 services（注意：Manager 不负责引擎管理，引擎信息通过 SystemClient 获取）
	searchHistoryService := service.NewSearchHistoryService(searchHistoryRepo)
	metadataService := service.NewMetadataService(metadataRepo, systemClient, metaClient, previewRegistry, contentRegistry)
	searchService, err := service.NewHybridSearchService(cfg, embeddingRepo)
	if err != nil {
		logger.L().Error("初始化混合检索服务失败", "error", err)
		os.Exit(1)
	}
	defer searchService.Close()

	// 创建统一 MVT 服务（整合实时生成 + 缓存访问，对前端隐藏 fingerprint）
	// ✅ 传入连接池配置，实时生成瓦片使用较小的连接数（默认5，避免峰值压力）
	mvtService := service.NewMVTService(metadataRepo, systemClient, 5)
	mvtService.SetVectorMaterializedViewRepository(vectorMaterializedViewRepo)
	spatialPreviewService := service.NewSpatialPreviewService(redisClient)
	unifiedMVTService := service.NewUnifiedMVTService(
		spatialPreviewService,
		mvtService,
		metadataRepo,
	)
	unifiedMVTService.SetRealtimeTileTimeout(time.Duration(cfg.TileCache.RealtimeTileTimeoutMS) * time.Millisecond)
	unifiedMVTService.SetRealtimeTileRetryAfter(time.Duration(cfg.TileCache.RealtimeTileRetryAfterSec) * time.Second)
	logger.L().Info("统一 MVT 服务已初始化（RESTful API + 三层缓存穿透架构）")

	// 初始化 MinIO 客户端（用于瓦片存储和删除）
	minioClient, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: cfg.MinioUseSSL,
	})
	if err != nil {
		log.Fatalf("❌ Failed to create MinIO client: %v", err)
	}

	// MinIO Bucket 名称（瓦片缓存结果默认写入 manager bucket）
	minioBucket := "manager"

	// 初始化快显状态服务（依赖数据库与 Meta 空间元数据）
	quickViewService := service.NewQuickViewService(db, metaClient)
	quickViewService.SetWorkflowEngineLister(systemClient)
	quickViewService.SetCapabilityOptions(service.QuickViewCapabilityOptions{
		DirectFlatGeobufMaxRows: cfg.TileCache.DirectFlatGeobufMaxRows,
		RealtimeTileTimeoutMS:   cfg.TileCache.RealtimeTileTimeoutMS,
	})

	// 初始化向量化服务（Manager 模块的按需向量化）
	embeddingService, err := service.NewEmbeddingService(embeddingRepo, systemClient, metaClient, taskExecRepo, cfg, logger.L())
	if err != nil {
		logger.L().Warn("向量化服务初始化失败（功能将不可用）", "error", err)
		embeddingService = nil // 设置为 nil，允许服务继续启动
	} else {
		logger.L().Info("向量化服务已初始化（支持单对象、目录、Bucket 三级向量化）")
	}

	// 初始化任务定义服务
	embeddingTaskSvc := service.NewEmbeddingTaskService(embeddingRepo, embeddingService, taskExecRepo, cfg)
	tileCacheTaskSvc := service.NewTileCacheTaskService(tileCacheRepo, taskExecRepo)
	vectorMaterializedViewTaskSvc := service.NewVectorMaterializedViewTaskService(vectorMaterializedViewRepo, taskExecRepo)
	rasterCOGTaskSvc := service.NewRasterCOGTaskService(rasterCOGRepo)
	rasterMosaicTaskSvc := service.NewRasterMosaicTaskService(rasterMosaicRepo, taskExecRepo)
	vectorTileSetTaskSvc := service.NewVectorTileSetTaskService(vectorTileSetRepo, taskExecRepo)
	vectorTileSetTaskSvc.SetMetaClient(metaClient)
	vectorTileSetTaskSvc.SetTileCacheRepository(tileCacheRepo)
	model3DGLBTaskSvc := service.NewModel3DGLBTaskService(model3DGLBRepo)
	gaussianSplatKSplatTaskSvc := service.NewGaussianSplatKSplatTaskService(gaussianSplatKSplatRepo)
	pointCloudCOPCTaskSvc := service.NewPointCloudCOPCTaskService(pointCloudCOPCRepo)
	cadPreviewTaskSvc := service.NewCADPreviewTaskService(cadPreviewRepo)
	model3DTilesTaskSvc := service.NewModel3DTilesTaskService(model3DTilesRepo)
	model3DTilesTaskSvc.SetBucket(minioBucket)
	model3DTilesTaskSvc.SetCleaner(service.NewMinIOModel3DTilesCleaner(minioClient, minioBucket))
	rasterCOGTaskSvc.SetBucket(minioBucket)
	rasterCOGTaskSvc.SetCleaner(service.NewMinIORasterCOGCleaner(minioClient, minioBucket))
	model3DGLBTaskSvc.SetBucket(minioBucket)
	model3DGLBTaskSvc.SetCleaner(service.NewMinIOModel3DGLBCleaner(minioClient, minioBucket))
	gaussianSplatKSplatTaskSvc.SetBucket(minioBucket)
	gaussianSplatKSplatTaskSvc.SetCleaner(service.NewMinIOGaussianSplatKSplatCleaner(minioClient, minioBucket))
	gaussianSplatKSplatTaskSvc.SetMetaClient(metaClient)
	pointCloudCOPCTaskSvc.SetBucket(minioBucket)
	pointCloudCOPCTaskSvc.SetCleaner(service.NewMinIOPointCloudCOPCCleaner(minioClient, minioBucket))
	cadPreviewTaskSvc.SetBucket(minioBucket)
	cadPreviewTaskSvc.SetCleaner(service.NewMinIOCADPreviewCleaner(minioClient, minioBucket))
	var postGISTileGenerator *mvt.PMTilesGenerator
	if systemClient != nil {
		postGISTileGenerator = mvt.NewPMTilesGenerator(mvt.NewTileGenerator(systemClient, cfg.TileCache.MaxDBConns))
		tileCacheTaskSvc.SetTileGenerator(
			service.NewManagerPostGISVectorTileCacheExecutor(postGISTileGenerator, minioClient, minioBucket),
			cfg.TileCache.Concurrency,
		)
		rasterCOGTaskSvc.SetExecutor(service.NewManagerRasterCOGExecutor(
			systemClient,
			systemClient,
			minioClient,
			cfg.MinioEndpoint,
			cfg.MinioAccessKey,
			cfg.MinioSecretKey,
			cfg.MinioUseSSL,
			minioBucket,
		))
	}
	vectorMaterializedViewTaskSvc.SetDBProvider(mvtService)
	vectorMaterializedViewTaskSvc.SetPreviewStateRepository(quickViewService.Repository())
	tileCacheTaskSvc.SetQuickViewService(quickViewService)
	tileCacheTaskSvc.SetMetaClient(metaClient)
	tileCacheTaskSvc.SetRealtimeTileTargetResolver(mvtService)
	tileCacheTaskSvc.SetTileCacheRuntimeCacheInvalidator(spatialPreviewService)
	tileCacheTaskSvc.SetTileCacheCleaner(spatialPreviewService)
	embeddingTaskScheduler := service.NewEmbeddingTaskScheduler(embeddingTaskSvc)
	if err := embeddingTaskScheduler.Start(context.Background()); err != nil {
		logger.L().Warn("向量化任务调度器启动失败", "error", err)
	}
	cleanupSvc := service.NewCleanupService(
		redisClient,
		metaClient,
		taskExecRepo,
		quickViewService.Repository(),
		tileCacheTaskSvc,
		embeddingRepo,
		vectorMaterializedViewTaskSvc,
		exportSessionRepo,
		minioClient,
		minioBucket,
		service.ExportCleanupOptions{
			SuccessRetention: cfg.ExportCleanup.SuccessRetention,
			FailedRetention:  cfg.ExportCleanup.FailedRetention,
			MaxRunningAge:    cfg.ExportCleanup.MaxRunningAge,
			Interval:         cfg.ExportCleanup.Interval,
		},
	)
	if err := cleanupSvc.Start(context.Background()); err != nil {
		logger.L().Warn("Manager cleanup 订阅启动失败", "error", err)
	}

	// 初始化 TaskProvider Handler
	taskProviderHandler := api.NewTaskProviderHandler(embeddingTaskSvc, tileCacheTaskSvc, vectorMaterializedViewTaskSvc, rasterCOGTaskSvc, taskExecRepo, rasterMosaicTaskSvc)
	taskProviderHandler.SetModel3DGLBTaskService(model3DGLBTaskSvc)
	taskProviderHandler.SetGaussianSplatKSplatTaskService(gaussianSplatKSplatTaskSvc)
	taskProviderHandler.SetPointCloudCOPCTaskService(pointCloudCOPCTaskSvc)
	taskProviderHandler.SetCADPreviewTaskService(cadPreviewTaskSvc)
	taskProviderHandler.SetModel3DTilesTaskService(model3DTilesTaskSvc)
	taskProviderHandler.SetVectorTileSetTaskService(vectorTileSetTaskSvc)

	// 设置 UnifiedMVTService 的 QuickViewService（延迟注入避免循环依赖）
	unifiedMVTService.SetQuickViewService(quickViewService)
	logger.L().Info("快显状态与瓦片缓存任务服务已初始化")

	// 初始化数据导入服务（Shapefile → business-postgres）
	transferClient := commonClient.NewTransferClient(cfg.TransferServiceURL, cfg.InternalAPIKey)
	importService := service.NewImportService(
		minioClient,
		minioBucket,
		transferClient,
		vectorMaterializedViewTaskSvc,
	)
	importHandler := api.NewImportHandler(importService)
	uploadService := service.NewUploadService(systemClient, metaClient)
	uploadHandler := api.NewUploadHandler(uploadService)
	resourceActionService := service.NewResourceActionService(systemClient)
	resourceActionHandler := api.NewResourceActionHandler(resourceActionService)
	exportService := service.NewExportService(
		systemClient,
		transferClient,
		exportSessionRepo,
		minioClient,
		minioBucket,
	)
	exportHandler := api.NewExportHandler(exportService)
	rasterMosaicRuntimeClient := service.NewHTTPRasterMosaicRuntimeClient(
		cfg.RasterMosaicRuntime.BaseURL,
		cfg.RasterMosaicRuntime.InternalKey,
		cfg.RasterMosaicRuntime.Timeout,
	)
	rasterMosaicTileService := service.NewRasterMosaicTileService(
		systemClient,
		metaClient,
		rasterMosaicRuntimeClient,
		cfg.RasterMosaicRuntime.TileSize,
	)
	rasterMosaicTileHandler := api.NewRasterMosaicTileHandler(rasterMosaicTileService)
	model3DGLBHandler := api.NewModel3DGLBHandler(model3DGLBRepo, minioClient, minioBucket)
	gaussianSplatKSplatHandler := api.NewGaussianSplatKSplatHandler(gaussianSplatKSplatRepo, minioClient, minioBucket)
	pointCloudCOPCHandler := api.NewPointCloudCOPCHandler(pointCloudCOPCRepo, minioClient, minioBucket)
	cadPreviewHandler := api.NewCADPreviewHandler(cadPreviewTaskSvc, cadPreviewRepo, minioClient, minioBucket)
	model3DTilesHandler := api.NewModel3DTilesHandler(model3DTilesRepo, minioClient, minioBucket)
	logger.L().Info("数据导入服务已初始化", "transfer_url", cfg.TransferServiceURL)

	router := api.SetupRouter(cfg, metadataService, searchService, searchHistoryService, unifiedMVTService, quickViewService, metadataRepo, systemClient, metaClient, cacheManager, redisClient, embeddingService, spatialPreviewService, rasterCOGRepo, taskProviderHandler, importHandler, uploadHandler, resourceActionHandler, exportHandler, rasterMosaicTileHandler, model3DGLBHandler, gaussianSplatKSplatHandler, pointCloudCOPCHandler, cadPreviewHandler, model3DTilesHandler, dataProfileHandler)

	serviceHost := utils.GetServiceHost()
	port := utils.GetModulePort("manager")
	serviceURL := utils.BuildServiceURL(serviceHost, port)
	if systemClient != nil {
		rasterMosaicTaskSvc.SetExecutor(service.NewManagerRasterMosaicExecutor(
			systemClient,
			systemClient,
			serviceURL,
			cfg.InternalAPIKey,
			cfg.RasterMosaicGeneration.Timeout,
		))
		model3DTilesTaskSvc.SetExecutor(service.NewManagerModel3DTilesExecutor(
			systemClient,
			systemClient,
			minioClient,
			cfg.MinioEndpoint,
			cfg.MinioAccessKey,
			cfg.MinioSecretKey,
			cfg.MinioUseSSL,
			minioBucket,
			cfg.RasterMosaicGeneration.Timeout,
		))
		model3DGLBTaskSvc.SetExecutor(service.NewManagerModel3DGLBExecutor(
			systemClient,
			systemClient,
			minioClient,
			cfg.MinioEndpoint,
			cfg.MinioAccessKey,
			cfg.MinioSecretKey,
			cfg.MinioUseSSL,
			minioBucket,
			cfg.RasterMosaicGeneration.Timeout,
		))
		gaussianSplatKSplatTaskSvc.SetExecutor(service.NewManagerGaussianSplatKSplatExecutor(
			systemClient,
			systemClient,
			minioClient,
			cfg.MinioEndpoint,
			cfg.MinioAccessKey,
			cfg.MinioSecretKey,
			cfg.MinioUseSSL,
			minioBucket,
			cfg.RasterMosaicGeneration.Timeout,
		))
		pointCloudCOPCTaskSvc.SetExecutor(service.NewManagerPointCloudCOPCExecutor(
			systemClient,
			systemClient,
			minioClient,
			serviceURL,
			cfg.InternalAPIKey,
			cfg.MinioEndpoint,
			cfg.MinioAccessKey,
			cfg.MinioSecretKey,
			cfg.MinioUseSSL,
			minioBucket,
			cfg.RasterMosaicGeneration.Timeout,
		))
		cadPreviewTaskSvc.SetExecutor(service.NewManagerCADPreviewExecutor(
			systemClient,
			systemClient,
			minioClient,
			cfg.MinioEndpoint,
			cfg.MinioAccessKey,
			cfg.MinioSecretKey,
			cfg.MinioUseSSL,
			minioBucket,
			cfg.RasterMosaicGeneration.Timeout,
		))
		tileCacheTaskSvc.SetWorkflowTileGenerator(service.NewManagerVectorTileCacheWorkflowExecutor(
			systemClient,
			systemClient,
			minioClient,
			serviceURL,
			cfg.InternalAPIKey,
			cfg.MinioEndpoint,
			cfg.MinioAccessKey,
			cfg.MinioSecretKey,
			cfg.MinioUseSSL,
			minioBucket,
			cfg.RasterMosaicGeneration.Timeout,
		))
		vectorTileSetExecutor := service.NewManagerVectorTileSetExecutor(
			systemClient, systemClient, minioClient, serviceURL, cfg.InternalAPIKey, cfg.RasterMosaicGeneration.Timeout,
		)
		vectorTileSetExecutor.SetPostGISGenerator(postGISTileGenerator, cfg.TileCache.Concurrency)
		vectorTileSetTaskSvc.SetExecutor(vectorTileSetExecutor)
	}
	if metaClient != nil {
		rasterMosaicTaskSvc.SetMetaScanSubmitter(metaClient)
		vectorTileSetTaskSvc.SetMetaScanSubmitter(metaClient)
	}

	// ========== 服务注册（注册到 System service_registry）==========
	if cfg.EnableIntegration && cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		registryClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
		registryClient.RegisterAndHeartbeatWithMetadata("manager", serviceURL, "/manager", map[string]interface{}{
			"module": "manager",
			"capabilities": map[string]interface{}{
				"cleanup_executor": map[string]interface{}{
					"enabled": true,
					"causes":  []string{events.CleanupCauseEngineDeleting, events.CleanupCauseTenantDeleted},
				},
			},
		})
	}

	// ========== 任务提供者注册（启动时自动注册到 System task_providers）==========
	if cfg.EnableIntegration && cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		taskProviderRegistry := service.NewTaskProviderRegistryService(
			cfg.SystemServiceURL,
			cfg.InternalAPIKey,
			serviceURL,
		)

		// 后台异步注册（不阻塞启动，支持重试）
		go func() {
			time.Sleep(2 * time.Second) // 等待服务完全启动
			maxRetries := 5
			for attempt := 1; attempt <= maxRetries; attempt++ {
				if err := taskProviderRegistry.Register(); err != nil {
					logger.L().Warn("任务提供者注册失败",
						"attempt", fmt.Sprintf("%d/%d", attempt, maxRetries),
						"error", err)
					time.Sleep(time.Duration(attempt*2) * time.Second) // 指数退避
					continue
				}
				logger.L().Info("✅ Manager 模块已注册到 task_providers")
				return
			}
			logger.L().Error("任务提供者注册失败（已达最大重试次数）", "max_retries", maxRetries)
		}()
	}

	// ✅ 注册优雅关闭处理器（关闭所有数据库连接池）
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		logger.L().Info("收到关闭信号，正在清理资源...")

		// 停止扫描事件订阅
		if scanEventHandler != nil {
			scanEventHandler.Stop()
		}
		embeddingTaskScheduler.Stop()
		cleanupSvc.Stop()

		if err := mvtService.Close(); err != nil {
			logger.L().Error("关闭数据库连接池失败", "error", err)
		} else {
			logger.L().Info("所有数据库连接池已关闭")
		}
		os.Exit(0)
	}()

	// 启动服务
	addr := ":" + cfg.Port
	logger.L().Info("Manager 服务启动", "addr", addr)
	if err := router.Run(addr); err != nil {
		logger.L().Error("Manager 服务启动失败", "error", err)
		os.Exit(1)
	}
}

func buildPluginDirSpec(dirs []string) string {
	if len(dirs) == 0 {
		return ""
	}
	cleanDirs := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		trimmed := strings.TrimSpace(dir)
		if trimmed == "" {
			continue
		}
		cleanDirs = append(cleanDirs, trimmed)
	}
	return strings.Join(cleanDirs, ",")
}
