package router

import (
	"context"
	"fmt"
	"log"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/middleware/cors"
	"github.com/addp/gateway/internal"
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

	// 初始化模块发现（如果启用）
	var moduleDiscovery *internal.ModuleDiscovery
	if cfg.ModuleRegistryEnabled {
		log.Println("🔍 启用模块发现，从 System 加载模块列表...")

		// 创建用于模块发现的 System 客户端
		registryClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
		moduleDiscovery = internal.NewModuleDiscovery(registryClient, cfg.ModuleRefreshInterval)

		// 启动模块发现
		if err := moduleDiscovery.Start(cfg.ModuleRefreshInterval); err != nil {
			log.Printf("⚠️ 模块发现启动失败，回退到硬编码路由: %v", err)
			moduleDiscovery = nil // 回退到硬编码路由
		} else {
			log.Println("✅ 模块发现已启动")
		}
	} else {
		log.Println("ℹ️  模块发现已禁用，使用硬编码路由")
	}

	// 健康检查（无需鉴权）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "ok",
			"service": "gateway",
		})
	})

	// 网关首页（无需鉴权）
	router.GET("/", func(c *gin.Context) {
		response := gin.H{
			"message": "全域数据平台 API Gateway",
			"version": "1.0.0",
		}

		// 如果启用了模块发现，显示动态模块列表
		if moduleDiscovery != nil {
			modules := moduleDiscovery.GetModules()
			moduleMap := make(map[string]string)
			for name, info := range modules {
				moduleMap[name] = info.ModuleURL
			}
			response["modules"] = moduleMap
			response["module_discovery"] = "enabled"
		} else {
			// 使用硬编码服务列表
			response["services"] = gin.H{
				"system":   cfg.SystemServiceURL,
				"manager":  cfg.ManagerServiceURL,
				"meta":     cfg.MetaServiceURL,
				"transfer": cfg.TransferServiceURL,
				"develop":  cfg.DevelopServiceURL,
				"service":  cfg.ServiceServiceURL,
				"copilot":  cfg.CopilotServiceURL,
			}
			response["module_discovery"] = "disabled"
		}

		c.JSON(200, response)
	})

	// 创建硬编码代理（作为 fallback）
	systemProxy := proxy.NewServiceProxy(cfg.SystemServiceURL)
	managerProxy := proxy.NewServiceProxy(cfg.ManagerServiceURL)
	metaProxy := proxy.NewServiceProxy(cfg.MetaServiceURL)
	transferProxy := proxy.NewServiceProxy(cfg.TransferServiceURL)
	developProxy := proxy.NewServiceProxy(cfg.DevelopServiceURL)
	serviceProxy := proxy.NewServiceProxy(cfg.ServiceServiceURL)
	copilotProxy := proxy.NewServiceProxy(cfg.CopilotServiceURL)

	// ============ 公开路由（无需鉴权）============
	// 注意：这些路由必须在通配符路由之前定义，避免冲突
	public := router.Group("/api")
	{
		// System 模块的认证接口（登录、注册）
		if moduleDiscovery != nil {
			public.POST("/system/login", func(c *gin.Context) {
				if p, err := moduleDiscovery.GetProxy("system"); err == nil {
					p.Handle(c)
				} else {
					systemProxy.Handle(c) // fallback
				}
			})
			public.POST("/system/register", func(c *gin.Context) {
				if p, err := moduleDiscovery.GetProxy("system"); err == nil {
					p.Handle(c)
				} else {
					systemProxy.Handle(c) // fallback
				}
			})
		} else {
			public.POST("/system/login", systemProxy.Handle)
			public.POST("/system/register", systemProxy.Handle)
		}
	}

	// ============ 受保护的路由（需要 API Key 鉴权）============
	api := router.Group("/api")
	api.Use(apiKeyAuthMiddleware.Handler())      // API Key 验证
	api.Use(rateLimiterMiddleware.Handler())     // 限流
	api.Use(accessLoggerMiddleware.Handler())    // 访问日志
	{
		// 如果启用了模块发现，使用动态路由
		if moduleDiscovery != nil {
			log.Println("✅ 使用动态路由")

			// 动态路由：自动支持所有注册的模块
			api.Any("/:module", func(c *gin.Context) {
				moduleName := c.Param("module")

				// 从模块发现获取代理
				p, err := moduleDiscovery.GetProxy(moduleName)
				if err != nil {
					// Fallback 到硬编码路由
					switch moduleName {
					case "system":
						systemProxy.Handle(c)
					case "manager":
						managerProxy.Handle(c)
					case "meta":
						metaProxy.Handle(c)
					case "transfer":
						transferProxy.Handle(c)
					case "develop":
						developProxy.Handle(c)
					case "service":
						serviceProxy.Handle(c)
					case "copilot":
						copilotProxy.HandleWithPathRewrite("/api")(c)
					default:
						c.JSON(503, gin.H{
							"error": fmt.Sprintf("模块 %s 不可用", moduleName),
						})
					}
					return
				}

				p.Handle(c)
			})

			api.Any("/:module/*path", func(c *gin.Context) {
				moduleName := c.Param("module")

				// 从模块发现获取代理
				p, err := moduleDiscovery.GetProxy(moduleName)
				if err != nil {
					// Fallback 到硬编码路由
					switch moduleName {
					case "system":
						systemProxy.Handle(c)
					case "manager":
						managerProxy.Handle(c)
					case "meta":
						metaProxy.Handle(c)
					case "transfer":
						transferProxy.Handle(c)
					case "develop":
						developProxy.Handle(c)
					case "service":
						serviceProxy.Handle(c)
					case "copilot":
						copilotProxy.HandleWithPathRewrite("/api")(c)
					default:
						c.JSON(503, gin.H{
							"error": fmt.Sprintf("模块 %s 不可用", moduleName),
						})
					}
					return
				}

				p.Handle(c)
			})
		} else {
			// 使用硬编码路由（原有逻辑保持不变）
			log.Println("ℹ️  使用硬编码路由")

			// System 模块 - 排除公开路由（login、register），其他全部转发
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

			// Manager 模块
			api.Any("/manager", managerProxy.Handle)
			api.Any("/manager/*path", managerProxy.Handle)

			// Meta 模块
			api.Any("/meta", metaProxy.Handle)
			api.Any("/meta/*path", metaProxy.Handle)

			// Transfer 模块
			api.Any("/transfer", transferProxy.Handle)
			api.Any("/transfer/*path", transferProxy.Handle)

			// Develop 模块
			api.Any("/develop", developProxy.Handle)
			api.Any("/develop/*path", developProxy.Handle)

			// Service 模块
			api.Any("/service", serviceProxy.Handle)
			api.Any("/service/*path", serviceProxy.Handle)

			// Copilot 模块
			api.Any("/copilot", copilotProxy.HandleWithPathRewrite("/api"))
			api.Any("/copilot/*path", copilotProxy.HandleWithPathRewrite("/api"))

			// 内部 API（跨模块调用）
			internalGroup := api.Group("/internal")
			{
				internalGroup.Any("/engines", systemProxy.Handle)
				internalGroup.Any("/engines/*path", systemProxy.Handle)
				internalGroup.Any("/users/*path", systemProxy.Handle)
				internalGroup.Any("/tenants/*path", systemProxy.Handle)
			}
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
