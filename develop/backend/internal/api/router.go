package api

import (
	commonAuth "github.com/addp/common/middleware/auth"
	commonCors "github.com/addp/common/middleware/cors"
	"github.com/addp/develop/backend/internal/config"
	"github.com/gin-gonic/gin"
)

// SetupRouter 设置路由
func SetupRouter(cfg *config.Config, sqlHandler *SQLHandler, spatialHandler *SpatialHandler, gisExecutionHandler *GISExecutionHandler) *gin.Engine {
	router := gin.Default()

	// 全局中间件
	router.Use(commonCors.CORS())

	// 健康检查（无需认证）
	router.GET("/health", sqlHandler.Health)
	router.GET("/api/develop/health", sqlHandler.Health)

	// API 路由组（需要认证）
	api := router.Group("/api/develop")
	api.Use(commonAuth.SystemAuthMiddleware(cfg.SystemServiceURL))
	{
		// 资源列表（数据库连接）
		api.GET("/resources", sqlHandler.ListResources)

		// SQL 执行
		api.POST("/execute", sqlHandler.Execute)

		// 连接测试
		api.GET("/test/:resource_id", sqlHandler.TestConnection)

		// ==================== 空间工作流 API ====================
		spatial := api.Group("/spatial")
		{
			// 空间引擎列表（供前端引擎选择器使用）
			spatial.GET("/engines", spatialHandler.ListSpatialEngines)

			// 算子列表（供前端使用）
			spatial.GET("/operators", spatialHandler.ListOperators)

			// 即时执行工作流
			spatial.POST("/workflow/execute", spatialHandler.ExecuteWorkflow)

			// 执行单个算子
			spatial.POST("/operators/:name/execute", spatialHandler.ExecuteOperator)

			// 任务管理
			spatial.GET("/tasks", spatialHandler.ListTasks)
			spatial.POST("/tasks", spatialHandler.CreateTask)
			spatial.GET("/tasks/:id", spatialHandler.GetTask)
			spatial.PUT("/tasks/:id", spatialHandler.UpdateTask)
			spatial.DELETE("/tasks/:id", spatialHandler.DeleteTask)
			spatial.POST("/tasks/:id/execute", spatialHandler.ExecuteTask)

			// 执行状态查询
			spatial.GET("/executions/:id", spatialHandler.GetExecutionStatus)
		}

		// ==================== GIS 执行历史 API ====================
		gisExecutions := api.Group("/gis-executions")
		{
			gisExecutions.GET("", gisExecutionHandler.ListExecutions)                     // 列表
			gisExecutions.GET("/:id", gisExecutionHandler.GetExecution)                   // 详情
			gisExecutions.GET("/:id/logs", gisExecutionHandler.GetExecutionLogs)          // 日志
			gisExecutions.POST("/:id/retry", gisExecutionHandler.RetryExecution)          // 重试
			gisExecutions.POST("/:id/cancel", gisExecutionHandler.CancelExecution)        // 取消
			gisExecutions.DELETE("/:id", gisExecutionHandler.DeleteExecution)             // 删除
			gisExecutions.GET("/statistics", gisExecutionHandler.GetExecutionStatistics)  // 统计
		}

		// TODO: Phase 2 - 脚本管理
		// api.GET("/scripts", scriptHandler.List)
		// api.POST("/scripts", scriptHandler.Create)
		// api.GET("/scripts/:id", scriptHandler.Get)
		// api.PUT("/scripts/:id", scriptHandler.Update)
		// api.DELETE("/scripts/:id", scriptHandler.Delete)
	}

	return router
}
