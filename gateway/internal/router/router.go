package router

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/httptrace"
	"github.com/addp/common/infra/bootstrap"

	"github.com/addp/common/middleware/cors"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/modulelifecycle"
	"github.com/addp/gateway/internal"
	"github.com/addp/gateway/internal/cache"
	"github.com/addp/gateway/internal/config"
	"github.com/addp/gateway/internal/middleware"
	"github.com/addp/gateway/internal/proxy"
	"github.com/addp/gateway/pkg/client"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func SetupRouter(cfg *config.Config, redisClient *redis.Client) (*gin.Engine, *commonClient.SystemServiceClient) {
	httptrace.MarkIngressBoundary()
	router := modulelifecycle.NewRouter()

	// CORS 中间件
	router.Use(cors.CORS())
	router.Use(i18nmiddleware.I18nMiddleware())

	// 初始化数据库连接
	db := initDatabase(cfg)

	// 初始化 System 客户端
	serviceTokenSource, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemServiceURL, "addp-gateway", cfg.ServiceClientSecret, nil)
	if err != nil {
		log.Fatalf("Gateway Service Token Source 初始化失败: %v", err)
	}
	systemServiceClient := commonClient.NewSystemServiceClient(cfg.SystemServiceURL, serviceTokenSource, nil)
	systemClient := client.NewSystemClient(systemServiceClient)

	// 消费凭据撤销需要快速生效，Gateway 只保留短期本地正缓存。
	localCache := cache.NewLocalCache(30 * time.Second)

	// 创建中间件实例
	apiConsumerAuthMiddleware := middleware.NewAPIConsumerAuthMiddleware(systemClient, localCache)
	rateLimiterMiddleware := middleware.NewRateLimiterMiddleware(redisClient)
	accessLoggerMiddleware := middleware.NewAccessLoggerMiddleware(db)

	moduleDiscovery := internal.NewModuleDiscovery(systemClient)
	if err := moduleDiscovery.Start(cfg.ModuleWatchTimeout); err != nil {
		log.Printf("模块注册表初次加载失败，将通过 revision watch 继续重试: %v", err)
	}

	lifecycle := modulelifecycle.NewStandalone("gateway", func(context.Context) modulelifecycle.CheckResult {
		if moduleDiscovery.HasSnapshot() {
			return modulelifecycle.CheckResult{Name: "system_registry_snapshot", Status: modulelifecycle.CheckReady}
		}
		return modulelifecycle.CheckResult{Name: "system_registry_snapshot", Status: modulelifecycle.CheckNotReady, ErrorCode: "system_registry_snapshot_unavailable"}
	}, modulelifecycle.RedisCheck(redisClient))
	lifecycle.RegisterHealthRoutes(router)
	router.Use(lifecycle.RequireReady())

	// 网关首页（无需鉴权）
	router.GET("/", func(c *gin.Context) {
		response := gin.H{
			"message": "全域数据平台 API Gateway",
			"version": "1.0.0",
		}

		moduleMap := moduleDiscovery.RouteAddresses()
		moduleMap["system"] = cfg.SystemServiceURL
		response["modules"] = moduleMap

		c.JSON(200, response)
	})

	// System 是模块注册表和认证上下文的 bootstrap 权威。
	systemProxy := proxy.NewServiceProxy(cfg.SystemServiceURL)
	serviceHandler := registeredModuleHandler("service", moduleDiscovery)

	// Query Service 是首期 API Consumer 数据面。公开请求或 Bearer 请求仍由 Service 判断；
	// 携带 X-API-Key 时，Gateway 先完成调用方认证、限流和访问日志。
	query := router.Group("/api/query")
	query.Use(apiConsumerAuthMiddleware.Handler())
	query.Use(accessLoggerMiddleware.Handler())
	query.Use(rateLimiterMiddleware.Handler())
	query.POST("/:serviceName/query", serviceHandler)
	router.POST("/api/gquery/:serviceName", serviceHandler)

	// OGC API Features 公开路由（不接受 API Consumer Credential）
	ogc := router.Group("/ogc")
	{
		ogc.GET("/features/:serviceName", serviceHandler)
		ogc.GET("/features/:serviceName/conformance", serviceHandler)
		ogc.GET("/features/:serviceName/collections", serviceHandler)
		ogc.GET("/features/:serviceName/collections/:collectionId/items", serviceHandler)
		ogc.GET("/features/:serviceName/collections/:collectionId/items/:featureId", serviceHandler)
	}

	// WMTS 公开路由（认证由 Service 模块内部判断）
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

	// XYZ Tiles 公开路由（认证由 Service 模块内部判断）
	// 注意：Gin 不支持 :y.:format 语法，使用 /*yformat 通配符，由 Service 后端解析
	tiles := router.Group("/tiles")
	{
		tiles.GET("/:serviceName/:layerName/:z/:x/*yformat", serviceHandler)
	}

	// 控制面只接受各 owner 路由声明的 Bearer Credential，显式拒绝 API Consumer Credential。
	api := router.Group("/api/v1")
	api.Use(middleware.RejectAPIConsumerCredentialOnControlPlane())
	{
		registerModuleRoutes(api, systemProxy, moduleDiscovery)
	}

	return router, systemServiceClient
}

type moduleProxyLookup interface {
	AlternateProxy(moduleName, excludeURL string) *proxy.ServiceProxy
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
	r := c.Request
	if moduleName == "catalog" && r.Method == http.MethodGet && r.URL.EscapedPath() == "/api/v1/catalog/entries" &&
		(r.Body == nil || r.Body == http.NoBody) && r.Header.Get("Upgrade") == "" {
		alternate := func() *proxy.ServiceProxy { return discovery.AlternateProxy(moduleName, p.GetTargetURL()) }
		if alternate() != nil {
			p.HandleReadWithAlternate(c, alternate)
			return
		}
	}
	p.Handle(c)
}

// initDatabase 初始化数据库连接
func initDatabase(cfg *config.Config) *gorm.DB {
	db, err := bootstrap.OpenPlatformRuntime(cfg.Database, "gateway", &gorm.Config{})
	if err != nil {
		log.Printf("Failed to connect to database: %v (continuing without DB)", err)
		return nil
	}
	if err := middleware.RequireAccessLog(db); err != nil {
		if pool, poolErr := db.DB(); poolErr == nil {
			_ = pool.Close()
		}
		log.Printf("Failed to verify Gateway access log schema: %v (continuing without access log DB)", err)
		return nil
	}

	log.Println("Database connected successfully")
	return db
}
