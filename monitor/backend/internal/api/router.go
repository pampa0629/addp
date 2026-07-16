package api

import (
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/middleware/audit"
	commonAuth "github.com/addp/common/middleware/auth"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/monitor/docs"
	"github.com/addp/monitor/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// SetupRouter 设置路由
func SetupRouter(
	queryService *service.ExecutionQueryService,
	statisticsService *service.StatisticsService,
	healthService *service.HealthCheckService,
	alertService *service.AlertService,
	alertRuleService *service.AlertRuleService,
	webhookService *service.WebhookService,
	emailService *service.EmailService,
	systemURL string,
	redisClient *redis.Client,
	systemClient *commonClient.SystemClient,
) *gin.Engine {
	router := gin.Default()

	// i18n 中间件（解析 Accept-Language 请求头）
	router.Use(i18nmiddleware.I18nMiddleware())

	// Swagger 文档
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// CORS 中间件
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
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
	alertHandler := NewAlertHandler(alertService)
	alertRuleHandler := NewAlertRuleHandler(alertRuleService)
	webhookHandler := NewWebhookHandler(webhookService)
	emailHandler := NewEmailHandler(emailService)

	// API 路由组
	api := router.Group("/api/v1/monitor")

	// 使用 Redis 缓存中间件 (TTL: 5分钟)
	if redisClient != nil {
		api.Use(commonAuth.CachedSystemAuthMiddleware(systemURL, redisClient, 5*time.Minute))
	} else {
		// Fallback: 无缓存模式
		api.Use(commonAuth.SystemAuthMiddleware(systemURL))
	}
	if systemClient != nil {
		api.Use(audit.AuditMiddleware("monitor", systemClient))
	}

	{
		// 执行记录查询
		api.GET("/executions", executionHandler.ListExecutions)

		// 统计数据
		api.GET("/executions/stats", statisticsHandler.GetStatistics)
		api.GET("/executions/trend", statisticsHandler.GetTrendData)

		api.GET("/executions/by-execution-id/:execution_id/tree", executionHandler.GetExecutionTreeByExecutionID)
		api.GET("/executions/by-execution-id/:execution_id", executionHandler.GetExecutionByExecutionID)
		api.GET("/executions/:id/tree", executionHandler.GetExecutionTree)
		api.GET("/executions/:id", executionHandler.GetExecution)

		api.GET("/alerts", alertHandler.ListAlerts)
		api.POST("/alerts/:id/acknowledge", alertHandler.AcknowledgeAlert)
		api.POST("/alerts/:id/suppress", alertHandler.SuppressAlert)
		api.GET("/alert-rule-targets", alertRuleHandler.ListAlertRuleTargets)
		api.GET("/alert-rules", alertRuleHandler.ListAlertRules)
		api.POST("/alert-rules", alertRuleHandler.CreateAlertRule)
		api.PATCH("/alert-rules/:id", alertRuleHandler.UpdateAlertRule)
		api.DELETE("/alert-rules/:id", alertRuleHandler.DeleteAlertRule)
		api.GET("/webhook-destinations", webhookHandler.ListWebhookDestinations)
		api.POST("/webhook-destinations", webhookHandler.CreateWebhookDestination)
		api.PATCH("/webhook-destinations/:id", webhookHandler.UpdateWebhookDestination)
		api.POST("/webhook-destinations/:id/test", webhookHandler.TestWebhookDestination)
		api.DELETE("/webhook-destinations/:id", webhookHandler.DeleteWebhookDestination)
		api.GET("/webhook-deliveries", webhookHandler.ListWebhookDeliveries)
		api.POST("/webhook-deliveries/:delivery_id/retry", webhookHandler.RetryWebhookDelivery)
		api.GET("/email-destinations", emailHandler.ListEmailDestinations)
		api.POST("/email-destinations", emailHandler.CreateEmailDestination)
		api.PATCH("/email-destinations/:id", emailHandler.UpdateEmailDestination)
		api.POST("/email-destinations/:id/test", emailHandler.TestEmailDestination)
		api.DELETE("/email-destinations/:id", emailHandler.DeleteEmailDestination)
		api.GET("/email-deliveries", emailHandler.ListEmailDeliveries)
		api.POST("/email-deliveries/:delivery_id/retry", emailHandler.RetryEmailDelivery)

		// 模块健康检查
		api.GET("/task-providers", healthHandler.GetTaskProviders)
		api.GET("/providers/health", healthHandler.CheckAllProvidersHealth)
		api.GET("/providers/:module/health", healthHandler.CheckProviderHealth)
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
