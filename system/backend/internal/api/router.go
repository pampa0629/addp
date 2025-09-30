package api

import (
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func SetupRouter(db *gorm.DB, cfg *config.Config) *gin.Engine {
	router := gin.Default()

	// CORS
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

	// 初始化 repositories
	userRepo := repository.NewUserRepository(db)
	logRepo := repository.NewLogRepository(db)
	resourceRepo := repository.NewResourceRepository(db)

	// 初始化 services
	userService := service.NewUserService(userRepo)
	logService := service.NewLogService(logRepo)
	resourceService := service.NewResourceService(resourceRepo)

	// 日志中间件
	router.Use(middleware.LoggerMiddleware(logService))

	// 根路由
	router.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": cfg.ProjectName,
			"name_en": "All Domain Data Platform",
		})
	})

	// API 路由组
	api := router.Group("/api")
	{
		// 认证路由（不需要认证）
		auth := api.Group("/auth")
		{
			authHandler := NewAuthHandler(userService, cfg)
			auth.POST("/login", authHandler.Login)
			auth.POST("/register", authHandler.Register)
		}

		// 需要认证的路由
		protected := api.Group("")
		protected.Use(middleware.AuthMiddleware(cfg))
		{
			// 用户管理
			users := protected.Group("/users")
			{
				userHandler := NewUserHandler(userService)
				users.GET("", userHandler.List)
				users.GET("/:id", userHandler.GetByID)
				users.PUT("/:id", userHandler.Update)
				users.DELETE("/:id", userHandler.Delete)
				users.GET("/me", userHandler.Me)
			}

			// 日志管理
			logs := protected.Group("/logs")
			{
				logHandler := NewLogHandler(logService)
				logs.GET("", logHandler.List)
				logs.GET("/:id", logHandler.GetByID)
			}

			// 资源管理
			resources := protected.Group("/resources")
			{
				resourceHandler := NewResourceHandler(resourceService)
				resources.POST("", resourceHandler.Create)
				resources.GET("", resourceHandler.List)
				resources.GET("/:id", resourceHandler.GetByID)
				resources.PUT("/:id", resourceHandler.Update)
				resources.DELETE("/:id", resourceHandler.Delete)
				resources.POST("/:id/test", resourceHandler.TestConnection)        // 测试已有资源连接
				resources.POST("/test-connection", resourceHandler.TestConnectionBeforeCreate) // 创建前测试连接
			}
		}
	}

	return router
}