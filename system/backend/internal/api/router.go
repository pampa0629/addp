package api

import (
	"fmt"
	"log"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
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
		log.Printf("✅ Redis 客户端已初始化: %s", redisAddr)
	} else {
		log.Println("⚠️  Redis 未配置，资源变更事件通知功能将被禁用")
	}

	// CORS
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
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
	resourceRepo := repository.NewResourceRepository(db)
	tenantRepo := repository.NewTenantRepository(db)
	appRepo := repository.NewApplicationRepository(db)

	// 初始化 services
	userService := service.NewUserService(userRepo)
	logService := service.NewLogService(logRepo, userRepo)
	resourceService := service.NewResourceService(resourceRepo, userRepo, cfg.EncryptionKey, redisClient)
	tenantService := service.NewTenantService(tenantRepo, userRepo, db)
	registryService := service.NewRegistryService(resourceRepo)
	appService := service.NewApplicationService(appRepo)
	taskProviderService := service.NewTaskProviderService(db)

	// 日志中间件
	router.Use(middleware.LoggerMiddleware(logService, userRepo))

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

	// API 路由组
	api := router.Group("/api")
	{
		// 认证路由（不需要认证）
		auth := api.Group("/auth")
		{
			authHandler := NewAuthHandler(userService, cfg)
			auth.POST("/login", authHandler.Login)
			auth.POST("/register", authHandler.Register)
			auth.POST("/refresh", authHandler.Refresh) // Token 刷新端点
		}

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
				logs.GET("/:id", logHandler.GetByID)
			}

			// 资源管理
			resources := protected.Group("/resources")
			{
				resourceHandler := NewResourceHandler(resourceService)
				resources.POST("", resourceHandler.Create)
				resources.GET("", resourceHandler.List)
				resources.GET("/:id", resourceHandler.GetByID)
				resources.PUT("/:id", resourceHandler.Update)
				resources.DELETE("/:id", resourceHandler.Delete)
				resources.POST("/:id/test", resourceHandler.TestConnection)                    // 测试已有资源连接
				resources.POST("/test-connection", resourceHandler.TestConnectionBeforeCreate) // 创建前测试连接
				resources.GET("/:id/schemas", resourceHandler.ListSchemas)                     // 列出schemas
				resources.GET("/:id/tables", resourceHandler.ListTables)                       // 列出表
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
		}
	}

	// 内部 API（用于服务间调用，使用 X-Internal-API-Key 认证）
	internal := router.Group("/internal")
	internal.Use(middleware.InternalAPIMiddleware(cfg))
	{
		configHandler := NewConfigHandler(cfg)
		internal.GET("/config", configHandler.GetSharedConfig)

		// 服务间调用的资源API
		resourceHandler := NewResourceHandler(resourceService)
		internal.GET("/resources", resourceHandler.ListInternal)
		internal.GET("/resources/:id", resourceHandler.GetByIDInternal)
		internal.POST("/resources", resourceHandler.CreateInternal)

		// 能力注册 API
		registry := internal.Group("/registry")
		{
			registryHandler := NewRegistryHandler(registryService)
			registry.POST("/capabilities", registryHandler.RegisterCapability)
			registry.GET("/capabilities", registryHandler.ListCapabilities)
			registry.GET("/capabilities/:identifier", registryHandler.GetCapabilityByIdentifier)
			registry.GET("/compute-resources", registryHandler.ListComputeResources)
		}

		// 任务提供者注册 API（供模块启动时自注册使用）
		taskProviderHandler := NewTaskProviderHandler(taskProviderService)
		internal.POST("/task-providers/register", taskProviderHandler.RegisterOrUpdate)
		internal.GET("/task-providers", taskProviderHandler.List)
		internal.GET("/task-providers/:module_name", taskProviderHandler.Get)

		// API Key 验证 API（供 Gateway 调用）
		internalHandler := NewInternalHandler(appService)
		internal.GET("/api-keys/validate", internalHandler.ValidateAPIKey)
		internal.GET("/api-keys/bulk", internalHandler.BulkGetAPIKeys)
	}

	return router
}
