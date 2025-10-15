package api

import (
	"time"

	"github.com/addp/common/logger"
	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/service"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func SetupRouterNew(cfg *config.Config, db *gorm.DB) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger())

	// CORS配置
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowCredentials = true
	corsConfig.AllowHeaders = []string{"Origin", "Content-Type", "Authorization"}
	corsConfig.AllowAllOrigins = true
	router.Use(cors.New(corsConfig))

	// 创建服务
	resourceService := service.NewResourceService(db, cfg.SystemServiceURL, cfg.InternalAPIKey)
	if err := resourceService.PreloadResources(); err != nil {
		logger.L().Warn("资源预加载失败，延迟到首次请求", "error", err)
	}
	scanService := service.NewScanServiceNew(db, resourceService)

	// 创建Handler
	handler := NewHandler(resourceService, scanService)

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})

	// API路由组（需要认证）
	api := router.Group("/api/meta")
	api.Use(auth.SystemAuthMiddleware(cfg.SystemServiceURL))
	{
		// 资源相关
		api.GET("/resources", handler.GetResources)

		// Schema相关
		api.GET("/schemas/:resource_id", handler.GetSchemas)
		api.GET("/schemas/:resource_id/available", handler.ListAvailableSchemas)
		api.GET("/object-storage/:resource_id/nodes", handler.ListObjectStorageNodes)

		// 扫描相关
		api.POST("/scan/auto", handler.AutoScan)
		api.POST("/scan/resource", handler.ScanResource)
	}

	return router
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		log := logger.With(
			"component", "http_server",
			"method", c.Request.Method,
			"path", path,
			"status", status,
			"latency_ms", latency.Milliseconds(),
			"client_ip", c.ClientIP(),
		)

		if len(c.Errors) > 0 {
			log = log.With("errors", c.Errors.String())
		}

		switch {
		case status >= 500:
			log.Error("请求处理完成")
		case status >= 400:
			log.Warn("请求处理完成")
		default:
			log.Info("请求处理完成")
		}
	}
}
