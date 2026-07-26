package api

import (
	"fmt"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
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

	runtime, err := NewIAMRuntime(db, cfg)
	if err != nil {
		panic(fmt.Errorf("装配目标 IAM Runtime 失败: %w", err))
	}
	auditWriter := iam.NewAuditWriter(runtime.Repository)

	engineRepo := repository.NewEngineRepository(db)
	appRepo := repository.NewApplicationRepository(db)
	moduleRegistryRepo := repository.NewModuleRegistryRepository(db)
	engineService := service.NewEngineService(engineRepo, cfg.EncryptionKey, redisClient)
	registryService := service.NewRegistryService(engineRepo)
	appService := service.NewApplicationService(appRepo)
	taskProviderService := service.NewTaskProviderService(db)
	moduleRegistryService := service.NewModuleRegistryService(moduleRegistryRepo)
	taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db)
	cleanupService := service.NewCleanupOrchestratorService(
		redisClient, taskExecutionRepo, auditWriter, moduleRegistryService,
	)
	engineService = engineService.WithCleanupOrchestrator(cleanupService)

	router.Use(corsMiddleware(cfg))
	router.Use(requestidmiddleware.RequestIDMiddleware())
	router.Use(middleware.LoggerMiddleware(auditWriter))
	router.Use(i18nmiddleware.I18nMiddleware())

	router.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": cfg.ProjectName, "name_en": "All Domain Data Platform"})
	})
	router.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	api := router.Group("/api/v1/system")
	if err := RegisterIAMRoutes(api, runtime, redisClient, cfg); err != nil {
		panic(fmt.Errorf("注册 IAM 路由失败: %w", err))
	}
	if err := RegisterIAMManagementRoutes(api, runtime); err != nil {
		panic(fmt.Errorf("注册 IAM 管理路由失败: %w", err))
	}
	if err := RegisterIAMMigratedBusinessRoutes(
		api,
		runtime,
		NewEngineHandler(engineService),
		NewApplicationHandler(appService),
		NewCleanupHandler(cleanupService),
	); err != nil {
		panic(fmt.Errorf("注册 IAM 业务路由失败: %w", err))
	}

	internal := router.Group("/api/v1/internal")
	internal.Use(middleware.InternalAPIMiddleware(cfg))
	{
		internal.GET("/config", NewConfigHandler(cfg).GetSharedConfig)

		engineHandler := NewEngineHandler(engineService)
		internal.GET("/engines", engineHandler.ListInternal)
		internal.GET("/engines/:id", engineHandler.GetByIDInternal)
		internal.POST("/engines", engineHandler.CreateInternal)
		internal.PUT("/engines/:id", engineHandler.UpdateInternal)
		internal.POST("/engines/register", engineHandler.RegisterEngineInternal)
		internal.POST("/engines/:id/check-connection", engineHandler.TriggerConnectionCheckInternal)

		registry := internal.Group("/registry")
		registryHandler := NewRegistryHandler(registryService, engineService)
		registry.POST("/capabilities", registryHandler.RegisterCapability)
		registry.GET("/capabilities", registryHandler.ListCapabilities)
		registry.GET("/compute-engines", registryHandler.ListComputeEngines)

		taskProviderHandler := NewTaskProviderHandler(taskProviderService)
		internal.POST("/task-providers/register", taskProviderHandler.RegisterOrUpdate)
		internal.GET("/task-providers", taskProviderHandler.List)
		internal.GET("/task-providers/:module_name", taskProviderHandler.Get)
		internal.POST("/audit-logs", runtime.InternalAuditHandler.Create)

		internalHandler := NewInternalHandler(appService)
		internal.GET("/api-keys/validate", internalHandler.ValidateAPIKey)
		internal.GET("/api-keys/bulk", internalHandler.BulkGetAPIKeys)

		moduleRegistryHandler := NewModuleRegistryHandler(moduleRegistryService)
		internal.POST("/modules/register", moduleRegistryHandler.Register)
		internal.POST("/modules/heartbeat", moduleRegistryHandler.Heartbeat)
		internal.GET("/modules", moduleRegistryHandler.ListModules)
		internal.GET("/modules/:name", moduleRegistryHandler.GetModule)
		internal.DELETE("/modules/:name", moduleRegistryHandler.DeleteModule)
	}

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
