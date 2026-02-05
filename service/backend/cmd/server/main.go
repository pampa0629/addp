package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
	"github.com/addp/common/utils"
	commonScheduler "github.com/addp/common/scheduler"
	"github.com/addp/service/internal/api"
	"github.com/addp/service/internal/config"
	"github.com/addp/service/internal/repository"
	"github.com/addp/service/internal/service/data"
	"github.com/addp/service/internal/service/registry"
	serviceInternal "github.com/addp/service/internal/service"
)

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
	if err := utils.CheckPortAvailable(cfg.Port); err != nil {
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
	externalServiceRepo := repository.NewExternalServiceRepository(db)
	internalServiceRepo := repository.NewInternalServiceRepository(db)
	queryServiceRepo := repository.NewQueryServiceRepository(db)
	registeredServiceRepo := repository.NewRegisteredServiceRepository(db)

	logger.L().Info("Service 配置加载完成",
		"enable_integration", cfg.EnableIntegration,
		"internal_api_key_set", cfg.InternalAPIKey != "",
	)

	// 初始化 System 客户端
	var systemClient *commonClient.SystemClient
	if cfg.EnableIntegration && cfg.InternalAPIKey != "" {
		systemClient = commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
	}

	// 初始化 Meta 客户端
	var metaClient *commonClient.MetaClient
	if cfg.EnableIntegration && cfg.InternalAPIKey != "" && cfg.MetaServiceURL != "" {
		metaClient = commonClient.NewMetaClientWithInternalKey(cfg.MetaServiceURL, cfg.InternalAPIKey)
		logger.L().Info("MetaClient 已初始化", "meta_url", cfg.MetaServiceURL)
	} else {
		logger.L().Warn("Meta 集成未启用或配置不完整，元数据查询功能将不可用")
	}

	// 初始化 services
	externalServiceService := registry.NewExternalServiceService(externalServiceRepo)
	internalServiceService := serviceInternal.NewInternalServiceService(internalServiceRepo, metaClient)

	// 构建服务基础URL用于查询服务
	serviceHost := utils.GetServiceHost()
	port := utils.GetModulePort("service")
	serviceURL := utils.BuildServiceURL(serviceHost, port)

	queryServiceService := serviceInternal.NewQueryServiceService(queryServiceRepo, metaClient, serviceURL)
	queryExecutorService := serviceInternal.NewQueryExecutorService(queryServiceRepo, systemClient)
	registeredServiceService := serviceInternal.NewRegisteredServiceService(registeredServiceRepo, serviceURL)
	queryService := data.NewQueryService(systemClient, metaClient)

	// 初始化 handlers
	internalServiceHandler := api.NewInternalServiceHandler(internalServiceService)
	queryServiceHandler := api.NewQueryServiceHandler(queryServiceService, queryExecutorService)
	registeredServiceHandler := api.NewRegisteredServiceHandler(registeredServiceService)
	serviceRegistryHandler := api.NewServiceRegistryHandler(externalServiceService)
	dataServiceHandler := api.NewDataServiceHandler(queryService)
	engineHandler := api.NewEngineHandler(systemClient)
	dataSourceHandler := api.NewDataSourceHandler(systemClient, cfg.MetaServiceURL) // 数据源代理 Handler
	sqlDB, err := db.DB()
	if err != nil {
		logger.L().Error("Failed to get SQL DB", "error", err)
		os.Exit(1)
	}
	wfsHandler := api.NewWFSHandler(internalServiceRepo, db, sqlDB)
	ogcAPIHandler := api.NewOGCAPIHandler(internalServiceRepo, db)
	wmtsHandler := api.NewWMTSHandler(internalServiceRepo, db)
	restQueryHandler := api.NewRestQueryHandler(internalServiceRepo, db)

	// 设置路由（传递 systemClient 用于审计日志）
	router := api.SetupRouter(cfg, serviceRegistryHandler, dataServiceHandler, internalServiceHandler, queryServiceHandler, registeredServiceHandler, wfsHandler, ogcAPIHandler, wmtsHandler, restQueryHandler, engineHandler, dataSourceHandler, systemClient)

	// ========== 模块注册（注册到 System service_registry）==========
	if cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		go func() {
			// 等待3秒确保服务完全启动
			time.Sleep(3 * time.Second)

			// 构建服务URL
			serviceHost := utils.GetServiceHost()
			port := utils.GetModulePort("service")
			serviceURL := utils.BuildServiceURL(serviceHost, port)

			// 创建 System 客户端用于模块注册
			registryClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)

			// 注册服务
			registrationReq := &commonClient.ModuleRegistrationRequest{
				ModuleName:    "service",
				ModuleURL:     serviceURL,
				RoutePrefix:    "/service",
				HealthCheckURL: serviceURL + "/health",
				Metadata: map[string]interface{}{
					"module": "service",
				},
			}

			maxRetries := 3
			for attempt := 1; attempt <= maxRetries; attempt++ {
				if err := registryClient.RegisterModule(registrationReq); err != nil {
					logger.L().Warn("Service 模块注册失败",
						"attempt", attempt, "max", maxRetries, "error", err)
					time.Sleep(time.Duration(attempt*5) * time.Second)
					continue
				}
				logger.L().Info("Service 模块注册成功", "url", serviceURL)
				break
			}

			// 启动心跳
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()

			for range ticker.C {
				if err := registryClient.SendHeartbeat("service"); err != nil {
					logger.L().Error("Service 心跳失败", "error", err)
				} else {
					logger.L().Debug("Service 心跳成功")
				}
			}
		}()
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
			os.Exit(1)
		}
	}()

	// 等待中断信号以优雅关闭
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	logger.L().Info("收到关闭信号，开始优雅关闭...")

	// 停止调度器（等待正在执行的任务完成）
	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := scheduler.Stop(stopCtx); err != nil {
		logger.L().Error("调度器关闭失败", "error", err)
	} else {
		logger.L().Info("调度器已停止")
	}

	logger.L().Info("Service 模块已优雅关闭")
}
