package api

import (
	"github.com/addp/mvt/internal/config"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// SetupRouter 配置路由
func SetupRouter(cfg *config.Config, handler *Handler) *gin.Engine {
	// 根据日志级别设置 Gin 模式
	if cfg.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	// CORS 配置
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{cfg.FrontendURL},
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept"},
		ExposeHeaders:    []string{"Content-Length", "X-Cache"},
		AllowCredentials: true,
		MaxAge:           12 * 3600,
	}))

	// 健康检查
	router.GET("/health", handler.Health)

	// API 路由组
	api := router.Group("/api")
	{
		// 数据源管理
		api.GET("/datasources", handler.ListDataSources)
		api.GET("/datasources/:id", handler.GetDataSource)

		// 缓存管理
		api.POST("/cache/clear", handler.ClearCache)
		api.POST("/cache/clear/:datasource_id", handler.ClearCache)
		api.GET("/cache/stats", handler.GetCacheStats)
	}

	// 瓦片服务（遵循 TMS 标准）
	// 注意：使用 *filepath 匹配以支持 .mvt 扩展名
	router.GET("/tiles/:datasource_id/:z/:x/*filepath", handler.GetTile)

	return router
}
