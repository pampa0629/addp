package api

import (
	commonExecution "github.com/addp/common/execution"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/quality/docs"
	qualityauthorization "github.com/addp/quality/internal/authorization"
	"github.com/addp/quality/internal/service"
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
	ruleEngineSvc *service.RuleEngineService,
	checkTaskSvc *service.CheckTaskService,
	checkExecutor *service.CheckExecutor,
	issueSvc *service.IssueService,
	db *gorm.DB,
	systemURL string,
	redisClient *redis.Client,
) *gin.Engine {
	router := gin.Default()

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

	ruleAppHandler := NewRuleApplicationHandler(ruleEngineSvc)
	checkTaskHandler := NewCheckTaskHandler(checkTaskSvc, checkExecutor)
	taskProviderHandler := NewTaskProviderHandler(checkTaskSvc, checkExecutor)
	executionHandler := NewExecutionHandler(commonExecution.NewTaskExecutionRepository(db))
	issueHandler := NewIssueHandler(issueSvc)

	api := router.Group("/api/v1/quality")
	api.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: systemURL}),
		commonAuth.MustNewContextGuard("tenant"),
	)
	permission := func(keys ...string) gin.HandlerFunc {
		return commonAuth.MustNewPermissionGuard(keys...)
	}

	{
		// 规则应用（字段-规则映射）
		ruleApps := api.Group("/rule-applications")
		{
			ruleApps.GET("", permission(qualityauthorization.PermissionQualityRuleApplicationRead), ruleAppHandler.List)
			ruleApps.POST("", permission(qualityauthorization.PermissionQualityRuleApplicationCreate), ruleAppHandler.Create)
			ruleApps.GET("/:id", permission(qualityauthorization.PermissionQualityRuleApplicationRead), ruleAppHandler.Get)
			ruleApps.PUT("/:id", permission(qualityauthorization.PermissionQualityRuleApplicationUpdate), ruleAppHandler.Update)
			ruleApps.DELETE("/:id", permission(qualityauthorization.PermissionQualityRuleApplicationDelete), ruleAppHandler.Delete)
		}

		// 检查任务
		checkTasks := api.Group("/check-tasks")
		{
			checkTasks.GET("", permission(qualityauthorization.PermissionQualityCheckTaskRead), checkTaskHandler.List)
			checkTasks.POST("", permission(qualityauthorization.PermissionQualityCheckTaskCreate), checkTaskHandler.Create)
			checkTasks.GET("/:id", permission(qualityauthorization.PermissionQualityCheckTaskRead), checkTaskHandler.Get)
			checkTasks.PUT("/:id", permission(qualityauthorization.PermissionQualityCheckTaskUpdate), checkTaskHandler.Update)
			checkTasks.DELETE("/:id", permission(qualityauthorization.PermissionQualityCheckTaskDelete), checkTaskHandler.Delete)
			checkTasks.POST("/:id/run", permission(qualityauthorization.PermissionQualityCheckTaskExecute), checkTaskHandler.Run)
		}

		// TaskProvider 标准入口
		tasks := api.Group("/tasks")
		{
			tasks.GET("", permission(qualityauthorization.PermissionQualityCheckTaskRead), taskProviderHandler.ListTasks)
			tasks.GET("/:task_type/:id", permission(qualityauthorization.PermissionQualityCheckTaskRead), taskProviderHandler.TaskDetail)
			tasks.POST("/:task_type/:id/execute", permission(qualityauthorization.PermissionQualityCheckTaskExecute), taskProviderHandler.TaskExecute)
		}

		// 执行记录（读 common.task_executions）
		executions := api.Group("/executions")
		{
			executions.GET("", permission(qualityauthorization.PermissionQualityCheckTaskRead), executionHandler.List)
			executions.GET("/:execution_id", permission(qualityauthorization.PermissionQualityCheckTaskRead), executionHandler.Get)
		}

		// 问题工单
		issues := api.Group("/issues")
		{
			issues.GET("", permission(qualityauthorization.PermissionQualityIssueRead), issueHandler.List)
			issues.GET("/:id", permission(qualityauthorization.PermissionQualityIssueRead), issueHandler.Get)
			issues.PUT("/:id/status", permission(qualityauthorization.PermissionQualityIssueUpdate), issueHandler.UpdateStatus)
		}
	}

	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	return router
}
