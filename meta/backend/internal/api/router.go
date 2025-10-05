package api

import (
	"github.com/addp/common/client"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/middleware"
	"github.com/addp/meta/internal/service"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupRouter 设置路由
func SetupRouter(cfg *config.Config, db *gorm.DB) *gin.Engine {
	router := gin.Default()

	// CORS中间件
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	// 创建 System 客户端（使用内部 API Key）
	var systemClient *client.SystemClient
	if cfg.EnableIntegration {
		// 从环境变量获取 Internal API Key
		systemClient = client.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
	} else {
		// 如果不启用集成，使用空客户端（仅用于本地开发）
		systemClient = client.NewSystemClient(cfg.SystemServiceURL, "")
	}

	// 创建服务
	syncService := service.NewSyncService(db, systemClient)
	scanService := service.NewScanService(db, systemClient)
	metadataService := service.NewMetadataService(db, systemClient)

	// 创建处理器
	syncHandler := NewSyncHandler(syncService, cfg.SystemServiceURL)
	scanHandler := NewScanHandler(scanService)
	metadataHandler := NewMetadataHandler(metadataService)

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// API路由组 - 需要认证
	api := router.Group("/api/meta")
	api.Use(middleware.AuthMiddleware(cfg.SystemServiceURL))
	{
		// 同步相关
		api.POST("/sync/auto", syncHandler.AutoSyncAll)
		api.POST("/sync/:resource_id", syncHandler.SyncResource)

		// 扫描相关
		api.POST("/scan/database/:database_id", scanHandler.DeepScanDatabase)
		api.POST("/scan/table/:table_id", scanHandler.DeepScanTable)

		// 元数据查询
		api.GET("/datasources", metadataHandler.ListDatasources)
		api.GET("/datasources/:id", metadataHandler.GetDatasource)
		api.GET("/datasources/:id/databases", metadataHandler.ListDatabases)

		api.GET("/databases/:id", metadataHandler.GetDatabase)
		api.GET("/databases/:id/tables", metadataHandler.ListTables)

		api.GET("/tables/:id", metadataHandler.GetTable)
		api.GET("/tables/:id/fields", metadataHandler.ListFields)

		// 同步日志
		api.GET("/logs", metadataHandler.ListSyncLogs)

		// 搜索
		api.GET("/search/tables", metadataHandler.SearchTables)
		api.GET("/search/fields", metadataHandler.SearchFields)

		// 统计信息
		api.GET("/stats", metadataHandler.GetStats)
	}

	return router
}
