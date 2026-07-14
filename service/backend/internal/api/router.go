package api

import (
	"strconv"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/middleware/audit"
	authMiddleware "github.com/addp/common/middleware/auth"
	commonAuth "github.com/addp/common/middleware/auth"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/service/docs"
	_ "github.com/addp/service/i18n"
	"github.com/addp/service/internal/config"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"
)

func SetupRouter(
	cfg *config.Config,
	db *gorm.DB,
	dataServiceHandler *DataServiceHandler,
	queryServiceHandler *QueryServiceHandler,
	ogcFeaturesHandler *OGCFeaturesHandler,
	registeredServiceHandler *RegisteredServiceHandler,
	tileServiceHandler *TileServiceHandler,
	tileEndpointHandler *TileEndpointHandler,
	wmtsHandler *WMTSHandler,
	ogcTilesHandler *OGCTilesHandler,
	resourceCapabilityHandler *ResourceCapabilityHandler,
	serviceEndpointHandler *ServiceEndpointHandler,
	graphQueryHandler *GraphQueryHandler,
	systemClient *commonClient.SystemClient,
) *gin.Engine {
	router := gin.Default()

	// i18n 中间件（解析 Accept-Language 请求头）
	router.Use(i18nmiddleware.I18nMiddleware())

	// Swagger 文档
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 注意：CORS 由 Gateway 统一处理，此处无需设置 CORS 中间件

	// 健康检查端点（无需认证）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy", "service": "service"})
	})

	// 查询服务端点（支持公开访问，handler内部会检查权限）
	// 可选认证：有 token 就解析注入 tenant_id，没有就跳过（公开服务仍可访问）
	optionalAuth := func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			if tokenParam := c.Query("token"); tokenParam != "" {
				authHeader = "Bearer " + tokenParam
				c.Request.Header.Set("Authorization", authHeader)
			}
		}
		if authHeader != "" {
			authMiddleware.SystemAuthMiddleware(cfg.SystemServiceURL)(c)
			return
		}
		c.Next()
	}
	router.GET("/api/query/:serviceName", optionalAuth, queryServiceHandler.QueryData)

	// 图查询服务执行端点（支持公开访问）
	router.POST("/api/gquery/:serviceName", optionalAuth, graphQueryHandler.ExecuteQuery)

	// OGC API Features 端点（支持公开访问，handler内部会检查权限）
	router.GET("/ogc/features/:serviceName", ogcFeaturesHandler.GetLandingPage)
	router.GET("/ogc/features/:serviceName/conformance", ogcFeaturesHandler.GetConformance)
	router.GET("/ogc/features/:serviceName/collections", ogcFeaturesHandler.GetCollections)
	router.GET("/ogc/features/:serviceName/collections/:collectionId/items", ogcFeaturesHandler.GetItems)
	router.GET("/ogc/features/:serviceName/collections/:collectionId/items/:featureId", ogcFeaturesHandler.GetItem)

	// 代理服务端点（公开访问，无需认证）
	router.Any("/api/service/registered/proxy/:id/*path", registeredServiceHandler.ProxyService)

	// XYZ Tiles 端点（支持公开访问，handler内部会检查权限）
	// 注意：使用通配符捕获 y.format，在 handler 中解析
	router.GET("/tiles/:serviceName/:layerName/:z/:x/*yformat", tileEndpointHandler.GetXYZTile)

	// WMTS 端点（支持公开访问，handler内部会检查权限）
	router.GET("/wmts/:serviceName", wmtsHandler.GetCapabilities)

	// OGC Tiles API 端点（支持公开访问，handler内部会检查权限）
	router.GET("/ogc/tiles/:serviceName", ogcTilesHandler.GetLandingPage)
	router.GET("/ogc/tiles/:serviceName/conformance", ogcTilesHandler.GetConformance)
	router.GET("/ogc/tiles/:serviceName/tileMatrixSets", ogcTilesHandler.GetTileMatrixSets)
	router.GET("/ogc/tiles/:serviceName/tileMatrixSets/:tileMatrixSetId", ogcTilesHandler.GetTileMatrixSet)
	router.GET("/ogc/tiles/:serviceName/tiles", ogcTilesHandler.GetTilesets)
	router.GET("/ogc/tiles/:serviceName/tiles/:layer/:tileMatrixSetId/:tileMatrix/:tileRow/:tileCol", ogcTilesHandler.GetTile)

	// API 路由组（需要认证）
	api := router.Group("/api/v1/service")
	assetDiscHandler := newAssetDiscoverableHandler(db)
	{
		// 内部服务调用支持（X-Internal-API-Key 跳过 JWT 认证）
		api.Use(func(c *gin.Context) {
			if apiKey := c.GetHeader("X-Internal-API-Key"); apiKey != "" {
				tenantID := uint(0)
				if tenantIDStr := c.GetHeader("X-Tenant-ID"); tenantIDStr != "" {
					if tid, err := strconv.ParseUint(tenantIDStr, 10, 32); err == nil {
						tenantID = uint(tid)
					}
				}
				c.Set(commonAuth.ContextUserIDKey, uint(1))
				c.Set(commonAuth.ContextUsernameKey, "internal-api-call")
				c.Set(commonAuth.ContextTenantIDKey, tenantID)
				c.Next()
				return
			}
			c.Next()
		})
		// JWT 认证（内部 API Key 已通过时仍需阻止未认证请求）
		api.Use(func(c *gin.Context) {
			if c.GetHeader("X-Internal-API-Key") != "" {
				c.Next()
				return
			}
			authMiddleware.SystemAuthMiddleware(cfg.SystemServiceURL)(c)
		})
		// 审计日志中间件（记录到 System 模块）
		if systemClient != nil {
			api.Use(audit.AuditMiddleware("service", systemClient))
		}

		// 资产发现接口（供 Asset 模块调用）
		api.GET("/assets/discoverable", assetDiscHandler.listDiscoverableAssets)

		// 服务端点查询接口（供 Portal 等模块按 source_reference 查询 endpoint）
		api.GET("/endpoints", serviceEndpointHandler.GetEndpoints)

		// 查询服务管理 API
		queryAPI := api.Group("/query")
		{
			queryAPI.POST("", queryServiceHandler.CreateService)
			queryAPI.GET("", queryServiceHandler.ListServices)
			queryAPI.GET("/:id", queryServiceHandler.GetService)
			queryAPI.PUT("/:id", queryServiceHandler.UpdateService)
			queryAPI.DELETE("/:id", queryServiceHandler.DeleteService)
			queryAPI.GET("/:id/source-snapshot-diff", queryServiceHandler.CheckSourceSnapshot)
			queryAPI.POST("/:id/refresh-source-snapshot", queryServiceHandler.RefreshSourceSnapshot)
		}

		// 图查询服务管理 API
		graphAPI := api.Group("/graph")
		{
			graphAPI.POST("", graphQueryHandler.CreateService)
			graphAPI.GET("", graphQueryHandler.ListServices)
			graphAPI.GET("/:id", graphQueryHandler.GetService)
			graphAPI.PUT("/:id", graphQueryHandler.UpdateService)
			graphAPI.DELETE("/:id", graphQueryHandler.DeleteService)
		}

		// 注册服务管理 API
		registeredAPI := api.Group("/registered")
		{
			registeredAPI.POST("", registeredServiceHandler.CreateService)
			registeredAPI.GET("", registeredServiceHandler.ListServices)
			registeredAPI.GET("/:id", registeredServiceHandler.GetService)
			registeredAPI.PUT("/:id", registeredServiceHandler.UpdateService)
			registeredAPI.DELETE("/:id", registeredServiceHandler.DeleteService)
			registeredAPI.POST("/:id/refresh", registeredServiceHandler.RefreshMetadata)
			registeredAPI.POST("/:id/health", registeredServiceHandler.HealthCheck)
		}

		// 瓦片服务管理 API
		tileAPI := api.Group("/tile")
		{
			tileAPI.POST("", tileServiceHandler.CreateTileService)
			tileAPI.GET("", tileServiceHandler.ListTileServices)
			tileAPI.GET("/search", tileServiceHandler.SearchTileServices)
			tileAPI.GET("/by-name/:serviceName", tileServiceHandler.GetTileServiceByName)
			tileAPI.GET("/:id", tileServiceHandler.GetTileService)
			tileAPI.PUT("/:id", tileServiceHandler.UpdateTileService)
			tileAPI.DELETE("/:id", tileServiceHandler.DeleteTileService)
		}

		// 瓦片服务图层管理 API (单独的路由组避免冲突)
		tileLayerAPI := api.Group("/tile-layers")
		{
			tileLayerAPI.POST("/:serviceId", tileServiceHandler.AddLayer)
			tileLayerAPI.GET("/:serviceId", tileServiceHandler.ListLayers)
			tileLayerAPI.GET("/:serviceId/:layerId", tileServiceHandler.GetLayer)
			tileLayerAPI.PUT("/:serviceId/:layerId", tileServiceHandler.UpdateLayer)
			tileLayerAPI.DELETE("/:serviceId/:layerId", tileServiceHandler.DeleteLayer)
		}

		// 数据查询服务 API
		data := api.Group("/data")
		{
			data.POST("/query", dataServiceHandler.Query)
			data.POST("/aggregate", dataServiceHandler.Aggregate)
			data.GET("/structure", dataServiceHandler.GetTableStructure)
		}

		// 资源能力辅助 API。资源选择统一走 Meta resource-tree，Service 仅保留业务能力接口。
		api.GET("/graphs/node-shapes", resourceCapabilityHandler.GetGraphNodeShapes)
		api.POST("/sql/output-contract", resourceCapabilityHandler.GetSQLOutputContract)
	}

	return router
}
