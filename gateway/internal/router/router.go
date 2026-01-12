package router

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/addp/common/middleware/cors"
	"github.com/addp/gateway/internal/cache"
	"github.com/addp/gateway/internal/config"
	"github.com/addp/gateway/internal/middleware"
	"github.com/addp/gateway/internal/proxy"
	"github.com/addp/gateway/pkg/client"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func SetupRouter(cfg *config.Config) *gin.Engine {
	router := gin.Default()

	// CORS 中间件
	router.Use(cors.CORS())

	// 初始化数据库连接
	db := initDatabase(cfg)

	// 初始化 Redis 连接
	redisClient := initRedis(cfg)

	// 初始化 System 客户端
	systemClient := client.NewSystemClient(cfg.SystemServiceURL, cfg.InternalAPIKey)

	// 初始化本地缓存（5分钟 TTL）
	localCache := cache.NewLocalCache(5 * time.Minute)

	// 创建中间件实例
	apiKeyAuthMiddleware := middleware.NewAPIKeyAuthMiddleware(systemClient, localCache, redisClient)
	rateLimiterMiddleware := middleware.NewRateLimiterMiddleware(redisClient)
	accessLoggerMiddleware := middleware.NewAccessLoggerMiddleware(db)

	// 健康检查（无需鉴权）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "ok",
			"service": "gateway",
		})
	})

	// 网关首页（无需鉴权）
	router.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "全域数据平台 API Gateway",
			"version": "1.0.0",
			"services": gin.H{
				"system":   cfg.SystemServiceURL,
				"manager":  cfg.ManagerServiceURL,
				"meta":     cfg.MetaServiceURL,
				"transfer": cfg.TransferServiceURL,
				"develop":  cfg.DevelopServiceURL,
				"service":  cfg.ServiceServiceURL,
				"copilot":  cfg.CopilotServiceURL,
			},
		})
	})

	// 创建代理
	systemProxy := proxy.NewServiceProxy(cfg.SystemServiceURL)
	managerProxy := proxy.NewServiceProxy(cfg.ManagerServiceURL)
	metaProxy := proxy.NewServiceProxy(cfg.MetaServiceURL)
	transferProxy := proxy.NewServiceProxy(cfg.TransferServiceURL)
	developProxy := proxy.NewServiceProxy(cfg.DevelopServiceURL)
	serviceProxy := proxy.NewServiceProxy(cfg.ServiceServiceURL)
	copilotProxy := proxy.NewServiceProxy(cfg.CopilotServiceURL)

	// 公开路由（无需鉴权）- 主要是认证相关接口
	public := router.Group("/api")
	{
		// System 模块的认证接口（登录、注册）
		public.POST("/system/login", systemProxy.Handle)
		public.POST("/system/register", systemProxy.Handle)
	}

	// 受保护的路由（需要 API Key 鉴权）
	api := router.Group("/api")
	api.Use(apiKeyAuthMiddleware.Handler())      // API Key 验证
	api.Use(rateLimiterMiddleware.Handler())     // 限流
	api.Use(accessLoggerMiddleware.Handler())    // 访问日志
	{
		// ============ System 模块 ============
		systemGroup := api.Group("/system")
		{
			systemGroup.Any("/users", systemProxy.Handle)
			systemGroup.Any("/users/*path", systemProxy.Handle)
			systemGroup.Any("/tenants", systemProxy.Handle)
			systemGroup.Any("/tenants/*path", systemProxy.Handle)
			systemGroup.Any("/engines", systemProxy.Handle)
			systemGroup.Any("/engines/*path", systemProxy.Handle)
			systemGroup.Any("/logs", systemProxy.Handle)
			systemGroup.Any("/logs/*path", systemProxy.Handle)
			systemGroup.Any("/applications", systemProxy.Handle)
			systemGroup.Any("/applications/*path", systemProxy.Handle)
			systemGroup.Any("/api-docs/*path", systemProxy.Handle)
			systemGroup.Any("/admin/*path", systemProxy.Handle)
		}

		// ============ Manager 模块 ============
		// 使用路径重写，将 /api/manager/* 重写为 /api/*
		managerGroup := api.Group("/manager")
		{
			managerGroup.Any("/engines", managerProxy.HandleWithPathRewrite("/manager"))
			managerGroup.Any("/engines/*path", managerProxy.HandleWithPathRewrite("/manager"))
			managerGroup.Any("/preview", managerProxy.HandleWithPathRewrite("/manager"))
			managerGroup.Any("/preview/*path", managerProxy.HandleWithPathRewrite("/manager"))
			managerGroup.Any("/tree/*path", managerProxy.HandleWithPathRewrite("/manager"))
			managerGroup.Any("/search", managerProxy.HandleWithPathRewrite("/manager"))
			managerGroup.Any("/search/*path", managerProxy.HandleWithPathRewrite("/manager"))
			managerGroup.Any("/operators", managerProxy.HandleWithPathRewrite("/manager"))
			managerGroup.Any("/operators/*path", managerProxy.HandleWithPathRewrite("/manager"))
			managerGroup.Any("/tasks", managerProxy.HandleWithPathRewrite("/manager"))
			managerGroup.Any("/tasks/*path", managerProxy.HandleWithPathRewrite("/manager"))
			managerGroup.Any("/mvt/*path", managerProxy.HandleWithPathRewrite("/manager"))
			managerGroup.Any("/embedding/*path", managerProxy.HandleWithPathRewrite("/manager"))
			managerGroup.Any("/video-stream", managerProxy.HandleWithPathRewrite("/manager"))
			managerGroup.Any("/config/*path", managerProxy.HandleWithPathRewrite("/manager"))
			managerGroup.Any("/pre-cache/*path", managerProxy.HandleWithPathRewrite("/manager"))
			managerGroup.Any("/spatial/*path", managerProxy.HandleWithPathRewrite("/manager"))
		}

		// ============ Meta 模块 ============
		metaGroup := api.Group("/meta")
		{
			metaGroup.Any("/engines", metaProxy.Handle)
			metaGroup.Any("/engines/*path", metaProxy.Handle)
			metaGroup.Any("/operators", metaProxy.Handle)
			metaGroup.Any("/operators/*path", metaProxy.Handle)
			metaGroup.Any("/scan/*path", metaProxy.Handle)
			metaGroup.Any("/object-storage/*path", metaProxy.Handle)
		}

		// ============ Transfer 模块 ============
		transferGroup := api.Group("/transfer")
		{
			transferGroup.Any("/tasks", transferProxy.Handle)
			transferGroup.Any("/tasks/*path", transferProxy.Handle)
			transferGroup.Any("/executions", transferProxy.Handle)
			transferGroup.Any("/executions/*path", transferProxy.Handle)
			transferGroup.Any("/connections", transferProxy.Handle)
			transferGroup.Any("/connections/*path", transferProxy.Handle)
		}

		// ============ Develop 模块 ============
		developGroup := api.Group("/develop")
		{
			developGroup.Any("/engines", developProxy.Handle)
			developGroup.Any("/engines/*path", developProxy.Handle)
			developGroup.Any("/sql", developProxy.Handle)
			developGroup.Any("/sql/*path", developProxy.Handle)
			developGroup.Any("/operators", developProxy.Handle)
			developGroup.Any("/operators/*path", developProxy.Handle)
			developGroup.Any("/workflows", developProxy.Handle)
			developGroup.Any("/workflows/*path", developProxy.Handle)
		}

		// ============ Service 模块 ============
		serviceGroup := api.Group("/service")
		{
			serviceGroup.Any("/services", serviceProxy.Handle)
			serviceGroup.Any("/services/*path", serviceProxy.Handle)
			serviceGroup.Any("/ogc/*path", serviceProxy.Handle)
		}

		// ============ Copilot 模块（暂不加前缀，保持原样）============
		api.Any("/copilot/*path", copilotProxy.HandleWithPathRewrite("/api"))

		// ============ 内部 API（跨模块调用，无需模块前缀）============
		internalGroup := api.Group("/internal")
		{
			// System 内部 API
			internalGroup.Any("/engines", systemProxy.Handle)
			internalGroup.Any("/engines/*path", systemProxy.Handle)
			internalGroup.Any("/users/*path", systemProxy.Handle)
			internalGroup.Any("/tenants/*path", systemProxy.Handle)
		}
	}

	return router
}

// initDatabase 初始化数据库连接
func initDatabase(cfg *config.Config) *gorm.DB {
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSchema)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Printf("Failed to connect to database: %v (continuing without DB)", err)
		return nil
	}

	log.Println("Database connected successfully")
	return db
}

// initRedis 初始化 Redis 连接
func initRedis(cfg *config.Config) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	// 测试连接
	ctx := context.Background()
	if _, err := client.Ping(ctx).Result(); err != nil {
		log.Printf("Failed to connect to Redis: %v (continuing without Redis)", err)
		return nil
	}

	log.Println("Redis connected successfully")
	return client
}
