package router

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/addp/common/buildinfo"
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
	serviceTokenSource, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemServiceURL, "addp-gateway", cfg.ServiceClientSecret, nil)
	if err != nil {
		log.Fatalf("Gateway Service Token Source 初始化失败: %v", err)
	}
	systemServiceClient := commonClient.NewSystemServiceClient(cfg.SystemServiceURL, serviceTokenSource, nil)
	systemClient := client.NewSystemClient(systemServiceClient)

	// 初始化本地缓存（5分钟 TTL）
	localCache := cache.NewLocalCache(5 * time.Minute)

	// 创建中间件实例
	apiKeyAuthMiddleware := middleware.NewAPIKeyAuthMiddleware(systemClient, localCache, redisClient)
	rateLimiterMiddleware := middleware.NewRateLimiterMiddleware(redisClient)
	accessLoggerMiddleware := middleware.NewAccessLoggerMiddleware(db)

	moduleDiscovery := internal.NewModuleDiscovery(systemClient)
	if err := moduleDiscovery.Start(cfg.ModuleWatchTimeout); err != nil {
		log.Printf("模块注册表初次加载失败，将通过 revision watch 继续重试: %v", err)
	}

	// 健康检查（无需鉴权）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, buildinfo.Health("gateway"))
	})

	// 网关首页（无需鉴权）
	router.GET("/", func(c *gin.Context) {
		response := gin.H{
			"message": "全域数据平台 API Gateway",
			"version": "1.0.0",
		}

		modules := moduleDiscovery.GetModules()
		moduleMap := map[string]string{"system": cfg.SystemServiceURL}
		for name, info := range modules {
			if name != "system" {
				if backend, ok := selectModuleBackend(info, time.Now()); ok {
					moduleMap[name] = backend
				}
			}
		}
		response["modules"] = moduleMap

		c.JSON(200, response)
	})

	// System 是模块注册表和认证上下文的 bootstrap 权威。
	systemProxy := proxy.NewServiceProxy(cfg.SystemServiceURL)
	serviceHandler := registeredModuleHandler("service", moduleDiscovery)

	// 查询服务数据访问端点（公开，无需 API Key，认证由 Service 模块内部判断）
	registerQueryServiceRoute(router, serviceHandler)
	router.POST("/api/gquery/:serviceName", serviceHandler)

	// OGC API Features 公开路由（不需要 API Key 认证）
	ogc := router.Group("/ogc")
	{
		ogc.GET("/features/:serviceName", serviceHandler)
		ogc.GET("/features/:serviceName/conformance", serviceHandler)
		ogc.GET("/features/:serviceName/collections", serviceHandler)
		ogc.GET("/features/:serviceName/collections/:collectionId/items", serviceHandler)
		ogc.GET("/features/:serviceName/collections/:collectionId/items/:featureId", serviceHandler)
	}

	// WMTS 公开路由（不需要 API Key 认证，认证由 Service 模块内部判断）
	wmts := router.Group("/wmts")
	{
		wmts.GET("/:serviceName", serviceHandler)
	}

	// OGC Tiles API 公开路由（添加到 ogc 组之外，避免路径冲突）
	ogcTiles := router.Group("/ogc/tiles")
	{
		ogcTiles.GET("/:serviceName", serviceHandler)
		ogcTiles.GET("/:serviceName/conformance", serviceHandler)
		ogcTiles.GET("/:serviceName/tileMatrixSets", serviceHandler)
		ogcTiles.GET("/:serviceName/tileMatrixSets/:tileMatrixSetId", serviceHandler)
		ogcTiles.GET("/:serviceName/tiles", serviceHandler)
		ogcTiles.GET("/:serviceName/tiles/:layer/:tileMatrixSetId/:tileMatrix/:tileRow/:tileCol", serviceHandler)
	}

	// XYZ Tiles 公开路由（不需要 API Key 认证，认证由 Service 模块内部判断）
	// 注意：Gin 不支持 :y.:format 语法，使用 /*yformat 通配符，由 Service 后端解析
	tiles := router.Group("/tiles")
	{
		tiles.GET("/:serviceName/:layerName/:z/:x/*yformat", serviceHandler)
	}

	// ============ 受保护的路由（需要 API Key 鉴权）============
	api := router.Group("/api/v1")
	api.Use(apiKeyAuthMiddleware.Handler())   // API Key 验证
	api.Use(accessLoggerMiddleware.Handler()) // API Key 访问日志，包裹限流器以记录 429
	api.Use(rateLimiterMiddleware.Handler())  // 限流
	{
		registerModuleRoutes(api, systemProxy, moduleDiscovery)
	}

	return router
}

func selectModuleBackend(module *commonClient.ModuleInfo, now time.Time) (string, bool) {
	if module == nil {
		return "", false
	}
	for _, instance := range module.Instances {
		if instance.Role == commonClient.ModuleRuntimeRoleBackend && instance.Status == "up" &&
			instance.LeaseExpiresAt.After(now) && instance.ModuleURL != "" {
			return instance.ModuleURL, true
		}
	}
	return "", false
}

func registerQueryServiceRoute(router gin.IRoutes, handler gin.HandlerFunc) {
	router.POST("/api/query/:serviceName/query", handler)
}

type moduleProxyLookup interface {
	GetProxy(moduleName string) (*proxy.ServiceProxy, error)
}

func registerModuleRoutes(api *gin.RouterGroup, systemProxy *proxy.ServiceProxy, discovery moduleProxyLookup) {
	handler := moduleRouteHandler(systemProxy, discovery)
	api.Any("/:module", handler)
	api.Any("/:module/*path", handler)
}

func moduleRouteHandler(systemProxy *proxy.ServiceProxy, discovery moduleProxyLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		moduleName := c.Param("module")
		if moduleName == "system" {
			systemProxy.Handle(c)
			return
		}
		proxyRegisteredModule(c, moduleName, discovery)
	}
}

func registeredModuleHandler(moduleName string, discovery moduleProxyLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		proxyRegisteredModule(c, moduleName, discovery)
	}
}

func proxyRegisteredModule(c *gin.Context, moduleName string, discovery moduleProxyLookup) {
	p, err := discovery.GetProxy(moduleName)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": fmt.Sprintf("module %s is unavailable", moduleName),
		})
		return
	}
	p.Handle(c)
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
	if err := db.AutoMigrate(&middleware.AccessLog{}); err != nil {
		log.Printf("Failed to migrate Gateway access log schema: %v (continuing without access log DB)", err)
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
