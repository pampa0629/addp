package main

import (
	"context"
	"fmt"
	commonExecution "github.com/addp/common/execution"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	commonconfiguration "github.com/addp/common/configuration"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/manager/internal/api"
	managerauthorization "github.com/addp/manager/internal/authorization"
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
	if err := commonConfig.CheckPortAvailable(cfg.Port); err != nil {
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
	embeddingConfigurationRepo := repository.NewEmbeddingConfigurationRepository(db)
	inferenceScenarioBindingRepo := repository.NewInferenceScenarioBindingRepository(db)
	quickViewPolicyRepo := repository.NewQuickViewPolicyRepository(db)
	baseMapProviderRepo := repository.NewBaseMapProviderRepository(db)
	tileCacheRepo := repository.NewTileCacheRepository(db)
	vectorMaterializedViewRepo := repository.NewVectorMaterializedViewRepository(db)
	rasterCOGRepo := repository.NewRasterCOGRepository(db)
	rasterMosaicRepo := repository.NewRasterMosaicRepository(db)
	vectorTileSetRepo := repository.NewVectorTileSetRepository(db)
	model3DGLBRepo := repository.NewModel3DGLBRepository(db)
	gaussianSplatKSplatRepo := repository.NewGaussianSplatKSplatRepository(db)
	pointCloudCOPCRepo := repository.NewPointCloudCOPCRepository(db)
	pptxPDFRepo := repository.NewPPTXPDFRepository(db)
	model3DTilesRepo := repository.NewModel3DTilesRepository(db)
	exportSessionRepo := repository.NewExportSessionRepository(db)
	dataProfileRepo := repository.NewDataProfileRepository(db)
	dataProfileExecutionRepo := repository.NewDataProfileExecutionRepository(db)
	taskExecRepo := commonExecution.NewTaskExecutionRepository(db)
	embeddingConfigurationService := service.NewEmbeddingConfigurationService(embeddingConfigurationRepo)
	if err := embeddingConfigurationService.Initialize(context.Background()); err != nil {
		logger.L().Error("Manager 向量化配置初始化失败", "error", err)
		os.Exit(1)
	}
	embeddingConfigurationProvider := embeddingConfigurationService.Provider()
	inferenceScenarioBindingService := service.NewInferenceScenarioBindingService(inferenceScenarioBindingRepo)
	quickViewPolicyService := service.NewQuickViewPolicyService(quickViewPolicyRepo)
	baseMapProviderService := service.NewBaseMapProviderService(baseMapProviderRepo)
	logger.L().Info("Manager repositories 初始化完成")

	logger.L().Info("Manager 配置加载完成",
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
	engineCacheService := service.NewEngineCacheService(cfg.SystemServiceURL, nil, redisClient)
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
	// 初始化 Meta 客户端（用于查询元数据）
	if cfg.ServiceClientSecret == "" || cfg.SystemServiceURL == "" || cfg.MetaServiceURL == "" {
		logger.L().Error("Manager Service Client Credential、System URL 和 Meta URL 必须配置")
		os.Exit(1)
	}
	serviceTokenSource, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemServiceURL, "addp-manager", cfg.ServiceClientSecret, nil)
	if err != nil {
		logger.L().Error("Service Token Source 初始化失败", "error", err)
		os.Exit(1)
	}
	systemServiceClient := commonClient.NewSystemServiceClient(cfg.SystemServiceURL, serviceTokenSource, nil)
	securityClient := commonClient.NewSecurityClient(cfg.SecurityServiceURL, serviceTokenSource, nil)
	systemClient := commonClient.NewSystemClient(cfg.SystemServiceURL, serviceTokenSource)
	engineCacheService = service.NewEngineCacheService(cfg.SystemServiceURL, serviceTokenSource, redisClient)
	workflowRuntimeLister := service.NewWorkflowRuntimeEngineLister(systemServiceClient)
	metaClient := commonClient.NewMetaClient(cfg.MetaServiceURL, serviceTokenSource)
	inferenceClient, err := commonClient.NewInferenceClient(cfg.SystemServiceURL, serviceTokenSource, nil)
	if err != nil {
		logger.L().Error("InferenceClient 初始化失败", "error", err)
		os.Exit(1)
	}
	logger.L().Info("MetaClient 已初始化", "meta_url", cfg.MetaServiceURL)

	contentRegistry := objectcontent.NewObjectContentRegistry()
	pluginDirs := preview.ParsePluginDirSpec(cfg.PreviewPluginDir)
	contentPluginSpec := buildPluginDirSpec(pluginDirs)
	objectcontent.LoadObjectContentPlugins(contentRegistry, contentPluginSpec)
	logger.L().Info("数据预览: 已激活内容插件")

	previewRegistry := preview.NewPreviewRegistry()

	preview.LoadPreviewPlugins(previewRegistry, metadataRepo, metaClient, contentRegistry, buildPluginDirSpec(pluginDirs))
	logger.L().Info("数据预览: 已激活预览插件", "providers", previewRegistry.Providers())
	profilePreviewResolver := preview.NewPreviewResolver(previewRegistry, systemClient, metaClient, systemServiceClient)
	dataProfileSampler := service.NewPreviewDataProfileSampleProvider(profilePreviewResolver, metaClient)

	// 初始化 services（注意：Manager 不负责引擎管理，引擎信息通过 SystemClient 获取）
	searchHistoryService := service.NewSearchHistoryService(searchHistoryRepo)
	metadataService := service.NewMetadataService(metadataRepo, systemClient, metaClient, previewRegistry, contentRegistry)
	searchService, err := service.NewHybridSearchService(cfg, embeddingRepo, embeddingConfigurationProvider, inferenceScenarioBindingService, inferenceClient)
	if err != nil {
		logger.L().Error("初始化混合检索服务失败", "error", err)
		os.Exit(1)
	}
	defer searchService.Close()
	managerProjectionBarrier := service.NewManagerProjectionBarrier(dataProfileRepo, dataProfileExecutionRepo, searchService)
	protectionStore, err := projectionstore.New(db, cfg.DBSchema, "manager", managerProjectionBarrier)
	if err != nil {
		logger.L().Error("保护投影本地存储初始化失败", "error", err)
		os.Exit(1)
	}
	if err := managerProjectionBarrier.ReconcileInstalled(context.Background(), db, protectionStore.ManagedTargets()); err != nil {
		logger.L().Error("已安装保护投影的派生数据收敛失败", "error", err)
		os.Exit(1)
	}
	dataProfileService := service.NewDataProfileService(dataProfileRepo, dataProfileExecutionRepo, dataProfileSampler, protectionStore)
	dataProfileHandler := api.NewDataProfileHandler(dataProfileService)

	// 创建统一 MVT 服务（整合实时生成 + 缓存访问，对前端隐藏 fingerprint）
	// ✅ 传入连接池配置，实时生成瓦片使用较小的连接数（默认5，避免峰值压力）
	mvtService := service.NewMVTService(metadataRepo, systemClient, 5)
	mvtService.SetVectorMaterializedViewRepository(vectorMaterializedViewRepo)
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
	spatialPreviewService := service.NewSpatialPreviewService(redisClient, minioClient)
	quickViewPolicy, policyErr := quickViewPolicyService.Get(context.Background())
	if policyErr != nil {
		logger.L().Error("读取快显策略失败", "error", policyErr)
		os.Exit(1)
	}
	if quickViewPolicy.RasterMosaicGenerationTimeoutSec > 0 {
		cfg.RasterMosaicGeneration.Timeout = time.Duration(quickViewPolicy.RasterMosaicGenerationTimeoutSec) * time.Second
	}
	unifiedMVTService := service.NewUnifiedMVTService(
		spatialPreviewService,
		mvtService,
		metadataRepo,
	)
	unifiedMVTService.SetRealtimeTileTimeout(time.Duration(quickViewPolicy.RealtimeTileTimeoutMS) * time.Millisecond)
	unifiedMVTService.SetRealtimeTileRetryAfter(time.Duration(quickViewPolicy.RealtimeTileRetryAfterSec) * time.Second)
	logger.L().Info("统一 MVT 服务已初始化（RESTful API + 三层缓存穿透架构）")

	// 初始化快显状态服务（依赖数据库与 Meta 空间元数据）
	quickViewService := service.NewQuickViewService(db, metaClient)
	quickViewService.SetWorkflowEngineLister(workflowRuntimeLister)
	quickViewService.SetCapabilityOptions(service.QuickViewCapabilityOptions{DirectFlatGeobufMaxRows: quickViewPolicy.DirectFlatGeobufMaxRows, RealtimeTileTimeoutMS: quickViewPolicy.RealtimeTileTimeoutMS})
	quickViewPolicyService.SetApplier(func(value service.QuickViewPolicyResponse) {
		unifiedMVTService.SetRealtimeTileTimeout(time.Duration(value.RealtimeTileTimeoutMS) * time.Millisecond)
		unifiedMVTService.SetRealtimeTileRetryAfter(time.Duration(value.RealtimeTileRetryAfterSec) * time.Second)
		quickViewService.SetCapabilityOptions(service.QuickViewCapabilityOptions{DirectFlatGeobufMaxRows: value.DirectFlatGeobufMaxRows, RealtimeTileTimeoutMS: value.RealtimeTileTimeoutMS})
		if value.RasterMosaicGenerationTimeoutSec > 0 {
			cfg.RasterMosaicGeneration.Timeout = time.Duration(value.RasterMosaicGenerationTimeoutSec) * time.Second
		}
	})

	// 初始化向量化服务（Manager 模块的按需向量化）
	embeddingService, err := service.NewEmbeddingService(embeddingRepo, systemClient, metaClient, inferenceClient, taskExecRepo, embeddingConfigurationProvider, inferenceScenarioBindingService, logger.L())
	if err != nil {
		logger.L().Error("向量化服务初始化失败", "error", err)
		os.Exit(1)
	}
	logger.L().Info("向量化服务已初始化（推理由 Inference Runtime 提供）")

	// 初始化任务定义服务
	embeddingTaskSvc := service.NewEmbeddingTaskService(embeddingRepo, embeddingService, taskExecRepo, embeddingConfigurationProvider)
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
	pptxPDFTaskSvc := service.NewPPTXPDFTaskService(pptxPDFRepo)
	pptxPDFTaskSvc.SetMetaClient(metaClient)
	pptxPDFTaskSvc.SetBucket(minioBucket)
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
	var postGISTileGenerator *mvt.PMTilesGenerator
	if systemClient != nil {
		tileCacheTaskSvc.SetSourceEngineResolver(func(ctx context.Context, engineID uint) (*commonModels.Engine, error) {
			return systemClient.GetEngine(engineID)
		})
		postGISTileGenerator = mvt.NewPMTilesGenerator(mvt.NewTileGenerator(systemClient, cfg.TileCache.MaxDBConns))
		tileCacheTaskSvc.SetTileGenerator(
			service.NewManagerPostGISVectorTileCacheExecutor(postGISTileGenerator, minioClient, minioBucket),
			cfg.TileCache.Concurrency,
		)
		rasterCOGTaskSvc.SetExecutor(service.NewManagerRasterCOGExecutor(
			systemClient,
			workflowRuntimeLister,
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
	var registration *commonClient.ModuleRegistrationLifecycle
	embeddingTaskScheduler := service.NewEmbeddingTaskScheduler(embeddingTaskSvc)
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
	taskProviderHandler.SetPPTXPDFTaskService(pptxPDFTaskSvc)
	taskProviderHandler.SetModel3DTilesTaskService(model3DTilesTaskSvc)
	taskProviderHandler.SetVectorTileSetTaskService(vectorTileSetTaskSvc)

	// 设置 UnifiedMVTService 的 QuickViewService（延迟注入避免循环依赖）
	unifiedMVTService.SetQuickViewService(quickViewService)
	logger.L().Info("快显状态与瓦片缓存任务服务已初始化")

	// 初始化数据导入服务（Shapefile → business-postgres）
	transferClient := commonClient.NewTransferClient(cfg.TransferServiceURL, serviceTokenSource)
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
		serviceTokenSource,
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
	model3DTilesHandler := api.NewModel3DTilesHandler(model3DTilesRepo, minioClient, minioBucket)
	pptxPDFHandler := api.NewPPTXPDFHandler(pptxPDFTaskSvc, minioClient, minioBucket)
	logger.L().Info("数据导入服务已初始化", "transfer_url", cfg.TransferServiceURL)

	lifecycleController := modulelifecycle.NewBusiness("manager", commonClient.ModuleRuntimeRoleBackend)
	router := api.SetupRouter(cfg, metadataService, searchService, searchHistoryService, unifiedMVTService, quickViewService, metadataRepo, systemClient, systemServiceClient, metaClient, cacheManager, redisClient, embeddingService, embeddingConfigurationService, inferenceScenarioBindingService, quickViewPolicyService, baseMapProviderService, spatialPreviewService, rasterCOGRepo, taskProviderHandler, importHandler, uploadHandler, resourceActionHandler, exportHandler, rasterMosaicTileHandler, model3DGLBHandler, gaussianSplatKSplatHandler, pointCloudCOPCHandler, model3DTilesHandler, dataProfileHandler, protectionStore, lifecycleController, pptxPDFHandler)

	serviceHost := commonConfig.GetServiceHost()
	serviceURL := commonConfig.BuildServiceURL(serviceHost, cfg.Port)
	if systemClient != nil {
		rasterMosaicTaskSvc.SetExecutor(service.NewManagerRasterMosaicExecutor(
			systemClient,
			workflowRuntimeLister,
			serviceURL,
			cfg.RasterMosaicGeneration.Timeout,
		))
		model3DTilesTaskSvc.SetExecutor(service.NewManagerModel3DTilesExecutor(
			systemClient,
			workflowRuntimeLister,
			minioClient,
			cfg.MinioEndpoint,
			cfg.MinioAccessKey,
			cfg.MinioSecretKey,
			cfg.MinioUseSSL,
			minioBucket,
			cfg.DocumentWorkflowGeneration.Timeout,
		))
		model3DGLBTaskSvc.SetExecutor(service.NewManagerModel3DGLBExecutor(
			systemClient,
			workflowRuntimeLister,
			minioClient,
			cfg.MinioEndpoint,
			cfg.MinioAccessKey,
			cfg.MinioSecretKey,
			cfg.MinioUseSSL,
			minioBucket,
			cfg.RasterMosaicGeneration.Timeout,
		))
		pptxPDFTaskSvc.SetExecutor(service.NewManagerPPTXPDFExecutor(
			systemClient,
			workflowRuntimeLister,
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
			workflowRuntimeLister,
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
			workflowRuntimeLister,
			minioClient,
			serviceURL,
			cfg.MinioEndpoint,
			cfg.MinioAccessKey,
			cfg.MinioSecretKey,
			cfg.MinioUseSSL,
			minioBucket,
			cfg.RasterMosaicGeneration.Timeout,
		))
		tileCacheTaskSvc.SetWorkflowTileGenerator(service.NewManagerVectorTileCacheWorkflowExecutor(
			systemClient,
			workflowRuntimeLister,
			minioClient,
			serviceURL,
			cfg.MinioEndpoint,
			cfg.MinioAccessKey,
			cfg.MinioSecretKey,
			cfg.MinioUseSSL,
			minioBucket,
			cfg.RasterMosaicGeneration.Timeout,
		))
		vectorTileSetExecutor := service.NewManagerVectorTileSetExecutor(
			systemClient, workflowRuntimeLister, minioClient, serviceURL, cfg.RasterMosaicGeneration.Timeout,
		)
		vectorTileSetExecutor.SetTemporarySourceStorage(
			cfg.MinioEndpoint, cfg.MinioAccessKey, cfg.MinioSecretKey, cfg.MinioUseSSL, minioBucket,
		)
		vectorTileSetExecutor.SetPostGISGenerator(postGISTileGenerator, cfg.TileCache.Concurrency)
		vectorTileSetTaskSvc.SetExecutor(vectorTileSetExecutor)
	}
	if metaClient != nil {
		rasterMosaicTaskSvc.SetMetaScanSubmitter(metaClient)
		vectorTileSetTaskSvc.SetMetaScanSubmitter(metaClient)
	}

	// ========== 模块定义、运行实例与 TaskProvider 声明发布 ==========
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()
	projectionstore.NewRunner(protectionStore, securityClient, systemServiceClient, 30*time.Second, nil).Start(runtimeContext)
	addr := ":" + cfg.Port
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		logger.L().Error("Manager 监听绑定失败", "error", err, "addr", addr)
		return
	}
	if systemServiceClient != nil {
		taskProvider, err := service.ManagerTaskProviderDeclaration()
		if err != nil {
			logger.L().Error("构建 Manager TaskProvider 声明失败", "error", err)
			os.Exit(1)
		}
		registration = systemServiceClient.RegisterAndHeartbeat(runtimeContext, &commonClient.ModuleRegistrationRequest{
			ModuleName: "manager", ModuleURL: serviceURL, RoutePrefix: "/manager",
			HealthCheckURL: serviceURL + "/health/ready",
			TaskProvider:   taskProvider,
			Metadata: map[string]interface{}{
				"capabilities": map[string]interface{}{
					"cleanup_executor": map[string]interface{}{
						"enabled": true,
						"causes":  []string{events.CleanupCauseEngineDeleting, events.CleanupCauseTenantDeleted},
					},
				},
			},
			ConfigurationManagement: &commonconfiguration.ManagementDeclaration{
				SchemaVersion: commonconfiguration.ManagementSchemaVersion,
				Entries: []commonconfiguration.ManagementEntry{{
					ID: "manager.configuration", OwnerModule: "manager",
					ScopeTypes: []string{commonconfiguration.ScopePlatformDefaultWithTenantOverride}, FrontendRoute: "/configuration/manager",
					ReadPermission: managerauthorization.PermissionManagerConfigurationRead, UpdatePermission: managerauthorization.PermissionManagerConfigurationUpdate,
				}},
			},
		})
		lifecycleController.AttachRegistration(registration)
		modulelifecycle.CancelRuntimeOnFatal(registration, stopRuntime)
	}
	embeddingTaskScheduler.SetClaimGate(func() bool { return registration != nil && registration.IsRegistered() })
	if err := embeddingTaskScheduler.Start(runtimeContext); err != nil {
		logger.L().Warn("向量化任务调度器启动失败", "error", err)
	}

	// 启动服务并等待进程信号，统一关闭注册租约和本地资源。
	logger.L().Info("Manager 服务启动", "addr", addr)
	go func() {
		if err := router.RunListener(listener); err != nil {
			logger.L().Error("Manager 服务启动失败", "error", err)
			stopRuntime()
		}
	}()
	<-runtimeContext.Done()
	logger.L().Info("收到关闭信号，正在清理资源...")

	if registration != nil {
		<-registration.Done()
	}
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
