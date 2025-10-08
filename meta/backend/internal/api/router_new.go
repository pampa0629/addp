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

func SetupRouterNew(cfg *config.Config, db *gorm.DB) *gin.Engine {
	router := gin.Default()

	// CORS配置
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowCredentials = true
	corsConfig.AllowHeaders = []string{"Origin", "Content-Type", "Authorization"}
	corsConfig.AllowAllOrigins = true
	router.Use(cors.New(corsConfig))

	// 创建SystemClient
	systemClient := client.NewSystemClient(cfg.SystemServiceURL, "")

	// 创建服务
	resourceService := service.NewResourceService(db, cfg.SystemServiceURL, cfg.InternalAPIKey)
	scanService := service.NewScanServiceNew(db, systemClient, resourceService)

	// 创建Handler
	handler := NewHandler(resourceService, scanService)

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})

	// API路由组（需要认证）
	api := router.Group("/api/meta")
	api.Use(middleware.AuthMiddleware(cfg.SystemServiceURL))
	{
		// 资源相关
		api.GET("/resources", handler.GetResources)

		// Schema相关
		api.GET("/schemas/:resource_id", handler.GetSchemas)
		api.GET("/schemas/:resource_id/available", handler.ListAvailableSchemas)

		// 扫描相关
		api.POST("/scan/auto", handler.AutoScan)
		api.POST("/scan/resource", handler.ScanResource)
	}

	return router
}
