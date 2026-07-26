package api

import (
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/middleware/audit"
	authMiddleware "github.com/addp/common/middleware/auth"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/service/docs"
	_ "github.com/addp/service/i18n"
	serviceauthorization "github.com/addp/service/internal/authorization"
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
	// 可选认证：有 Bearer Header 就解析 AuthContext，没有就按公开访问处理。
	optionalAuth := optionalSystemAuth(cfg.SystemServiceURL)
	router.GET("/api/query/:serviceName", optionalAuth, queryServiceHandler.QueryData)

	// 图查询服务执行端点（支持公开访问）
	router.POST("/api/gquery/:serviceName", optionalAuth, graphQueryHandler.ExecuteQuery)

	// OGC API Features 端点（支持公开访问，handler内部会检查权限）
	router.GET("/ogc/features/:serviceName", optionalAuth, ogcFeaturesHandler.GetLandingPage)
	router.GET("/ogc/features/:serviceName/conformance", optionalAuth, ogcFeaturesHandler.GetConformance)
	router.GET("/ogc/features/:serviceName/collections", optionalAuth, ogcFeaturesHandler.GetCollections)
	router.GET("/ogc/features/:serviceName/collections/:collectionId/items", optionalAuth, ogcFeaturesHandler.GetItems)
	router.GET("/ogc/features/:serviceName/collections/:collectionId/items/:featureId", optionalAuth, ogcFeaturesHandler.GetItem)

	// 代理服务端点（公开访问，无需认证）
	router.Any("/api/service/registered/proxy/:id/*path", registeredServiceHandler.ProxyService)

	// XYZ Tiles 端点（支持公开访问，handler内部会检查权限）
	// 注意：使用通配符捕获 y.format，在 handler 中解析
	router.GET("/tiles/:serviceName/:layerName/:z/:x/*yformat", optionalAuth, tileEndpointHandler.GetXYZTile)

	// WMTS 端点（支持公开访问，handler内部会检查权限）
	router.GET("/wmts/:serviceName", optionalAuth, wmtsHandler.GetCapabilities)

	// OGC Tiles API 端点（支持公开访问，handler内部会检查权限）
	router.GET("/ogc/tiles/:serviceName", optionalAuth, ogcTilesHandler.GetLandingPage)
	router.GET("/ogc/tiles/:serviceName/conformance", optionalAuth, ogcTilesHandler.GetConformance)
	router.GET("/ogc/tiles/:serviceName/tileMatrixSets", optionalAuth, ogcTilesHandler.GetTileMatrixSets)
	router.GET("/ogc/tiles/:serviceName/tileMatrixSets/:tileMatrixSetId", optionalAuth, ogcTilesHandler.GetTileMatrixSet)
	router.GET("/ogc/tiles/:serviceName/tiles", optionalAuth, ogcTilesHandler.GetTilesets)
	router.GET("/ogc/tiles/:serviceName/tiles/:layer/:tileMatrixSetId/:tileMatrix/:tileRow/:tileCol", optionalAuth, ogcTilesHandler.GetTile)

	assetDiscHandler := newAssetDiscoverableHandler(db)
	internal := router.Group("/api/v1/service/internal")
	internal.Use(internalAPIKeyMiddleware(cfg.InternalAPIKey))
	{
		internal.GET("/assets/discoverable", assetDiscHandler.listDiscoverableAssets)
		internal.GET("/endpoints", serviceEndpointHandler.GetEndpoints)
	}

	// 管理 API 只接受 canonical Bearer Tenant AuthContext。
	api := router.Group("/api/v1/service")
	api.Use(
		authMiddleware.MustNewMiddleware(authMiddleware.MiddlewareConfig{SystemURL: cfg.SystemServiceURL}),
		authMiddleware.MustNewContextGuard("tenant"),
	)
	permission := func(keys ...string) gin.HandlerFunc {
		return authMiddleware.MustNewPermissionGuard(keys...)
	}
	{
		// 审计日志中间件（记录到 System 模块）
		if systemClient != nil {
			api.Use(audit.AuditMiddleware("service", systemClient))
		}

		// 查询服务管理 API
		queryAPI := api.Group("/query")
		{
			queryAPI.POST("", permission(serviceauthorization.PermissionServiceDefinitionCreate), queryServiceHandler.CreateService)
			queryAPI.GET("", permission(serviceauthorization.PermissionServiceDefinitionRead), queryServiceHandler.ListServices)
			queryAPI.GET("/:id", permission(serviceauthorization.PermissionServiceDefinitionRead), queryServiceHandler.GetService)
			queryAPI.PUT("/:id", permission(serviceauthorization.PermissionServiceDefinitionUpdate), queryServiceHandler.UpdateService)
			queryAPI.DELETE("/:id", permission(serviceauthorization.PermissionServiceDefinitionDelete), queryServiceHandler.DeleteService)
			queryAPI.GET("/:id/source-snapshot-diff", permission(serviceauthorization.PermissionServiceDefinitionRead), queryServiceHandler.CheckSourceSnapshot)
			queryAPI.POST("/:id/refresh-source-snapshot", permission(serviceauthorization.PermissionServiceDefinitionUpdate), queryServiceHandler.RefreshSourceSnapshot)
		}

		// 图查询服务管理 API
		graphAPI := api.Group("/graph")
		{
			graphAPI.POST("", permission(serviceauthorization.PermissionServiceDefinitionCreate), graphQueryHandler.CreateService)
			graphAPI.GET("", permission(serviceauthorization.PermissionServiceDefinitionRead), graphQueryHandler.ListServices)
			graphAPI.GET("/:id", permission(serviceauthorization.PermissionServiceDefinitionRead), graphQueryHandler.GetService)
			graphAPI.PUT("/:id", permission(serviceauthorization.PermissionServiceDefinitionUpdate), graphQueryHandler.UpdateService)
			graphAPI.DELETE("/:id", permission(serviceauthorization.PermissionServiceDefinitionDelete), graphQueryHandler.DeleteService)
		}

		// 注册服务管理 API
		registeredAPI := api.Group("/registered")
		{
			registeredAPI.POST("", permission(serviceauthorization.PermissionServiceExternalRegistrationCreate), registeredServiceHandler.CreateService)
			registeredAPI.GET("", permission(serviceauthorization.PermissionServiceExternalRegistrationRead), registeredServiceHandler.ListServices)
			registeredAPI.GET("/:id", permission(serviceauthorization.PermissionServiceExternalRegistrationRead), registeredServiceHandler.GetService)
			registeredAPI.PUT("/:id", permission(serviceauthorization.PermissionServiceExternalRegistrationUpdate), registeredServiceHandler.UpdateService)
			registeredAPI.DELETE("/:id", permission(serviceauthorization.PermissionServiceExternalRegistrationDelete), registeredServiceHandler.DeleteService)
			registeredAPI.POST("/:id/refresh", permission(serviceauthorization.PermissionServiceExternalRegistrationUpdate), registeredServiceHandler.RefreshMetadata)
			registeredAPI.POST("/:id/health", permission(serviceauthorization.PermissionServiceExternalRegistrationRead), registeredServiceHandler.HealthCheck)
		}

		// 瓦片服务管理 API
		tileAPI := api.Group("/tile")
		{
			tileAPI.POST("", permission(serviceauthorization.PermissionServiceDefinitionCreate), tileServiceHandler.CreateTileService)
			tileAPI.GET("", permission(serviceauthorization.PermissionServiceDefinitionRead), tileServiceHandler.ListTileServices)
			tileAPI.GET("/search", permission(serviceauthorization.PermissionServiceDefinitionRead), tileServiceHandler.SearchTileServices)
			tileAPI.GET("/by-name/:serviceName", permission(serviceauthorization.PermissionServiceDefinitionRead), tileServiceHandler.GetTileServiceByName)
			tileAPI.GET("/:id", permission(serviceauthorization.PermissionServiceDefinitionRead), tileServiceHandler.GetTileService)
			tileAPI.PUT("/:id", permission(serviceauthorization.PermissionServiceDefinitionUpdate), tileServiceHandler.UpdateTileService)
			tileAPI.DELETE("/:id", permission(serviceauthorization.PermissionServiceDefinitionDelete), tileServiceHandler.DeleteTileService)
		}

		// 瓦片服务图层管理 API (单独的路由组避免冲突)
		tileLayerAPI := api.Group("/tile-layers")
		{
			tileLayerAPI.POST("/:serviceId", permission(serviceauthorization.PermissionServiceDefinitionUpdate), tileServiceHandler.AddLayer)
			tileLayerAPI.GET("/:serviceId", permission(serviceauthorization.PermissionServiceDefinitionRead), tileServiceHandler.ListLayers)
			tileLayerAPI.GET("/:serviceId/:layerId", permission(serviceauthorization.PermissionServiceDefinitionRead), tileServiceHandler.GetLayer)
			tileLayerAPI.PUT("/:serviceId/:layerId", permission(serviceauthorization.PermissionServiceDefinitionUpdate), tileServiceHandler.UpdateLayer)
			tileLayerAPI.DELETE("/:serviceId/:layerId", permission(serviceauthorization.PermissionServiceDefinitionUpdate), tileServiceHandler.DeleteLayer)
		}

		// 数据查询服务 API
		data := api.Group("/data")
		{
			data.POST("/query", permission(serviceauthorization.PermissionServiceDefinitionRead), dataServiceHandler.Query)
			data.POST("/aggregate", permission(serviceauthorization.PermissionServiceDefinitionRead), dataServiceHandler.Aggregate)
			data.GET("/structure", permission(serviceauthorization.PermissionServiceDefinitionRead), dataServiceHandler.GetTableStructure)
		}

		// 资源能力辅助 API。资源选择统一走 Meta resource-tree，Service 仅保留业务能力接口。
		api.GET("/graphs/node-shapes", permission(serviceauthorization.PermissionServiceDefinitionRead), resourceCapabilityHandler.GetGraphNodeShapes)
		api.POST("/sql/output-contract", permission(serviceauthorization.PermissionServiceDefinitionRead), resourceCapabilityHandler.GetSQLOutputContract)
	}

	return router
}

func optionalSystemAuth(systemURL string) gin.HandlerFunc {
	systemAuth := authMiddleware.MustNewMiddleware(authMiddleware.MiddlewareConfig{SystemURL: systemURL})
	return func(c *gin.Context) {
		if strings.TrimSpace(c.GetHeader("Authorization")) == "" {
			c.Next()
			return
		}
		systemAuth(c)
	}
}
