package api

import (
	"context"
	"fmt"
	commonClient "github.com/addp/common/client"
	"time"

	"github.com/addp/common/logger"
	i18nmiddleware "github.com/addp/common/middleware/i18n"

	"github.com/addp/common/modulelifecycle"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"

	_ "github.com/addp/system/docs"
)

func SetupRouter(db *gorm.DB, cfg *config.Config) *gin.Engine {
	router := modulelifecycle.NewRouter()
	if err := cfg.ValidateTrustedProxies(); err != nil {
		panic(err)
	}
	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		panic(fmt.Errorf("配置受信代理失败: %w", err))
	}

	var redisClient *redis.Client
	if cfg.RedisHost != "" {
		redisAddr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)
		redisClient = redis.NewClient(&redis.Options{Addr: redisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB})
		logger.L().Info("Redis 客户端已初始化", "addr", redisAddr)
	} else {
		logger.L().Warn("Redis 未配置，资源变更事件通知功能将被禁用")
	}

	securityPolicyService := iam.NewSecurityPolicyService(iam.NewRepository(db))
	securityPolicyContext, cancelSecurityPolicy := context.WithTimeout(context.Background(), 10*time.Second)
	securityPolicy, err := securityPolicyService.LoadAndMarkApplied(securityPolicyContext)
	cancelSecurityPolicy()
	if err != nil {
		panic(fmt.Errorf("加载 IAM 安全策略失败: %w", err))
	}
	runtime, err := NewIAMRuntime(db, cfg, *securityPolicy)
	if err != nil {
		panic(fmt.Errorf("装配目标 IAM Runtime 失败: %w", err))
	}
	auditWriter := iam.NewAuditWriter(runtime.Repository)

	engineRepo := repository.NewEngineRepository(db)
	apiConsumerRepo := repository.NewAPIConsumerRepository(db)
	moduleRegistryRepo := repository.NewModuleRegistryRepository(db)
	engineService := service.NewEngineService(engineRepo, cfg.EncryptionKey, redisClient)
	engineHandler := NewEngineHandler(engineService)
	executionAuthorizationHandler, err := NewIAMExecutionAuthorizationHandler(
		runtime.ExecutionAuthorizationService,
		engineService,
	)
	if err != nil {
		panic(fmt.Errorf("装配 IAM Execution Authorization Handler 失败: %w", err))
	}
	runtime.ExecutionAuthorizationHandler = executionAuthorizationHandler
	notebookSessionAuthorizationHandler, err := NewIAMNotebookSessionAuthorizationHandler(
		runtime.NotebookSessionAuthorizationService,
		engineService,
		service.NewStorageEngineService(),
	)
	if err != nil {
		panic(fmt.Errorf("装配 IAM Notebook Session Authorization Handler 失败: %w", err))
	}
	runtime.NotebookSessionAuthorizationHandler = notebookSessionAuthorizationHandler
	taskAuthorizationSubjectHandler, err := NewIAMTaskAuthorizationSubjectHandler(
		runtime.TaskAuthorizationSubjectService,
	)
	if err != nil {
		panic(fmt.Errorf("装配 IAM Task Authorization Subject Handler 失败: %w", err))
	}
	runtime.TaskAuthorizationSubjectHandler = taskAuthorizationSubjectHandler
	apiConsumerService := service.NewAPIConsumerService(apiConsumerRepo)
	moduleRegistryService := service.NewModuleRegistryService(moduleRegistryRepo)
	moduleRegistryService.SetDeploymentDiscovery(&service.DeploymentDiscoveryService{Directory: cfg.DeploymentDiscoveryDirectory})
	taskProviderService := service.NewTaskProviderService(moduleRegistryService)
	moduleRegistryHandler := NewModuleRegistryHandler(moduleRegistryService, cfg.EncryptionKey)
	cleanupService := service.NewCleanupOrchestratorService(db, moduleRegistryService)
	engineService = engineService.WithCleanupOrchestrator(cleanupService)

	router.Use(corsMiddleware(cfg))

	router.Use(middleware.LoggerMiddleware(auditWriter))
	router.Use(i18nmiddleware.I18nMiddleware())

	lifecycle := modulelifecycle.NewStandalone(
		"system",
		modulelifecycle.StaticCheck("local_dependencies", true, ""),
		modulelifecycle.StaticCheck("iam_bootstrap", true, ""),
	)
	lifecycle.RegisterHealthRoutes(router)
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	router.Use(lifecycle.RequireReady())

	router.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": cfg.ProjectName, "name_en": "All Domain Data Platform"})
	})

	api := router.Group("/api/v1/system")
	if err := RegisterHostNodeRoutes(api, runtime, &HostNodeHandler{service: service.NewHostNodeService(repository.NewHostNodeRepository(db))}); err != nil {
		panic(err)
	}
	if err := RegisterEngineDeploymentRoutes(api, runtime, &EngineDeploymentHandler{service: service.NewEngineDeploymentService(repository.NewEngineDeploymentRepository(db)), discovery: &service.DeploymentDiscoveryService{Directory: cfg.DeploymentDiscoveryDirectory}}); err != nil {
		panic(err)
	}

	if err := RegisterEngineRasterPolicyRoutes(api, runtime, &EngineRasterPolicyHandler{
		service: service.NewEngineRasterPolicyService(repository.NewEngineRasterPolicyRepository(db)),
	}); err != nil {
		panic(fmt.Errorf("register raster resource policy routes: %w", err))
	}
	fulfillmentService := engineaccess.NewService(engineaccess.NewRepository(db), nil).
		WithExecutionEngineSnapshot(engineService.DecryptAuthorizedExecutionSnapshot)
	authorizationFactsService := engineaccess.NewService(engineaccess.NewRepository(db), nil)
	var metaFacts engineaccess.AuthorizationFactsReader
	var metaIdentities engineaccess.DataItemIdentityReader
	var metaScanFacts engineaccess.MetaScanFactsReader
	if err := RegisterIAMManagerProfileAuthorizationRoutes(api, runtime, &IAMManagerProfileAuthorizationHandler{
		service: iam.NewManagerProfileAuthorizationService(runtime.Repository, fulfillmentService.PrepareExecutionSourceRead),
	}); err != nil {
		panic(err)
	}
	if err := RegisterEngineAccessSourceReadCheckRoutes(api, runtime, &EngineAccessSourceReadCheckHandler{service: fulfillmentService}); err != nil {
		panic(err)
	}
	if err := RegisterIAMManagerModel3DAuthorizationRoutes(api, runtime, &IAMManagerModel3DAuthorizationHandler{
		service: iam.NewManagerModel3DAuthorizationService(runtime.Repository, fulfillmentService.PrepareExecutionSourceRead),
	}); err != nil {
		panic(err)
	}
	if secret := cfg.ServiceClientSecrets["addp-system"]; secret != "" {
		tokens, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemServiceURL, "addp-system", secret, nil)
		if err != nil {
			panic("invalid System fulfillment credential configuration")
		}
		fulfillmentService.WithFulfillmentBasisReader(commonClient.NewCatalogFulfillmentClient(cfg.CatalogServiceURL, tokens, nil))
		metaClient := commonClient.NewMetaClient(cfg.MetaServiceURL, tokens)
		metaFacts, metaScanFacts, metaIdentities = metaClient, metaClient, metaClient
		authorizationFactsService.WithAuthorizationFactsReader(metaFacts).WithDataItemIdentityReader(metaIdentities)
		fulfillmentService.WithAuthorizationFactsReader(metaFacts)
		fulfillmentService.WithTransferProductionReceiptReader(commonClient.NewTransferClient(cfg.TransferServiceURL, tokens))
	}
	if err := RegisterEngineAccessAuthorizationFactsRoutes(api, runtime, &EngineAccessAuthorizationFactsHandler{service: authorizationFactsService}); err != nil {
		panic(err)
	}
	if err := RegisterMetaCollectionRoutes(api, runtime, &MetaCollectionHandler{service: engineaccess.NewService(engineaccess.NewRepository(db), nil).WithMetaScanFactsReader(metaScanFacts)}); err != nil {
		panic(err)
	}
	if err := RegisterEngineAccessFulfillmentAcceptanceRoutes(api, runtime, &EngineAccessFulfillmentAcceptanceHandler{service: fulfillmentService}); err != nil {
		panic(err)
	}
	if err := RegisterEngineAccessFulfillmentGrantRoutes(api, runtime, &EngineAccessFulfillmentGrantHandler{service: fulfillmentService}); err != nil {
		panic(err)
	}
	if err := RegisterEngineAccessInitialGrantRoutes(api, runtime, &EngineAccessInitialGrantHandler{service: fulfillmentService}); err != nil {
		panic(err)
	}
	if err := RegisterEngineAccessHandlingScopeRoutes(api, runtime, &EngineAccessHandlingScopeHandler{
		service: engineaccess.NewService(engineaccess.NewRepository(db), hasLiveEngineCatalogCapability).WithAuthorizationFactsReader(metaFacts),
	}); err != nil {
		panic(err)
	}
	if err := RegisterEngineAccessFulfillmentRecoveryRoutes(api, runtime, &EngineAccessFulfillmentRecoveryHandler{
		service: engineaccess.NewService(engineaccess.NewRepository(db), nil),
	}); err != nil {
		panic(err)
	}
	if err := RegisterEngineAccessDirectPermissionRoutes(api, runtime, &EngineAccessDirectPermissionHandler{
		service: engineaccess.NewService(engineaccess.NewRepository(db), hasLiveEngineCatalogCapability).WithAuthorizationFactsReader(metaFacts).WithDataItemIdentityReader(metaIdentities),
	}); err != nil {
		panic(fmt.Errorf("register engine direct permission routes: %w", err))
	}
	if err := registerExecutionReadScopeRoute(api, runtime); err != nil {
		panic(err)
	}
	if err := RegisterEngineAccessDelegationRoutes(api, runtime, NewEngineAccessDelegationHandler(
		engineaccess.NewService(engineaccess.NewRepository(db), hasLiveEngineCatalogCapability),
	)); err != nil {
		panic(fmt.Errorf("注册引擎授权管理委派路由失败: %w", err))
	}
	if err := RegisterEngineAccessGrantRoutes(api, runtime, &EngineAccessGrantHandler{
		service: engineaccess.NewService(engineaccess.NewRepository(db), hasLiveEngineCatalogCapability).
			WithAuthorizationFactsReader(metaFacts).
			WithIndependentTargetVerifier(service.NewIndependentGrantTargetVerifier(engineService)),
	}); err != nil {
		panic(fmt.Errorf("register engine grant routes: %w", err))
	}
	if err := RegisterEngineAccessDenyRoutes(api, runtime, &EngineAccessDenyHandler{
		service: engineaccess.NewService(engineaccess.NewRepository(db), nil).WithAuthorizationFactsReader(metaFacts),
	}); err != nil {
		panic(fmt.Errorf("register engine deny routes: %w", err))
	}
	if err := RegisterIAMRoutes(api, runtime, redisClient); err != nil {
		panic(fmt.Errorf("注册 IAM 路由失败: %w", err))
	}
	if err := RegisterIAMEmergencySessionRoutes(api, runtime); err != nil {
		panic(fmt.Errorf("register emergency session routes: %w", err))
	}
	if err := RegisterIAMManagementRoutes(api, runtime, moduleRegistryHandler); err != nil {
		panic(fmt.Errorf("注册 IAM 管理路由失败: %w", err))
	}
	if err := RegisterIAMMigratedBusinessRoutes(
		api,
		runtime,
		engineHandler,
		NewAPIConsumerHandler(apiConsumerService),
		NewCleanupHandler(cleanupService),
	); err != nil {
		panic(fmt.Errorf("注册 IAM 业务路由失败: %w", err))
	}
	if err := RegisterIAMServiceRuntimeRoutes(
		api,
		runtime,
		moduleRegistryHandler,
		NewTaskProviderHandler(taskProviderService),
		engineHandler,
	); err != nil {
		panic(fmt.Errorf("注册 IAM Service Runtime 路由失败: %w", err))
	}
	if err := RegisterObservabilityIdentityRoute(api, runtime, moduleRegistryHandler); err != nil {
		panic(fmt.Errorf("注册观测身份投影路由失败: %w", err))
	}
	if err := RegisterPlatformPublicationCheckRoute(api, runtime); err != nil {
		panic(fmt.Errorf("注册平台定义发布核验路由失败: %w", err))
	}
	serviceInternalHandler := NewInternalHandler(apiConsumerService)
	platformContext, err := middleware.NewIAMServiceContextGuard("platform")
	if err != nil {
		panic(fmt.Errorf("创建 API Key 平台上下文守卫失败: %w", err))
	}
	apiConsumerCredentialValidate, err := middleware.NewIAMPermissionGuard("iam.api_consumer_runtime.read")
	if err != nil {
		panic(fmt.Errorf("创建 API 消费凭据校验权限守卫失败: %w", err))
	}
	api.GET("/runtime/api-consumer-credentials/validate", runtime.Authentication, runtime.ServiceCredential, platformContext, apiConsumerCredentialValidate, serviceInternalHandler.ValidateAPIConsumerCredential)
	configurationManagement := api.Group("/configuration-management")
	configurationManagement.Use(runtime.Authentication, runtime.UserAccessCredential)
	configurationManagement.GET("/entries", moduleRegistryHandler.ListConfigurationManagementEntries)

	return router
}

func corsMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		for _, allowedOrigin := range cfg.AllowedOrigins {
			if allowedOrigin == origin {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Credentials", "true")
				break
			}
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
