package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	commonconfiguration "github.com/addp/common/configuration"
	_ "github.com/addp/common/engine/plugins/builtin/general"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonScheduler "github.com/addp/common/scheduler"
	"github.com/addp/service/internal/api"
	serviceauthorization "github.com/addp/service/internal/authorization"
	"github.com/addp/service/internal/config"
	"github.com/addp/service/internal/repository"
	serviceInternal "github.com/addp/service/internal/service"
	"github.com/addp/service/internal/service/data"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"
)

// @title           ADDP Service API
// @version         1.0
// @host      localhost:8086
// @BasePath  /api/v1/service
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	// 加载根目录统一的环境变量
	commonConfig.LoadEnv()

	// 加载配置
	cfg := config.Load()
	commonConfig.InitLogger("service-backend.log", &commonConfig.LoggerOptions{
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
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		logger.L().Error("数据库初始化失败", "error", err)
		os.Exit(1)
	}

	// 初始化 repositories
	queryServiceRepo := repository.NewQueryServiceRepository(db)
	registeredServiceRepo := repository.NewRegisteredServiceRepository(db)
	tileServiceRepo := repository.NewTileServiceRepository(db)
	graphQueryServiceRepo := repository.NewGraphQueryServiceRepository(db)
	runtimePolicyService := serviceInternal.NewRuntimePolicyService(repository.NewRuntimePolicyRepository(db))
	if err := runtimePolicyService.Apply(context.Background(), cfg); err != nil {
		logger.L().Error("服务运行策略加载失败", "error", err)
		os.Exit(1)
	}
	taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db)

	logger.L().Info("Service 配置加载完成")

	redisClient := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer redisClient.Close()

	// 初始化 System 客户端
	// 初始化 Meta 客户端
	if cfg.ServiceClientSecret == "" || cfg.MetaServiceURL == "" || cfg.SystemServiceURL == "" {
		logger.L().Error("Service Client Credential、Meta URL 和 System URL 必须配置")
		os.Exit(1)
	}
	tokenSource, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemServiceURL, "addp-service", cfg.ServiceClientSecret, nil)
	if err != nil {
		logger.L().Error("Service Token Source 初始化失败", "error", err)
		os.Exit(1)
	}
	systemServiceClient := commonClient.NewSystemServiceClient(cfg.SystemServiceURL, tokenSource, nil)
	systemClient := commonClient.NewSystemClient(cfg.SystemServiceURL, tokenSource)
	metaClient := commonClient.NewMetaClient(cfg.MetaServiceURL, tokenSource)
	logger.L().Info("MetaClient 已初始化", "meta_url", cfg.MetaServiceURL)

	// 构建服务基础URL用于查询服务
	// 使用 Gateway URL 作为对外服务端点的基础地址
	queryServiceService := serviceInternal.NewQueryServiceService(queryServiceRepo, systemClient, metaClient, cfg.GatewayURL)
	queryExecutorService := serviceInternal.NewQueryExecutorService(systemClient, systemServiceClient, cfg.EncryptionKey)
	querySampleService := serviceInternal.NewQuerySampleService(
		systemServiceClient,
		commonClient.NewSystemExecutionAuthorizationClient(cfg.SystemServiceURL, nil),
		metaClient,
	)

	// 图查询服务
	graphQueryServiceService := serviceInternal.NewGraphQueryServiceService(graphQueryServiceRepo, cfg.GatewayURL)
	graphQueryExecutor := data.NewGraphQueryExecutor(graphQueryServiceRepo, systemClient)

	// 注册服务使用 Gateway URL 作为代理端点的基础地址
	// 这样用户可以通过统一的 Gateway 访问代理服务
	registeredServiceService := serviceInternal.NewRegisteredServiceService(registeredServiceRepo, cfg.GatewayURL)

	// 瓦片服务使用 Gateway URL 作为服务端点的基础地址
	tileServiceService := serviceInternal.NewTileServiceService(tileServiceRepo, metaClient, cfg.GatewayURL)

	// 初始化 MinIO 客户端（用于瓦片存储）
	minioClient, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: cfg.MinioUseSSL,
	})
	if err != nil {
		logger.L().Error("MinIO 客户端初始化失败", "error", err)
		os.Exit(1)
	}
	logger.L().Info("MinIO 客户端已初始化", "endpoint", cfg.MinioEndpoint)

	// MinIO Bucket 名称（Service 模块专用）
	minioBucket := "service"

	// 初始化瓦片相关服务
	staticTileService := serviceInternal.NewStaticTileService(systemClient)
	dynamicTileService := serviceInternal.NewDynamicTileService(systemClient)
	tileCacheService := serviceInternal.NewTileCacheService(minioClient, minioBucket, "tiles")
	cleanupService := serviceInternal.NewCleanupService(db, redisClient, taskExecutionRepo)
	if err := cleanupService.Start(context.Background()); err != nil {
		logger.L().Warn("Service 资源回收服务启动失败", "error", err)
	}
	defer cleanupService.Stop()

	queryService := data.NewQueryService(systemClient, metaClient)

	// 初始化 handlers
	queryServiceHandler := api.NewQueryServiceHandler(queryServiceService, queryExecutorService)
	ogcFeaturesHandler := api.NewOGCFeaturesHandler(queryServiceService, queryExecutorService, cfg.GatewayURL)
	registeredServiceHandler := api.NewRegisteredServiceHandler(registeredServiceService)
	tileServiceHandler := api.NewTileServiceHandler(tileServiceService)
	tileEndpointHandler := api.NewTileEndpointHandler(tileServiceService, staticTileService, dynamicTileService, tileCacheService)
	wmtsHandler := api.NewWMTSHandler(tileServiceService)
	ogcTilesHandler := api.NewOGCTilesHandler(tileServiceService, tileEndpointHandler)
	dataServiceHandler := api.NewDataServiceHandler(queryService)
	resourceCapabilityHandler := api.NewResourceCapabilityHandler(systemClient, metaClient)
	resourceCapabilityHandler.SetQuerySampleService(querySampleService)
	serviceEndpointHandler := api.NewServiceEndpointHandler(queryServiceService, registeredServiceService, tileServiceService)
	graphQueryHandler := api.NewGraphQueryHandler(graphQueryServiceService, graphQueryExecutor)

	// 设置路由（传递 systemClient 用于审计日志）
	router := api.SetupRouter(cfg, db, dataServiceHandler, queryServiceHandler, ogcFeaturesHandler, registeredServiceHandler, tileServiceHandler, tileEndpointHandler, wmtsHandler, ogcTilesHandler, resourceCapabilityHandler, serviceEndpointHandler, graphQueryHandler, systemClient, systemServiceClient, runtimePolicyService)

	// ========== 模块注册（注册到 System service_registry）==========
	runtimeContext, stopRuntime := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopRuntime()
	var registrationDone <-chan struct{}
	if cfg.SystemServiceURL != "" {
		serviceHost := commonConfig.GetServiceHost()
		serviceURL := commonConfig.BuildServiceURL(serviceHost, cfg.Port)
		registrationDone = systemServiceClient.RegisterAndHeartbeat(runtimeContext, &commonClient.ModuleRegistrationRequest{ModuleName: "service", ModuleURL: serviceURL, RoutePrefix: "/service", HealthCheckURL: serviceURL + "/health",
			ConfigurationManagement: &commonconfiguration.ManagementDeclaration{SchemaVersion: commonconfiguration.ManagementSchemaVersion, Entries: []commonconfiguration.ManagementEntry{{
				ID: "service.configuration", OwnerModule: "service", ScopeTypes: []string{commonconfiguration.ScopePlatformOnly}, FrontendRoute: "/configuration/service",
				ReadPermission: serviceauthorization.PermissionServiceConfigurationRead, UpdatePermission: serviceauthorization.PermissionServiceConfigurationUpdate,
			}}},
			Metadata: map[string]interface{}{
				"module": "service",
				"capabilities": map[string]interface{}{
					"cleanup_executor": map[string]interface{}{
						"enabled": true,
						"causes":  []string{events.CleanupCauseEngineDeleting, events.CleanupCauseTenantDeleted},
					},
				},
			},
		})
	}

	// 初始化调度器
	ctx := context.Background()
	scheduler, err := commonScheduler.NewScheduler(commonScheduler.Options{
		Name: "service-scheduler",
	})
	if err != nil {
		logger.L().Error("调度器初始化失败", "error", err)
		os.Exit(1)
	}

	// 启动调度器
	if err := scheduler.Start(ctx); err != nil {
		logger.L().Error("调度器启动失败", "error", err)
		os.Exit(1)
	}
	logger.L().Info("调度器已启动")

	// 注册定时任务
	// 1. 每小时自动健康检查所有活跃服务
	healthCheckHandler := func(ctx context.Context, taskID string) error {
		logger.L().Info("开始执行定时健康检查")

		// 获取所有租户列表
		var tenants []struct {
			ID uint
		}

		// 如果有 System 客户端，则从 System 服务获取租户列表
		// 否则，从数据库中查询所有不同的租户ID
		if systemClient != nil {
			// TODO: 调用 System API 获取租户列表
			// 暂时使用数据库查询
			logger.L().Debug("从数据库查询租户列表")
		}

		// 从注册服务表中查询所有唯一的租户ID
		if err := db.Raw("SELECT DISTINCT tenant_id AS id FROM service.registered_services WHERE status = 'active'").Scan(&tenants).Error; err != nil {
			logger.L().Error("查询租户列表失败", "error", err)
			return err
		}

		logger.L().Info("找到活跃租户", "count", len(tenants))

		// 遍历每个租户
		totalChecked := 0
		totalSuccess := 0
		totalFailed := 0

		for _, tenant := range tenants {
			// 获取该租户下所有活跃的注册服务
			services, err := registeredServiceRepo.GetByTenant(tenant.ID, map[string]interface{}{
				"status": "active",
			})
			if err != nil {
				logger.L().Error("获取租户服务失败", "tenant_id", tenant.ID, "error", err)
				continue
			}

			logger.L().Debug("租户服务数量", "tenant_id", tenant.ID, "count", len(services))

			// 对每个服务执行健康检查
			for _, service := range services {
				totalChecked++
				result, err := registeredServiceService.HealthCheck(service.ID)
				if err != nil {
					logger.L().Error("健康检查失败", "service_id", service.ID, "service_name", service.ServiceName, "error", err)
					totalFailed++
				} else {
					if result.Status == "healthy" {
						totalSuccess++
					} else {
						totalFailed++
					}
					logger.L().Debug("健康检查完成",
						"service_id", service.ID,
						"service_name", service.ServiceName,
						"status", result.Status,
						"response_time", result.ResponseTime)
				}
			}
		}

		logger.L().Info("定时健康检查完成",
			"total", totalChecked,
			"success", totalSuccess,
			"failed", totalFailed)

		return nil
	}
	if err := scheduler.Schedule(ctx, "health-check", cfg.HealthCheckCron, healthCheckHandler); err != nil {
		logger.L().Warn("健康检查任务调度失败", "error", err)
	} else {
		logger.L().Info("健康检查任务已调度", "cron", cfg.HealthCheckCron)
	}

	// 2. 每天凌晨刷新所有服务元数据
	metadataRefreshHandler := func(ctx context.Context, taskID string) error {
		logger.L().Info("开始执行定时元数据刷新")

		// 获取所有租户的租户ID
		var tenants []struct {
			ID uint
		}

		// 从注册服务表中查询所有唯一的租户ID（仅OGC服务需要刷新）
		if err := db.Raw(`SELECT DISTINCT tenant_id AS id
			FROM service.registered_services
			WHERE service_type IN ('wms', 'wfs', 'wmts', 'ogc_api')
			AND status = 'active'`).Scan(&tenants).Error; err != nil {
			logger.L().Error("查询租户列表失败", "error", err)
			return err
		}

		logger.L().Info("找到有OGC服务的租户", "count", len(tenants))

		// 遍历每个租户
		totalRefreshed := 0
		totalSuccess := 0
		totalFailed := 0

		for _, tenant := range tenants {
			// 获取该租户下所有OGC服务
			services, err := registeredServiceRepo.GetByTenant(tenant.ID, map[string]interface{}{
				"status": "active",
			})
			if err != nil {
				logger.L().Error("获取租户服务失败", "tenant_id", tenant.ID, "error", err)
				continue
			}

			// 仅处理 OGC 服务
			for _, service := range services {
				if service.ServiceType == "wms" || service.ServiceType == "wfs" ||
					service.ServiceType == "wmts" || service.ServiceType == "ogc_api" {

					totalRefreshed++
					err := registeredServiceService.RefreshMetadata(service.ID, false)
					if err != nil {
						logger.L().Error("元数据刷新失败",
							"service_id", service.ID,
							"service_name", service.ServiceName,
							"type", service.ServiceType,
							"error", err)
						totalFailed++
					} else {
						totalSuccess++
						logger.L().Debug("元数据刷新成功",
							"service_id", service.ID,
							"service_name", service.ServiceName,
							"type", service.ServiceType)
					}
				}
			}
		}

		logger.L().Info("定时元数据刷新完成",
			"total", totalRefreshed,
			"success", totalSuccess,
			"failed", totalFailed)

		return nil
	}
	if err := scheduler.Schedule(ctx, "metadata-refresh", cfg.MetadataRefreshCron, metadataRefreshHandler); err != nil {
		logger.L().Warn("元数据刷新任务调度失败", "error", err)
	} else {
		logger.L().Info("元数据刷新任务已调度", "cron", cfg.MetadataRefreshCron)
	}

	// 启动 HTTP 服务（在 goroutine 中运行）
	addr := ":" + cfg.Port
	logger.L().Info("Service 模块启动", "addr", addr, "schema", cfg.DBSchema)

	go func() {
		if err := router.Run(addr); err != nil {
			logger.L().Error("Service 模块启动失败", "error", err)
			stopRuntime()
		}
	}()

	// 等待中断信号以优雅关闭
	<-runtimeContext.Done()

	logger.L().Info("收到关闭信号，开始优雅关闭...")
	if registrationDone != nil {
		<-registrationDone
	}

	// 停止调度器（等待正在执行的任务完成）
	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := scheduler.Stop(stopCtx); err != nil {
		logger.L().Error("调度器关闭失败", "error", err)
	} else {
		logger.L().Info("调度器已停止")
	}

	// 关闭动态瓦片服务的数据库连接
	if err := dynamicTileService.Close(); err != nil {
		logger.L().Error("动态瓦片服务关闭失败", "error", err)
	} else {
		logger.L().Info("动态瓦片服务已关闭")
	}

	logger.L().Info("Service 模块已优雅关闭")
}
