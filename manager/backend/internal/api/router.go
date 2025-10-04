package api

import (
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

func SetupRouter(cfg *config.Config, resourceService *service.ResourceService, metadataService *service.MetadataService) *gin.Engine {
	router := gin.Default()

	// CORS
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// 根路由
	router.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Manager 数据管理服务",
			"version": "1.0.0",
		})
	})

	// API 路由组
	api := router.Group("/api")
	{
		// 资源管理
		resources := api.Group("/resources")
		{
			resourceHandler := NewResourceHandler(resourceService)
			resources.GET("", resourceHandler.List)
			resources.GET("/:id", resourceHandler.GetByID)

			// 元数据扫描和管理
			metadataHandler := NewMetadataHandler(metadataService)
			resources.POST("/:id/scan", metadataHandler.ScanResource)
			resources.GET("/:id/tables", metadataHandler.GetTables)
		}

		// 表管理
		tables := api.Group("/tables")
		{
			metadataHandler := NewMetadataHandler(metadataService)
			tables.POST("/:id/manage", metadataHandler.ManageTable)
			tables.POST("/:id/unmanage", metadataHandler.UnmanageTable)
		}
	}

	return router
}