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

		// Workspace file serving (用于前端 iframe)
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
