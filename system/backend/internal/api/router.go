package api

import (
	"context"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/system/internal/config"
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
	router := gin.Default()
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
	appRepo := repository.NewApplicationRepository(db)
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
	appService := service.NewApplicationService(appRepo)
	moduleRegistryService := service.NewModuleRegistryService(moduleRegistryRepo)
	taskProviderService := service.NewTaskProviderService(moduleRegistryService)
	moduleRegistryHandler := NewModuleRegistryHandler(moduleRegistryService)
	taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db)
	cleanupService := service.NewCleanupOrchestratorService(
		redisClient, taskExecutionRepo, auditWriter, moduleRegistryService,
	)
	engineService = engineService.WithCleanupOrchestrator(cleanupService)
	go func() {
		time.Sleep(5 * time.Second)
		if err := engineService.ResumeDeletingEngines(); err != nil {
			logger.L().Error("恢复未完成的引擎删除工作流失败", "error", err)
		}
	}()

	router.Use(corsMiddleware(cfg))
	router.Use(requestidmiddleware.RequestIDMiddleware())
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
	if err := RegisterIAMRoutes(api, runtime, redisClient); err != nil {
		panic(fmt.Errorf("注册 IAM 路由失败: %w", err))
	}
	if err := RegisterIAMManagementRoutes(api, runtime, moduleRegistryHandler); err != nil {
		panic(fmt.Errorf("注册 IAM 管理路由失败: %w", err))
	}
	if err := RegisterIAMMigratedBusinessRoutes(
		api,
		runtime,
		engineHandler,
		NewApplicationHandler(appService),
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
	serviceInternalHandler := NewInternalHandler(appService)
	platformContext, err := middleware.NewIAMServiceContextGuard("platform")
	if err != nil {
		panic(fmt.Errorf("创建 API Key 平台上下文守卫失败: %w", err))
	}
	apiKeyValidate, err := middleware.NewIAMPermissionGuard("system.api_key.read")
	if err != nil {
		panic(fmt.Errorf("创建 API Key 校验权限守卫失败: %w", err))
	}
	api.GET("/runtime/api-keys/validate", runtime.Authentication, runtime.ServiceCredential, platformContext, apiKeyValidate, serviceInternalHandler.ValidateAPIKeyService)
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
