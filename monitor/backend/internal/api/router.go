package api

import (
	"net/http"

	"github.com/addp/common/buildinfo"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/middleware/audit"
	commonAuth "github.com/addp/common/middleware/auth"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/monitor/docs"
	monitorauthorization "github.com/addp/monitor/internal/authorization"
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
	runtimePolicyService *service.RuntimePolicyService,
	smtpRelayService *service.SMTPRelayService,
	systemURL string,
	redisClient *redis.Client,
	systemClient *commonClient.SystemServiceClient,
	runtimeHealthServices ...*service.RuntimeHealthService,
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
	var runtimeHealthHandler *RuntimeHealthHandler
	if len(runtimeHealthServices) > 0 && runtimeHealthServices[0] != nil {
		runtimeHealthHandler = NewRuntimeHealthHandler(runtimeHealthServices[0])
	}
	alertHandler := NewAlertHandler(alertService)
	alertRuleHandler := NewAlertRuleHandler(alertRuleService)
	webhookHandler := NewWebhookHandler(webhookService)
	emailHandler := NewEmailHandler(emailService)

	// API 路由组
	api := router.Group("/api/v1/monitor")
	platform := api.Group("")
	platform.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: systemURL}),
		commonAuth.MustNewContextGuard("platform"),
	)
	if systemClient != nil {
		platform.Use(audit.ServiceAuditMiddleware("monitor", systemClient))
	}
	if runtimePolicyService != nil {
		handler := NewRuntimePolicyHandler(runtimePolicyService)
		platform.GET("/settings/runtime-policy", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorConfigurationRead), handler.Get)
		platform.PUT("/settings/runtime-policy", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorConfigurationUpdate), handler.Update)
	}
	if smtpRelayService != nil {
		handler := NewSMTPRelayHandler(smtpRelayService)
		platform.GET("/settings/smtp-relay", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorConfigurationRead), handler.Get)
		platform.PUT("/settings/smtp-relay", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorConfigurationUpdate), handler.Update)
		platform.PUT("/settings/smtp-relay/credential", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorConfigurationUpdate), handler.SetCredential)
		platform.DELETE("/settings/smtp-relay/credential", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorConfigurationUpdate), handler.DeleteCredential)
	}

	api.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: systemURL}),
		commonAuth.MustNewContextGuard("tenant"),
	)
	permission := func(keys ...string) gin.HandlerFunc {
		return commonAuth.MustNewPermissionGuard(keys...)
	}
	if systemClient != nil {
		api.Use(audit.ServiceAuditMiddleware("monitor", systemClient))
	}

	{
		// 执行记录查询
		api.GET("/executions", permission(monitorauthorization.PermissionMonitorExecutionRead), executionHandler.ListExecutions)

		// 统计数据
		api.GET("/executions/stats", permission(monitorauthorization.PermissionMonitorStatisticsRead), statisticsHandler.GetStatistics)
		api.GET("/executions/trend", permission(monitorauthorization.PermissionMonitorStatisticsRead), statisticsHandler.GetTrendData)
		api.GET("/executions/runtime-metrics", permission(monitorauthorization.PermissionMonitorStatisticsRead), statisticsHandler.GetExecutionRuntimeMetrics)

		api.GET("/executions/by-execution-id/:execution_id/tree", permission(monitorauthorization.PermissionMonitorExecutionRead), executionHandler.GetExecutionTreeByExecutionID)
		api.GET("/executions/by-execution-id/:execution_id", permission(monitorauthorization.PermissionMonitorExecutionRead), executionHandler.GetExecutionByExecutionID)
		api.GET("/executions/:id/tree", permission(monitorauthorization.PermissionMonitorExecutionRead), executionHandler.GetExecutionTree)
		api.GET("/executions/:id", permission(monitorauthorization.PermissionMonitorExecutionRead), executionHandler.GetExecution)

		api.GET("/alerts", permission(monitorauthorization.PermissionMonitorAlertIncidentRead), alertHandler.ListAlerts)
		api.POST("/alerts/:id/acknowledge", permission(monitorauthorization.PermissionMonitorAlertIncidentUpdate), alertHandler.AcknowledgeAlert)
		api.POST("/alerts/:id/suppress", permission(monitorauthorization.PermissionMonitorAlertIncidentUpdate), alertHandler.SuppressAlert)
		api.GET("/alert-rule-targets", permission(monitorauthorization.PermissionMonitorAlertRuleRead), alertRuleHandler.ListAlertRuleTargets)
		api.GET("/alert-rules", permission(monitorauthorization.PermissionMonitorAlertRuleRead), alertRuleHandler.ListAlertRules)
		api.POST("/alert-rules", permission(monitorauthorization.PermissionMonitorAlertRuleCreate), alertRuleHandler.CreateAlertRule)
		api.PATCH("/alert-rules/:id", permission(monitorauthorization.PermissionMonitorAlertRuleUpdate), alertRuleHandler.UpdateAlertRule)
		api.DELETE("/alert-rules/:id", permission(monitorauthorization.PermissionMonitorAlertRuleDelete), alertRuleHandler.DeleteAlertRule)
		api.GET("/webhook-destinations", permission(monitorauthorization.PermissionMonitorNotificationDestinationRead), webhookHandler.ListWebhookDestinations)
		api.POST("/webhook-destinations", permission(monitorauthorization.PermissionMonitorNotificationDestinationCreate), webhookHandler.CreateWebhookDestination)
		api.PATCH("/webhook-destinations/:id", permission(monitorauthorization.PermissionMonitorNotificationDestinationUpdate), webhookHandler.UpdateWebhookDestination)
		api.POST("/webhook-destinations/:id/test", permission(monitorauthorization.PermissionMonitorNotificationDestinationExecute), webhookHandler.TestWebhookDestination)
		api.DELETE("/webhook-destinations/:id", permission(monitorauthorization.PermissionMonitorNotificationDestinationDelete), webhookHandler.DeleteWebhookDestination)
		api.GET("/webhook-deliveries", permission(monitorauthorization.PermissionMonitorNotificationDeliveryRead), webhookHandler.ListWebhookDeliveries)
		api.POST("/webhook-deliveries/:delivery_id/retry", permission(monitorauthorization.PermissionMonitorNotificationDeliveryRetry), webhookHandler.RetryWebhookDelivery)
		api.GET("/email-destinations", permission(monitorauthorization.PermissionMonitorNotificationDestinationRead), emailHandler.ListEmailDestinations)
		api.POST("/email-destinations", permission(monitorauthorization.PermissionMonitorNotificationDestinationCreate), emailHandler.CreateEmailDestination)
		api.PATCH("/email-destinations/:id", permission(monitorauthorization.PermissionMonitorNotificationDestinationUpdate), emailHandler.UpdateEmailDestination)
		api.POST("/email-destinations/:id/test", permission(monitorauthorization.PermissionMonitorNotificationDestinationExecute), emailHandler.TestEmailDestination)
		api.DELETE("/email-destinations/:id", permission(monitorauthorization.PermissionMonitorNotificationDestinationDelete), emailHandler.DeleteEmailDestination)
		api.GET("/email-deliveries", permission(monitorauthorization.PermissionMonitorNotificationDeliveryRead), emailHandler.ListEmailDeliveries)
		api.POST("/email-deliveries/:delivery_id/retry", permission(monitorauthorization.PermissionMonitorNotificationDeliveryRetry), emailHandler.RetryEmailDelivery)

		// 模块健康检查
		api.GET("/task-providers", permission(monitorauthorization.PermissionMonitorHealthRead), healthHandler.GetTaskProviders)
		api.GET("/providers/health", permission(monitorauthorization.PermissionMonitorHealthRead), healthHandler.CheckAllProvidersHealth)
		api.GET("/providers/:module/health", permission(monitorauthorization.PermissionMonitorHealthRead), healthHandler.CheckProviderHealth)
		api.GET("/modules", permission(monitorauthorization.PermissionMonitorHealthRead), healthHandler.GetModules)
		api.GET("/modules/:module/health", permission(monitorauthorization.PermissionMonitorHealthRead), healthHandler.CheckModuleHealth)
		api.GET("/modules/health/all", permission(monitorauthorization.PermissionMonitorHealthRead), healthHandler.CheckAllModules)
		if runtimeHealthHandler != nil {
			api.GET("/runtime-instances/health", permission(monitorauthorization.PermissionMonitorHealthRead), runtimeHealthHandler.ListHealth)
		}
	}

	// 健康检查（无认证）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, buildinfo.Health("monitor"))
	})

	return router
}
