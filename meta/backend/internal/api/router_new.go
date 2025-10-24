package api

import (
	"time"

	"github.com/addp/common/logger"
	auth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/service"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func SetupRouterNew(cfg *config.Config, resourceService *service.ResourceService, scanService *service.ScanServiceNew, taskService *service.ScanTaskService) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger())

	// CORS配置
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowCredentials = true
	corsConfig.AllowHeaders = []string{"Origin", "Content-Type", "Authorization"}
	corsConfig.AllowAllOrigins = true
	router.Use(cors.New(corsConfig))

	if resourceService == nil || scanService == nil {
		panic("resourceService and scanService must be provided to SetupRouterNew")
	}

	// 创建Handler
	handler := NewHandler(resourceService, scanService, taskService)

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
		api.POST("/scan/run/manual", handler.CreateManualScanRun)
		api.GET("/scan/runs", handler.ListScanRuns)
		api.GET("/scan/runs/:run_id", handler.GetScanRun)
		api.GET("/scan/tasks", handler.ListScanTasks)
		api.POST("/scan/tasks", handler.CreateScanTask)
		api.PUT("/scan/tasks/:task_id", handler.UpdateScanTask)
		api.DELETE("/scan/tasks/:task_id", handler.DeleteScanTask)
		api.POST("/scan/tasks/:task_id/trigger", handler.TriggerScanTask)

		// 元数据相关
		api.GET("/metadata/object", handler.GetObjectMetadata)
		api.POST("/metadata/extract", handler.ExtractObjectMetadata)
		api.GET("/metadata/tables", handler.GetTables)
		api.GET("/metadata/fields", handler.GetTableFields)
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
