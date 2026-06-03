package api

import (
	"time"

	commonExecution "github.com/addp/common/execution"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/quality/docs"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"
)

func getTenantID(c *gin.Context) int64 {
	if v, exists := c.Get("tenant_id"); exists {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	return 1
}

func getUserID(c *gin.Context) int64 {
	if v, exists := c.Get("user_id"); exists {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	return 1
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
	executionHandler := NewExecutionHandler(commonExecution.NewTaskExecutionRepository(db))
	issueHandler := NewIssueHandler(issueSvc)

	api := router.Group("/api/v1/quality")
	if redisClient != nil {
		api.Use(commonAuth.CachedSystemAuthMiddleware(systemURL, redisClient, 5*time.Minute))
	} else {
		api.Use(commonAuth.SystemAuthMiddleware(systemURL))
	}

	{
		// 规则应用（字段-规则映射）
		ruleApps := api.Group("/rule-applications")
		{
			ruleApps.GET("", ruleAppHandler.List)
			ruleApps.POST("", ruleAppHandler.Create)
			ruleApps.GET("/:id", ruleAppHandler.Get)
			ruleApps.PUT("/:id", ruleAppHandler.Update)
			ruleApps.DELETE("/:id", ruleAppHandler.Delete)
		}

		// 检查任务
		checkTasks := api.Group("/check-tasks")
		{
			checkTasks.GET("", checkTaskHandler.List)
			checkTasks.POST("", checkTaskHandler.Create)
			checkTasks.GET("/:id", checkTaskHandler.Get)
			checkTasks.PUT("/:id", checkTaskHandler.Update)
			checkTasks.DELETE("/:id", checkTaskHandler.Delete)
			checkTasks.POST("/:id/run", checkTaskHandler.Run)
		}

		// 执行记录（读 common.task_executions）
		executions := api.Group("/executions")
		{
			executions.GET("", executionHandler.List)
			executions.GET("/:id", executionHandler.Get)
		}

		// 问题工单
		issues := api.Group("/issues")
		{
			issues.GET("", issueHandler.List)
			issues.GET("/:id", issueHandler.Get)
			issues.PUT("/:id/status", issueHandler.UpdateStatus)
		}
	}

	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	return router
}
