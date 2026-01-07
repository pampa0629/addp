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
		public.POST("/auth/login", systemProxy.Handle)
		public.POST("/auth/register", systemProxy.Handle)
	}

	// 受保护的路由（需要 API Key 鉴权）
	api := router.Group("/api")
	api.Use(apiKeyAuthMiddleware.Handler())      // API Key 验证
	api.Use(rateLimiterMiddleware.Handler())     // 限流
	api.Use(accessLoggerMiddleware.Handler())    // 访问日志
	{
		// System 模块路由（用户、日志、资源、应用管理）
		api.Any("/users/*path", systemProxy.Handle)
		api.Any("/logs/*path", systemProxy.Handle)
		api.Any("/engines/*path", systemProxy.Handle)
		api.Any("/applications/*path", systemProxy.Handle)

		// Manager 模块路由（配置、数据源、目录、预览、搜索）
		api.Any("/config/*path", managerProxy.Handle)
		api.Any("/datasources/*path", managerProxy.Handle)
		api.Any("/directories/*path", managerProxy.Handle)
		api.Any("/preview/*path", managerProxy.Handle)
		api.Any("/upload/*path", managerProxy.Handle)
		api.Any("/explorer/*path", managerProxy.Handle)  // 新版数据探查 API
		api.Any("/data-explorer/*path", managerProxy.Handle) // 旧版（保留向后兼容）
		api.Any("/search/*path", managerProxy.Handle)

		// Meta 模块路由（元数据、血缘）
		api.Any("/meta/*path", metaProxy.Handle)
		api.Any("/metadata/*path", metaProxy.Handle)
		api.Any("/datasets/*path", metaProxy.Handle)
		api.Any("/lineage/*path", metaProxy.Handle)
		api.Any("/scan/*path", metaProxy.Handle)

		// Transfer 模块路由（任务、传输、本地引擎）
		api.Any("/tasks", transferProxy.Handle)
		api.Any("/tasks/*path", transferProxy.Handle)
		api.Any("/executions", transferProxy.Handle)
		api.Any("/executions/*path", transferProxy.Handle)
		api.Any("/transfer/*path", transferProxy.Handle)
		api.Any("/local-engines", transferProxy.Handle)
		api.Any("/local-engines/*path", transferProxy.Handle)
		api.Any("/system-engines", transferProxy.Handle)
		api.Any("/operators", transferProxy.Handle)
		api.Any("/operators/*path", transferProxy.Handle)
		api.Any("/transforms", transferProxy.Handle)
		api.Any("/transforms/*path", transferProxy.Handle)
		api.Any("/mappings/*path", transferProxy.Handle)
		api.Any("/object-storage/*path", transferProxy.Handle)

		// Develop 模块路由（SQL 开发、脚本管理）
		api.Any("/develop/*path", developProxy.Handle)
		api.Any("/scripts/*path", developProxy.Handle)
		api.Any("/spatial/*path", developProxy.Handle)

		// Copilot 模块路由（AI 辅助）
		api.Any("/copilot/*path", copilotProxy.HandleWithPathRewrite("/api"))

		// Service 模块路由（数据服务、OGC 服务、外部服务注册）
		api.Any("/service/*path", serviceProxy.Handle)
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
