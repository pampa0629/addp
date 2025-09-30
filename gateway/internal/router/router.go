package router

import (
	"github.com/addp/gateway/internal/config"
	"github.com/addp/gateway/internal/middleware"
	"github.com/addp/gateway/internal/proxy"
	"github.com/gin-gonic/gin"
)

func SetupRouter(cfg *config.Config) *gin.Engine {
	router := gin.Default()

	// CORS 中间件
	router.Use(middleware.CORS())

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
			"service": "gateway",
		})
	})

	// 网关首页
	router.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "全域数据平台 API Gateway",
			"version": "1.0.0",
			"services": gin.H{
				"system":   cfg.SystemServiceURL,
				"manager":  cfg.ManagerServiceURL,
				"meta":     cfg.MetaServiceURL,
				"transfer": cfg.TransferServiceURL,
			},
		})
	})

	// 创建代理
	systemProxy := proxy.NewServiceProxy(cfg.SystemServiceURL)
	managerProxy := proxy.NewServiceProxy(cfg.ManagerServiceURL)
	metaProxy := proxy.NewServiceProxy(cfg.MetaServiceURL)
	transferProxy := proxy.NewServiceProxy(cfg.TransferServiceURL)

	// 路由规则
	api := router.Group("/api")
	{
		// System 模块路由（认证、用户、日志、资源）
		api.Any("/auth/*path", systemProxy.Handle)
		api.Any("/users/*path", systemProxy.Handle)
		api.Any("/logs/*path", systemProxy.Handle)
		api.Any("/resources/*path", systemProxy.Handle)

		// Manager 模块路由（数据源、目录、预览）
		api.Any("/datasources/*path", managerProxy.Handle)
		api.Any("/directories/*path", managerProxy.Handle)
		api.Any("/preview/*path", managerProxy.Handle)
		api.Any("/upload/*path", managerProxy.Handle)

		// Meta 模块路由（元数据、血缘）
		api.Any("/metadata/*path", metaProxy.Handle)
		api.Any("/datasets/*path", metaProxy.Handle)
		api.Any("/lineage/*path", metaProxy.Handle)

		// Transfer 模块路由（任务、传输）
		api.Any("/tasks/*path", transferProxy.Handle)
		api.Any("/executions/*path", transferProxy.Handle)
	}

	return router
}