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
	dataServiceHandler *DataServiceHandler,
	queryServiceHandler *QueryServiceHandler,
	registeredServiceHandler *RegisteredServiceHandler,
	engineHandler *EngineHandler,
	dataSourceHandler *DataSourceHandler,
	systemClient *commonClient.SystemClient,
) *gin.Engine {
	router := gin.Default()

	// CORS 中间件
	router.Use(corsMiddleware.CORS())

	// 健康检查端点（无需认证）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy", "service": "service"})
	})

	// 查询服务端点（支持公开访问，handler内部会检查权限）
	router.GET("/api/query/:serviceName", queryServiceHandler.QueryData)

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

			// 代理转发端点 - 支持所有 HTTP 方法
			registeredAPI.Any("/proxy/:id/*path", registeredServiceHandler.ProxyService)
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
	}

	return router
}
