package api

import (
	commonExecution "github.com/addp/common/execution"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/modulelifecycle"
	_ "github.com/addp/quality/docs"
	qualityauthorization "github.com/addp/quality/internal/authorization"
	"github.com/addp/quality/internal/service"
	"github.com/addp/quality/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"
)

func getTenantID(c *gin.Context) int64 {
	return int64(commonAuth.GetTenantID(c))
}

func getUserID(c *gin.Context) int64 {
	return int64(commonAuth.GetUserID(c))
}

func SetupRouter(
	planSvc *service.PlanService,
	ruleSvc *service.RuleService,
	issueSvc *service.IssueService,
	catalogSummarySvc *service.CatalogSummaryService,
	db *gorm.DB,
	systemURL string,
	redisClient *redis.Client,
	lifecycle *modulelifecycle.Controller,
	standardReferenceGuardSvc *service.StandardReferenceGuardService,
) *gin.Engine {
	router := gin.Default()
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	lifecycle.RegisterHealthRoutes(router)
	router.Use(lifecycle.RequireReady())

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
	router.Use(commoni18n.I18nMiddleware())

	planHandler := NewPlanHandler(planSvc)
	overviewHandler := NewOverviewHandler(service.NewOverviewService(repository.NewOverviewRepository(db)))
	ruleHandler := NewRuleHandler(ruleSvc)
	taskProviderHandler := NewTaskProviderHandler(planSvc)
	executionHandler := NewExecutionHandler(commonExecution.NewTaskExecutionRepository(db))
	issueHandler := NewIssueHandler(issueSvc)
	catalogSummaryHandler := NewCatalogSummaryHandler(catalogSummarySvc)
	standardReferenceGuardHandler := NewStandardReferenceGuardHandler(standardReferenceGuardSvc)

	api := router.Group("/api/v1/quality")
	api.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: systemURL}),
		commonAuth.MustNewContextGuard("tenant"),
	)
	permission := func(keys ...string) gin.HandlerFunc {
		return commonAuth.MustNewPermissionGuard(keys...)
	}

	{
		api.GET("/overview", permission(qualityauthorization.PermissionQualityPlanRead, qualityauthorization.PermissionQualityIssueRead, "monitor.execution.read"), overviewHandler.Get)
		guard := api.Group("/standard-reference-guards")
		guard.Use(commonAuth.MustNewServiceClientGuard("addp-standard"))
		guard.PUT("/:resource_type/:resource_id", permission(qualityauthorization.PermissionQualityStandardReferenceUpdate), standardReferenceGuardHandler.SetState)
		catalogSummary := api.Group("/runtime/catalog-summaries")
		catalogSummary.Use(commonAuth.MustNewServiceClientGuard("addp-catalog"))
		catalogSummary.POST("/resolve", permission(qualityauthorization.PermissionQualityCatalogRead), catalogSummaryHandler.Resolve)
		plans := api.Group("/plans")
		plans.GET("", permission(qualityauthorization.PermissionQualityPlanRead), planHandler.List)
		plans.POST("", permission(qualityauthorization.PermissionQualityPlanCreate, qualityauthorization.PermissionQualityRuleRead), planHandler.Create)
		plans.GET("/:id", permission(qualityauthorization.PermissionQualityPlanRead), planHandler.Get)
		plans.PUT("/:id", permission(qualityauthorization.PermissionQualityPlanUpdate, qualityauthorization.PermissionQualityRuleRead), planHandler.Update)
		plans.DELETE("/:id", permission(qualityauthorization.PermissionQualityPlanDelete), planHandler.Delete)
		plans.POST("/:id/run", permission(qualityauthorization.PermissionQualityPlanExecute), planHandler.Run)
		rules := api.Group("/rules")
		rules.GET("", permission(qualityauthorization.PermissionQualityRuleRead), ruleHandler.List)
		rules.GET("/element-candidates", permission(qualityauthorization.PermissionQualityRuleRead), ruleHandler.ListElementCandidates)
		rules.POST("", permission(qualityauthorization.PermissionQualityRuleCreate), ruleHandler.Create)
		rules.GET("/:id", permission(qualityauthorization.PermissionQualityRuleRead), ruleHandler.Get)
		rules.PUT("/:id", permission(qualityauthorization.PermissionQualityRuleUpdate), ruleHandler.Update)
		rules.DELETE("/:id", permission(qualityauthorization.PermissionQualityRuleDelete), ruleHandler.Delete)
		rules.GET("/:id/plans", permission(qualityauthorization.PermissionQualityRuleRead, qualityauthorization.PermissionQualityPlanRead), ruleHandler.Plans)
		provider := api.Group("/task-provider")
		provider.Use(commonAuth.MustNewServiceClientGuard("addp-orchestrator"))
		provider.GET("/tasks", permission(qualityauthorization.PermissionQualityTaskProviderRead), taskProviderHandler.ListTasks)
		provider.GET("/tasks/:task_type/:id", permission(qualityauthorization.PermissionQualityTaskProviderRead), taskProviderHandler.TaskDetail)
		provider.POST("/tasks/:task_type/:id/execute", permission(qualityauthorization.PermissionQualityTaskProviderExecute), taskProviderHandler.TaskExecute)
		provider.GET("/executions/:execution_id", permission(qualityauthorization.PermissionQualityTaskProviderRead), executionHandler.ProviderGet)
		executions := api.Group("/executions")
		executions.GET("", permission("monitor.execution.read"), executionHandler.List)
		executions.GET("/:execution_id", permission("monitor.execution.read"), executionHandler.Get)
		issues := api.Group("/issues")
		issues.GET("", permission(qualityauthorization.PermissionQualityIssueRead), issueHandler.List)
		issues.GET("/:id", permission(qualityauthorization.PermissionQualityIssueRead), issueHandler.Get)
		issues.PUT("/:id/status", permission(qualityauthorization.PermissionQualityIssueUpdate), issueHandler.UpdateStatus)
	}
	return router
}
