package api

import (
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/middleware/audit"
	commonAuth "github.com/addp/common/middleware/auth"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/modulelifecycle"
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
	lifecycle *modulelifecycle.Controller,
	logPipeline *service.LogPipelineService,
	platformAlerts *service.PlatformAlertService,
	runtimeHealthService *service.RuntimeHealthService,
	monitoringTargets *service.MonitoringTargetService,
	resourceObservations *service.ResourceObservationService,
	resourceRules *service.ResourceAlertRuleService,
	httpRules *service.HTTPAlertRuleService,
) *gin.Engine {
	router := modulelifecycle.NewRouter(gin.Logger())

	// i18n 中间件（解析 Accept-Language 请求头）
	router.Use(i18nmiddleware.I18nMiddleware())

	// Swagger 文档
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	lifecycle.RegisterHealthRoutes(router)
	router.Use(lifecycle.RequireReady())

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
	if runtimeHealthService != nil {
		runtimeHealthHandler = NewRuntimeHealthHandler(runtimeHealthService)
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
		logPipelineAudit(systemClient),
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

	if logPipeline != nil {
		handler := NewLogPipelineHandler(logPipeline)
		platform.GET("/platform/log-pipeline", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorLogPipelineRead), handler.Summary)
		platform.GET("/platform/log-pipeline/policy", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorLogPipelineRead), handler.Policy)
		platform.PUT("/platform/log-pipeline/policy", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorLogPipelineUpdate), handler.UpdatePolicy)
		platform.POST("/platform/log-observations", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorLogObservationCreate), commonAuth.MustNewServiceClientGuard("addp-log-observer"), handler.Observe)
	}
	if platformAlerts != nil {
		handler := NewPlatformAlertHandler(platformAlerts)
		platform.GET("/platform/alerts", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformAlertRead), platformObservationIdentity(false), handler.Incidents)
		platform.POST("/platform/alerts/:id/acknowledge", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformAlertRead, monitorauthorization.PermissionMonitorPlatformAlertUpdate), platformObservationIdentity(false), handler.Acknowledge)
		platform.POST("/platform/alerts/:id/suppress", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformAlertRead, monitorauthorization.PermissionMonitorPlatformAlertUpdate), platformObservationIdentity(false), handler.Suppress)
		platform.GET("/platform/alert-notification-destinations", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformAlertNotificationRead), platformObservationIdentity(false), handler.Destinations)
		platform.POST("/platform/alert-notification-destinations", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformAlertNotificationRead, monitorauthorization.PermissionMonitorPlatformAlertNotificationUpdate), platformObservationIdentity(false), handler.CreateDestination)
		platform.PUT("/platform/alert-notification-destinations/:id", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformAlertNotificationRead, monitorauthorization.PermissionMonitorPlatformAlertNotificationUpdate), platformObservationIdentity(false), handler.UpdateDestination)
		platform.PUT("/platform/alert-notification-destinations/:id/credential", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformAlertNotificationRead, monitorauthorization.PermissionMonitorPlatformAlertNotificationUpdate), platformObservationIdentity(false), handler.SetCredential)
		platform.DELETE("/platform/alert-notification-destinations/:id", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformAlertNotificationRead, monitorauthorization.PermissionMonitorPlatformAlertNotificationUpdate), platformObservationIdentity(false), handler.DeleteDestination)
		platform.POST("/platform/alert-notification-destinations/:id/test", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformAlertNotificationRead, monitorauthorization.PermissionMonitorPlatformAlertNotificationUpdate), platformObservationIdentity(false), handler.TestDestination)
		platform.GET("/platform/alert-notification-deliveries", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformAlertNotificationRead), platformObservationIdentity(false), handler.Deliveries)
		platform.POST("/platform/alert-notification-deliveries/:id/retry", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformAlertNotificationRead, monitorauthorization.PermissionMonitorPlatformAlertNotificationUpdate), platformObservationIdentity(false), handler.RetryDelivery)
	}

	ruleHandler := &ResourceAlertRuleHandler{service: resourceRules}
	platform.GET("/platform/resource-alert-rules", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformResourceRuleRead), platformObservationIdentity(false), ruleHandler.List)
	platform.POST("/platform/resource-alert-rules", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformResourceRuleRead, monitorauthorization.PermissionMonitorPlatformResourceRuleCreate), platformObservationIdentity(false), ruleHandler.Create)
	platform.PUT("/platform/resource-alert-rules/:id", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformResourceRuleRead, monitorauthorization.PermissionMonitorPlatformResourceRuleUpdate), platformObservationIdentity(false), ruleHandler.Update)
	platform.DELETE("/platform/resource-alert-rules/:id", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformResourceRuleRead, monitorauthorization.PermissionMonitorPlatformResourceRuleDelete), platformObservationIdentity(false), ruleHandler.Delete)

	targetHandler := NewMonitoringTargetHandler(monitoringTargets)
	platform.GET("/platform/monitoring_targets", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorMonitoringTargetRead), platformObservationIdentity(false), targetHandler.List)
	platform.GET("/platform/monitoring_targets/:id", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorMonitoringTargetRead), platformObservationIdentity(false), targetHandler.Get)
	platform.POST("/platform/monitoring_targets", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorMonitoringTargetCreate), platformObservationIdentity(false), targetHandler.Create)
	platform.PUT("/platform/monitoring_targets/:id", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorMonitoringTargetUpdate), platformObservationIdentity(false), targetHandler.Update)
	platform.DELETE("/platform/monitoring_targets/:id", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorMonitoringTargetDelete), platformObservationIdentity(false), targetHandler.Delete)
	platform.GET("/platform/metrics_discovery", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorMetricsDiscoveryRead), platformObservationIdentity(true), targetHandler.Discovery)
	resourceHandler := NewResourceObservationHandler(resourceObservations)
	httpHandler := HTTPObservationHandler{service: resourceObservations}
	httpRuleHandler := HTTPAlertRuleHandler{service: httpRules}
	platform.GET("/platform/http-alert-rules", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformHttpRuleRead), platformObservationIdentity(false), httpRuleHandler.List)
	platform.POST("/platform/http-alert-rules", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformHttpRuleRead, monitorauthorization.PermissionMonitorPlatformHttpRuleCreate), platformObservationIdentity(false), httpRuleHandler.Create)
	platform.PUT("/platform/http-alert-rules/:id", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformHttpRuleRead, monitorauthorization.PermissionMonitorPlatformHttpRuleUpdate), platformObservationIdentity(false), httpRuleHandler.Update)
	platform.DELETE("/platform/http-alert-rules/:id", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorPlatformHttpRuleRead, monitorauthorization.PermissionMonitorPlatformHttpRuleDelete), platformObservationIdentity(false), httpRuleHandler.Delete)
	platform.GET("/platform/http_observations", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorHttpObservationRead), platformObservationIdentity(false), httpHandler.Summaries)
	platform.GET("/platform/http_interfaces", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorHttpObservationRead), platformObservationIdentity(false), httpHandler.Interfaces)
	platform.GET("/platform/http_trends", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorHttpObservationRead), platformObservationIdentity(false), httpHandler.Trend)
	platform.GET("/platform/resource_observations", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorResourceObservationRead), platformObservationIdentity(false), resourceHandler.Instant)
	platform.GET("/platform/resource_overview", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorResourceObservationRead), platformObservationIdentity(false), resourceHandler.Overview)
	platform.GET("/platform/resource_summaries", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorResourceObservationRead), platformObservationIdentity(false), resourceHandler.Summaries)
	platform.GET("/platform/process_resource_summaries", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorResourceObservationRead), platformObservationIdentity(false), resourceHandler.ProcessSummaries)
	platform.GET("/platform/process_resource_trends", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorResourceObservationRead), platformObservationIdentity(false), resourceHandler.ProcessTrend)
	platform.GET("/platform/resource_trends", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorResourceObservationRead), platformObservationIdentity(false), resourceHandler.Trend)
	platform.GET("/settings/resource-query-policy", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorConfigurationRead), platformObservationIdentity(false), resourceHandler.Policy)
	platform.PUT("/settings/resource-query-policy", commonAuth.MustNewPermissionGuard(monitorauthorization.PermissionMonitorConfigurationUpdate), platformObservationIdentity(false), resourceHandler.UpdatePolicy)
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

	api.Use(executionReadAuditMiddleware(systemClient))
	{
		// 执行记录查询
		api.GET("/executions", permission(monitorauthorization.PermissionMonitorExecutionRead), executionReadMiddleware(queryService), executionHandler.ListExecutions)

		// 统计数据
		api.GET("/executions/stats", permission(monitorauthorization.PermissionMonitorStatisticsRead), executionReadMiddleware(queryService), statisticsHandler.GetStatistics)
		api.GET("/executions/trend", permission(monitorauthorization.PermissionMonitorStatisticsRead), executionReadMiddleware(queryService), statisticsHandler.GetTrendData)
		api.GET("/executions/runtime-metrics", permission(monitorauthorization.PermissionMonitorStatisticsRead), executionReadMiddleware(queryService), statisticsHandler.GetExecutionRuntimeMetrics)

		api.GET("/executions/by-execution-id/:execution_id/tree", permission(monitorauthorization.PermissionMonitorExecutionRead), executionReadMiddleware(queryService), executionHandler.GetExecutionTreeByExecutionID)
		api.GET("/executions/by-execution-id/:execution_id", permission(monitorauthorization.PermissionMonitorExecutionRead), executionReadMiddleware(queryService), executionHandler.GetExecutionByExecutionID)
		api.GET("/executions/by-execution-id/:execution_id/events", permission(monitorauthorization.PermissionMonitorExecutionRead), executionReadMiddleware(queryService), executionHandler.GetExecutionEvents)
		api.GET("/executions/:id/tree", permission(monitorauthorization.PermissionMonitorExecutionRead), executionReadMiddleware(queryService), executionHandler.GetExecutionTree)
		api.GET("/executions/:id", permission(monitorauthorization.PermissionMonitorExecutionRead), executionReadMiddleware(queryService), executionHandler.GetExecution)

		api.GET("/alerts", permission(monitorauthorization.PermissionMonitorAlertIncidentRead), executionReadMiddleware(queryService), alertHandler.ListAlerts)
		api.POST("/alerts/:id/acknowledge", permission(monitorauthorization.PermissionMonitorAlertIncidentUpdate), executionReadMiddleware(queryService), alertHandler.AcknowledgeAlert)
		api.POST("/alerts/:id/suppress", permission(monitorauthorization.PermissionMonitorAlertIncidentUpdate), executionReadMiddleware(queryService), alertHandler.SuppressAlert)
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
		api.GET("/task-providers", permission(monitorauthorization.PermissionMonitorExecutionRead), healthHandler.GetTaskProviders)
		api.GET("/providers/health", permission(monitorauthorization.PermissionMonitorHealthRead), healthHandler.CheckAllProvidersHealth)
		api.GET("/providers/:module/health", permission(monitorauthorization.PermissionMonitorHealthRead), healthHandler.CheckProviderHealth)
		if runtimeHealthHandler != nil {
			api.GET("/runtime-instances/health", permission(monitorauthorization.PermissionMonitorHealthRead), runtimeHealthHandler.ListHealth)
		}
	}

	return router
}
