package api

import (
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/middleware/audit"
	authMiddleware "github.com/addp/common/middleware/auth"
	"github.com/addp/service/internal/config"
	"github.com/gin-gonic/gin"
)

func SetupRouter(
	cfg *config.Config,
	dataServiceHandler *DataServiceHandler,
	queryServiceHandler *QueryServiceHandler,
	ogcFeaturesHandler *OGCFeaturesHandler,
	registeredServiceHandler *RegisteredServiceHandler,
	tileServiceHandler *TileServiceHandler,
	tileEndpointHandler *TileEndpointHandler,
	wmtsHandler *WMTSHandler,
	ogcTilesHandler *OGCTilesHandler,
	engineHandler *EngineHandler,
	dataSourceHandler *DataSourceHandler,
	systemClient *commonClient.SystemClient,
) *gin.Engine {
	router := gin.Default()

	// 注意：CORS 由 Gateway 统一处理，此处无需设置 CORS 中间件

	// 健康检查端点（无需认证）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy", "service": "service"})
	})

	// 查询服务端点（支持公开访问，handler内部会检查权限）
	router.GET("/api/query/:serviceName", queryServiceHandler.QueryData)

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
	api := router.Group("/api/service")
	{
		// 认证中间件（通过 System 服务验证 token）
		api.Use(authMiddleware.SystemAuthMiddleware(cfg.SystemServiceURL))
		// 审计日志中间件（记录到 System 模块）
		if systemClient != nil {
			api.Use(audit.AuditMiddleware("service", systemClient))
		}

		// 查询服务管理 API
		queryAPI := api.Group("/query")
		{
			queryAPI.POST("", queryServiceHandler.CreateService)
			queryAPI.GET("", queryServiceHandler.ListServices)
			queryAPI.GET("/:id", queryServiceHandler.GetService)
			queryAPI.PUT("/:id", queryServiceHandler.UpdateService)
			queryAPI.DELETE("/:id", queryServiceHandler.DeleteService)
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

		// 数据源代理 API（用于前端选择数据表）
		api.GET("/engines", dataSourceHandler.GetEngines)
		api.GET("/engines/:engine_id/tree", dataSourceHandler.GetEngineTree)
		api.GET("/nodes/:node_id/children", dataSourceHandler.GetNodeChildren)
		api.GET("/tables/metadata", dataSourceHandler.GetTableMetadata)
		api.GET("/tables/spatial-metadata", dataSourceHandler.GetTableSpatialMetadata)
		api.POST("/sql/spatial-metadata", dataSourceHandler.GetSQLSpatialMetadata)
	}

	return router
}
