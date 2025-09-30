package api

import (
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

func SetupRouter(cfg *config.Config, dataSourceService *service.DataSourceService) *gin.Engine {
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
		// 数据源管理
		datasources := api.Group("/datasources")
		{
			dsHandler := NewDataSourceHandler(dataSourceService)
			datasources.POST("/sync", dsHandler.SyncFromSystem)
			datasources.GET("", dsHandler.List)
			datasources.GET("/:id", dsHandler.GetByID)
			datasources.DELETE("/:id", dsHandler.Delete)
		}
	}

	return router
}