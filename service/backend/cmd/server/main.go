package main

import (
	"os"

	commonClient "github.com/addp/common/client"
	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
	"github.com/addp/service/internal/api"
	"github.com/addp/service/internal/config"
	"github.com/addp/service/internal/repository"
	"github.com/addp/service/internal/service/data"
	"github.com/addp/service/internal/service/registry"
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

	// 初始化数据库
	db, err := repository.InitDatabase(cfg)
	if err != nil {
		logger.L().Error("数据库初始化失败", "error", err)
		os.Exit(1)
	}

	// 初始化 repositories
	externalServiceRepo := repository.NewExternalServiceRepository(db)

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
	queryService := data.NewQueryService(systemClient, metaClient)

	// 初始化 handlers
	serviceRegistryHandler := api.NewServiceRegistryHandler(externalServiceService)
	dataServiceHandler := api.NewDataServiceHandler(queryService)

	// 设置路由
	router := api.SetupRouter(cfg, serviceRegistryHandler, dataServiceHandler)

	// 启动服务
	addr := ":" + cfg.Port
	logger.L().Info("Service 模块启动", "addr", addr, "schema", cfg.DBSchema)
	if err := router.Run(addr); err != nil {
		logger.L().Error("Service 模块启动失败", "error", err)
		os.Exit(1)
	}
}
