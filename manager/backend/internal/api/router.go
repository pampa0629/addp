package api

import (
	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

func SetupRouter(cfg *config.Config, resourceService *service.ResourceService, metadataService *service.MetadataService, searchService *service.FullTextSearchService, historyService *service.SearchHistoryService) *gin.Engine {
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

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "ok",
			"service": "manager",
		})
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
	api.Use(auth.SystemAuthMiddleware(cfg.SystemServiceURL))
	{
		configGroup := api.Group("/config")
		{
			configHandler := NewConfigHandler(cfg)
			configGroup.GET("/map", configHandler.GetMapConfig)
		}

		// 数据探查
		explorer := api.Group("/data-explorer")
		{
			handler := NewDataExplorerHandler(metadataService)
			explorer.GET("/tree", handler.GetTree)
			explorer.GET("/resources", handler.ListResources)
			explorer.GET("/resources/:id/tree", handler.GetResourceTree)
			explorer.POST("/resources/:id/refresh", handler.RefreshNode)
			explorer.GET("/preview", handler.PreviewTable)
			explorer.GET("/video-stream", handler.VideoStream)
		}

		// 资源管理
		resources := api.Group("/resources")
		{
			resourceHandler := NewResourceHandler(resourceService)
			resources.GET("", resourceHandler.List)
			resources.GET("/:id", resourceHandler.GetByID)

			// 元数据扫描和管理
			metadataHandler := NewMetadataHandler(metadataService)
			resources.POST("/:id/scan", metadataHandler.ScanResource)
			resources.GET("/:id/scan-tasks", metadataHandler.ListScanTasks)
			resources.POST("/:id/scan-tasks", metadataHandler.CreateScanTask)
			resources.PUT("/:id/scan-tasks/:task_id", metadataHandler.UpdateScanTask)
			resources.DELETE("/:id/scan-tasks/:task_id", metadataHandler.DeleteScanTask)
			resources.POST("/:id/scan-tasks/:task_id/trigger", metadataHandler.TriggerScanTask)
			resources.GET("/:id/scan-runs", metadataHandler.ListScanRuns)
			resources.GET("/:id/scan-runs/:run_id", metadataHandler.GetScanRun)
			resources.POST("/:id/scan-runs/manual", metadataHandler.CreateManualScanRun)
			resources.GET("/:id/tables", metadataHandler.GetTables)
		}

		// 表管理
		tables := api.Group("/tables")
		{
			metadataHandler := NewMetadataHandler(metadataService)
			tables.POST("/:id/manage", metadataHandler.ManageTable)
			tables.POST("/:id/unmanage", metadataHandler.UnmanageTable)
		}

		searchGroup := api.Group("/search")
		{
			handler := NewSearchHandler(searchService, historyService)
			searchGroup.GET("/fulltext", handler.FullTextSearch)
			searchGroup.GET("/history", handler.ListHistory)
			searchGroup.DELETE("/history/:id", handler.DeleteHistoryItem)
			searchGroup.DELETE("/history", handler.ClearHistory)
		}
	}

	return router
}
