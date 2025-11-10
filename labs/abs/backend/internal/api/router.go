package api

import (
	"abs/internal/service"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// SetupRouter sets up the Gin router with all routes
func SetupRouter(taskService *service.TaskService, appService *service.AppService, wsManager *service.WebSocketManager, config *service.Config) *gin.Engine {
	router := gin.Default()

	// CORS middleware
	corsConfig := cors.Config{
		AllowOrigins:     []string{config.FrontendURL},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept"},
		AllowCredentials: true,
	}
	router.Use(cors.New(corsConfig))

	handler := NewHandler(taskService, appService, wsManager)

	// API routes
	api := router.Group("/api")
	{
		api.POST("/tasks", handler.CreateTask)
		api.GET("/tasks", handler.ListTasks)
		api.GET("/tasks/:id", handler.GetTask)

		api.POST("/apps", handler.CreateApp)
		api.GET("/apps", handler.ListApps)
		api.GET("/apps/:id", handler.GetApp)
		api.POST("/apps/:id/launch", handler.LaunchApp)
		api.POST("/apps/:id/modify", handler.ModifyApp)
		api.DELETE("/apps/:id", handler.DeleteApp)

		// App proxy - 反向代理到应用的实际端口
		// 优先级高于 workspace 路由，放在前面
		api.GET("/app-proxy/:app_id/*path", ProxyToApp(appService))
		api.POST("/app-proxy/:app_id/*path", ProxyToApp(appService))
		api.PUT("/app-proxy/:app_id/*path", ProxyToApp(appService))
		api.DELETE("/app-proxy/:app_id/*path", ProxyToApp(appService))
		api.PATCH("/app-proxy/:app_id/*path", ProxyToApp(appService))
		api.OPTIONS("/app-proxy/:app_id/*path", ProxyToApp(appService))

		// Workspace file serving (用于前端 iframe - 静态文件)
		api.GET("/workspace/*filepath", ServeWorkspaceFile(config.WorkspaceDir))
	}

	// WebSocket route
	router.GET("/ws", handler.WebSocket)

	// Health check
	router.GET("/health", handler.Health)

	// Workspace file serving (also support direct /workspace path without /api prefix)
	router.GET("/workspace/*filepath", ServeWorkspaceFile(config.WorkspaceDir))

	return router
}
