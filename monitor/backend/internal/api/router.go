package api

import (
	"time"

	commonAuth "github.com/addp/common/middleware/auth"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	_ "github.com/addp/monitor/docs"
)

// SetupRouter 设置路由
func SetupRouter(
	queryService *service.ExecutionQueryService,
	statisticsService *service.StatisticsService,
	healthService *service.HealthCheckService,
	systemURL string,
	redisClient *redis.Client,
) *gin.Engine {
	router := gin.Default()

	// i18n 中间件（解析 Accept-Language 请求头）
	router.Use(i18nmiddleware.I18nMiddleware())

	// Swagger 文档
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// CORS 中间件
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

	// 创建 Handlers
	executionHandler := NewExecutionHandler(queryService)
	statisticsHandler := NewStatisticsHandler(statisticsService)
	healthHandler := NewHealthHandler(healthService)

	// API 路由组
	api := router.Group("/api/v1/monitor")

	// 使用 Redis 缓存中间件 (TTL: 5分钟)
	if redisClient != nil {
		api.Use(commonAuth.CachedSystemAuthMiddleware(systemURL, redisClient, 5*time.Minute))
	} else {
		// Fallback: 无缓存模式
		api.Use(commonAuth.SystemAuthMiddleware(systemURL))
	}

	{
		// 执行记录查询
		api.GET("/executions", executionHandler.ListExecutions)
		api.GET("/executions/:id", executionHandler.GetExecution)

		// 统计数据
		api.GET("/executions/stats", statisticsHandler.GetStatistics)
		api.GET("/executions/trend", statisticsHandler.GetTrendData)

		// 模块健康检查
		api.GET("/modules", healthHandler.GetModules)
		api.GET("/modules/:module/health", healthHandler.CheckModuleHealth)
		api.GET("/modules/health/all", healthHandler.CheckAllModules)
	}

	// 健康检查（无认证）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	return router
}
