package api

import (
	authMiddleware "github.com/addp/common/middleware/auth"
	corsMiddleware "github.com/addp/common/middleware/cors"
	"github.com/addp/service/internal/config"
	"github.com/gin-gonic/gin"
)

func SetupRouter(
	cfg *config.Config,
	serviceRegistryHandler *ServiceRegistryHandler,
	dataServiceHandler *DataServiceHandler,
) *gin.Engine {
	router := gin.Default()

	// CORS 中间件
	router.Use(corsMiddleware.CORS())

	// 健康检查端点（无需认证）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy", "service": "service"})
	})

	// API 路由组（需要认证）
	api := router.Group("/api/service")
	{
		// 认证中间件（通过 System 服务验证 token）
		api.Use(authMiddleware.SystemAuthMiddleware(cfg.SystemServiceURL))

		// 服务注册管理 API
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

		// 服务目录 API
		api.GET("/catalog", serviceRegistryHandler.GetServiceCatalog)

		// 服务代理 API
		api.GET("/proxy/:id/*path", serviceRegistryHandler.ProxyService)

		// 数据查询服务 API
		data := api.Group("/data")
		{
			data.POST("/query", dataServiceHandler.Query)
			data.POST("/aggregate", dataServiceHandler.Aggregate)
		}

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
