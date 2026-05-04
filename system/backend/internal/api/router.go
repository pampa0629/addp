package api

import (
	"fmt"

	"github.com/addp/common/logger"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/config"
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

	// 初始化 Redis 客户端（可选，用于事件通知）
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
		logger.L().Warn("Redis 未配置，资源变更事件通知功能将被禁用")
	}

	// CORS 中间件（基于白名单）
	router.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		// 检查 origin 是否在白名单中
		allowed := false
		for _, allowedOrigin := range cfg.AllowedOrigins {
			if allowedOrigin == origin {
				allowed = true
				break
			}
		}

		// 只允许白名单中的 origin
		if allowed {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// 初始化 repositories
	userRepo := repository.NewUserRepository(db)
	logRepo := repository.NewLogRepository(db)
	engineRepo := repository.NewEngineRepository(db)
	tenantRepo := repository.NewTenantRepository(db)
	appRepo := repository.NewApplicationRepository(db)
	moduleRegistryRepo := repository.NewModuleRegistryRepository(db)

	// 初始化 services
	userService := service.NewUserService(userRepo)
	logService := service.NewLogService(logRepo, userRepo)
	engineService := service.NewEngineService(engineRepo, userRepo, cfg.EncryptionKey, redisClient)
	tenantService := service.NewTenantService(tenantRepo, userRepo, db)
	registryService := service.NewRegistryService(engineRepo)
	appService := service.NewApplicationService(appRepo)
	taskProviderService := service.NewTaskProviderService(db)
	cleanupOrchestratorService := service.NewCleanupOrchestratorService(redisClient)
	moduleRegistryService := service.NewModuleRegistryService(moduleRegistryRepo)

	// 日志中间件
	router.Use(middleware.LoggerMiddleware(logService))

	// i18n 中间件（从 Accept-Language 头解析语言）
	router.Use(i18nmiddleware.I18nMiddleware())

	// 根路由
	router.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": cfg.ProjectName,
			"name_en": "All Domain Data Platform",
		})
	})
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Swagger 文档
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// API 路由组
	api := router.Group("/api/v1/system")
	{
		// 认证路由（不需要认证）
		authHandler := NewAuthHandler(userService, cfg)
		api.POST("/login", authHandler.Login)
		api.POST("/register", authHandler.Register)
		api.POST("/refresh", authHandler.Refresh) // Token 刷新端点

		// 需要认证的路由
		protected := api.Group("")
		protected.Use(middleware.AuthMiddleware(cfg))
		{
			// 用户管理
			users := protected.Group("/users")
			{
				userHandler := NewUserHandler(userService)
				users.POST("", userHandler.Create)
				users.GET("", userHandler.List)
				users.GET("/me", userHandler.Me)
				users.GET("/:id", userHandler.GetByID)
				users.PUT("/:id", userHandler.Update)
				users.PUT("/:id/change-password", userHandler.ChangePassword)
				users.DELETE("/:id", userHandler.Delete)
			}

			// 日志管理
			logs := protected.Group("/logs")
			{
				logHandler := NewLogHandler(logService)
				logs.GET("", logHandler.List)
				logs.GET("/stats", logHandler.GetStats)   // 统计数据（新增）
				logs.GET("/trends", logHandler.GetTrends) // 时间趋势（新增）
				logs.GET("/export", logHandler.Export)    // 导出日志
				logs.GET("/:id", logHandler.GetByID)
			}

			// 引擎管理
			engines := protected.Group("/engines")
			{
				engineHandler := NewEngineHandler(engineService)
				engines.POST("", engineHandler.Create)
				engines.GET("", engineHandler.List)
				engines.GET("/:id", engineHandler.GetByID)
				engines.PUT("/:id", engineHandler.Update)
				engines.DELETE("/:id", engineHandler.Delete)
				engines.POST("/:id/test", engineHandler.TestConnection)                    // 测试已有引擎连接
				engines.POST("/test-connection", engineHandler.TestConnectionBeforeCreate) // 创建前测试连接
				engines.GET("/:id/namespaces", engineHandler.ListNamespaces)               // 列出 catalog 命名空间
				engines.GET("/:id/items", engineHandler.ListCatalogItems)                  // 列出 catalog 数据项
				engines.POST("/:id/catalog/children", engineHandler.ListCatalogChildren)   // 列出实时 catalog 子节点
			}

			// 租户管理
			tenants := protected.Group("/tenants")
			{
				tenantHandler := NewTenantHandler(tenantService)
				tenants.POST("", tenantHandler.Create)
				tenants.GET("", tenantHandler.List)
				tenants.GET("/:id", tenantHandler.GetByID)
				tenants.PUT("/:id", tenantHandler.Update)
				tenants.DELETE("/:id", tenantHandler.Delete)
			}

			// 应用管理
			applications := protected.Group("/applications")
			{
				appHandler := NewApplicationHandler(appService)
				applications.POST("", appHandler.CreateApplication)
				applications.GET("", appHandler.ListApplications)
				applications.GET("/:id", appHandler.GetApplication)
				applications.PUT("/:id", appHandler.UpdateApplication)
				applications.DELETE("/:id", appHandler.DeleteApplication)
				applications.POST("/:id/keys", appHandler.GenerateAPIKey)
				applications.GET("/:id/keys", appHandler.ListAPIKeys)
				applications.DELETE("/:id/keys/:key_id", appHandler.RevokeAPIKey)
			}

			// 垃圾数据清理管理（仅租户管理员）
			cleanup := protected.Group("/admin/cleanup")
			{
				cleanupHandler := NewCleanupHandler(cleanupOrchestratorService)
				cleanup.POST("/scan", cleanupHandler.CreateScanTask)
				cleanup.GET("/tasks/:task_id", cleanupHandler.GetTaskStatus)
				cleanup.POST("/execute", cleanupHandler.CreateExecuteTask)
				cleanup.GET("/history", cleanupHandler.GetTaskHistory)
			}
		}
	}

	// 内部 API（用于服务间调用，使用 X-Internal-API-Key 认证）
	internal := router.Group("/api/v1/internal")
	internal.Use(middleware.InternalAPIMiddleware(cfg))
	{
		configHandler := NewConfigHandler(cfg)
		internal.GET("/config", configHandler.GetSharedConfig)

		// 服务间调用的引擎API
		engineHandler := NewEngineHandler(engineService)
		internal.GET("/engines", engineHandler.ListInternal)
		internal.GET("/engines/:id", engineHandler.GetByIDInternal)
		internal.POST("/engines", engineHandler.CreateInternal)
		internal.POST("/engines/register", engineHandler.RegisterEngineInternal) // 引擎自注册
		internal.PUT("/engines/:id/connection-status", engineHandler.UpdateConnectionStatusInternal)
		internal.POST("/engines/:id/check-connection", engineHandler.TriggerConnectionCheckInternal) // 触发异步连接检测

		// 能力注册 API
		registry := internal.Group("/registry")
		{
			registryHandler := NewRegistryHandler(registryService, engineService)
			registry.POST("/capabilities", registryHandler.RegisterCapability)
			registry.GET("/capabilities", registryHandler.ListCapabilities)
			registry.GET("/capabilities/:identifier", registryHandler.GetCapabilityByIdentifier)
			registry.GET("/compute-engines", registryHandler.ListComputeEngines)
		}

		// 任务提供者注册 API（供模块启动时自注册使用）
		taskProviderHandler := NewTaskProviderHandler(taskProviderService)
		internal.POST("/task-providers/register", taskProviderHandler.RegisterOrUpdate)
		internal.GET("/task-providers", taskProviderHandler.List)

		// 审计日志 API（供其他模块记录审计日志）
		logHandler := NewLogHandler(logService)
		internal.POST("/audit-logs", logHandler.CreateFromInternal)
		internal.GET("/task-providers/:module_name", taskProviderHandler.Get)

		// API Key 验证 API（供 Gateway 调用）
		internalHandler := NewInternalHandler(appService)
		internal.GET("/api-keys/validate", internalHandler.ValidateAPIKey)
		internal.GET("/api-keys/bulk", internalHandler.BulkGetAPIKeys)

		// 模块注册与发现 API（供各模块注册和 Gateway 查询）
		moduleRegistryHandler := NewModuleRegistryHandler(moduleRegistryService)
		internal.POST("/modules/register", moduleRegistryHandler.Register)
		internal.POST("/modules/heartbeat", moduleRegistryHandler.Heartbeat)
		internal.GET("/modules", moduleRegistryHandler.ListModules)
		internal.GET("/modules/:name", moduleRegistryHandler.GetModule)
		internal.DELETE("/modules/:name", moduleRegistryHandler.DeleteModule)
	}

	return router
}
