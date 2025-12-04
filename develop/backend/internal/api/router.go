package api

import (
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/middleware"
	"github.com/gin-gonic/gin"
)

// SetupRouter 设置路由
func SetupRouter(cfg *config.Config, sqlHandler *SQLHandler) *gin.Engine {
	router := gin.Default()

	// 全局中间件
	router.Use(middleware.CORS())

	// 健康检查（无需认证）
	router.GET("/health", sqlHandler.Health)
	router.GET("/api/develop/health", sqlHandler.Health)

	// API 路由组（需要认证）
	api := router.Group("/api/develop")
	api.Use(middleware.AuthMiddleware(cfg.SystemServiceURL))
	{
		// SQL 执行
		api.POST("/execute", sqlHandler.Execute)

		// 连接测试
		api.GET("/test/:resource_id", sqlHandler.TestConnection)

		// TODO: Phase 2 - 脚本管理
		// api.GET("/scripts", scriptHandler.List)
		// api.POST("/scripts", scriptHandler.Create)
		// api.GET("/scripts/:id", scriptHandler.Get)
		// api.PUT("/scripts/:id", scriptHandler.Update)
		// api.DELETE("/scripts/:id", scriptHandler.Delete)
	}

	return router
}
