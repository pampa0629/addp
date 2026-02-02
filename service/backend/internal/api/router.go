package api

import (
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/middleware/audit"
	authMiddleware "github.com/addp/common/middleware/auth"
	corsMiddleware "github.com/addp/common/middleware/cors"
	"github.com/addp/service/internal/config"
	"github.com/gin-gonic/gin"
)

func SetupRouter(
	cfg *config.Config,
	serviceRegistryHandler *ServiceRegistryHandler,
	dataServiceHandler *DataServiceHandler,
	internalServiceHandler *InternalServiceHandler,
	wfsHandler *WFSHandler,
	ogcAPIHandler *OGCAPIHandler,
	wmtsHandler *WMTSHandler,
	restQueryHandler *RestQueryHandler,
	engineHandler *EngineHandler,
	systemClient *commonClient.SystemClient, // 用于审计日志
) *gin.Engine {
	router := gin.Default()

	// CORS 中间件
	router.Use(corsMiddleware.CORS())

	// 健康检查端点（无需认证）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy", "service": "service"})
	})

	// OGC WFS 端点（公开访问，无需认证）
	ogc := router.Group("/ogc/wfs")
	{
		ogc.GET("/:serviceName", wfsHandler.HandleWFSRequest)
		ogc.POST("/:serviceName", wfsHandler.PostGetFeature)
	}

	// OGC API Features 端点（公开访问，无需认证）
	api_features := router.Group("/ogc/api/:serviceName")
	{
		api_features.GET("/", ogcAPIHandler.LandingPage)
		api_features.GET("/conformance", ogcAPIHandler.Conformance)
		api_features.GET("/collections", ogcAPIHandler.Collections)
		api_features.GET("/collections/:collectionID", ogcAPIHandler.GetCollection)
		api_features.GET("/collections/:collectionID/items", ogcAPIHandler.Items)
		api_features.GET("/collections/:collectionID/items/:featureID", ogcAPIHandler.GetItem)
	}

	// OGC WMTS 端点（公开访问，无需认证）
	wmts := router.Group("/ogc/wmts")
	{
		wmts.GET("/:serviceName", wmtsHandler.GetCapabilities)
		wmts.GET("/:serviceName/tile/:layer/:z/:x/:y.mvt", wmtsHandler.GetTile)
	}

	// REST Query API 端点（公开访问，无需认证）
	query := router.Group("/api/query")
	{
		query.GET("/:serviceName/:layerName", restQueryHandler.Query)
	}

	// API 路由组（需要认证）
	api := router.Group("/api/service")
	{
		// 认证中间件（通过 System 服务验证 token）
		api.Use(authMiddleware.SystemAuthMiddleware(cfg.SystemServiceURL))
		// 审计日志中间件（记录到 System 模块）
		if systemClient != nil {
			api.Use(audit.AuditMiddleware("service", systemClient))
		}

		// 外部服务注册管理 API
		registry := api.Group("/registry")
		{
			registry.POST("/services", serviceRegistryHandler.CreateService)
			registry.GET("/services", serviceRegistryHandler.ListServices)
			registry.GET("/services/:id", serviceRegistryHandler.GetService)
			registry.PUT("/services/:id", serviceRegistryHandler.UpdateService)
			registry.DELETE("/services/:id", serviceRegistryHandler.DeleteService)
			registry.POST("/services/:id/refresh", serviceRegistryHandler.RefreshMetadata)
			registry.POST("/services/:id/health", serviceRegistryHandler.HealthCheck)
			registry.GET("/search", serviceRegistryHandler.SearchServices)
			registry.GET("/export", serviceRegistryHandler.ExportConfig)
		}

		// 内部服务（数据发布）API
		internal := api.Group("/internal")
		{
			// 服务管理
			internal.POST("/services", internalServiceHandler.CreateService)
			internal.GET("/services", internalServiceHandler.ListServices)
			internal.GET("/services/:id", internalServiceHandler.GetService)
			internal.PUT("/services/:id", internalServiceHandler.UpdateService)
			internal.DELETE("/services/:id", internalServiceHandler.DeleteService)

			// 图层管理
			internal.POST("/services/:id/layers", internalServiceHandler.AddLayer)
			internal.PUT("/services/:id/layers/:layer_id", internalServiceHandler.UpdateLayer)
			internal.DELETE("/services/:id/layers/:layer_id", internalServiceHandler.DeleteLayer)
		}

		// 服务目录 API
		api.GET("/catalog", serviceRegistryHandler.GetServiceCatalog)

		// 服务代理 API
		api.GET("/proxy/:id/*path", serviceRegistryHandler.ProxyService)

		// 数据查询服务 API
		data := api.Group("/data")
		{
			data.POST("/query", dataServiceHandler.Query)
			data.POST("/aggregate", dataServiceHandler.Aggregate)
			data.GET("/structure", dataServiceHandler.GetTableStructure)
		}

		// 引擎管理 API
		api.GET("/engines", engineHandler.ListEngines)

		// TODO: OGC API - Features
		// ogc := api.Group("/ogc")
		// {
		// 	ogc.GET("/collections", ogcHandler.GetCollections)
		// 	ogc.GET("/collections/:collection_id", ogcHandler.GetCollection)
		// 	ogc.GET("/collections/:collection_id/items", ogcHandler.GetItems)
		// 	ogc.GET("/collections/:collection_id/items/:item_id", ogcHandler.GetItem)
		// }
	}

	return router
}
