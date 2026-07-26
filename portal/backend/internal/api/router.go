package api

import (
	commonClient "github.com/addp/common/client"
	commonAuth "github.com/addp/common/middleware/auth"
	_ "github.com/addp/portal/docs"
	"github.com/addp/portal/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func SetupRouter(cfg *config.Config, redisClient *redis.Client) *gin.Engine {
	router := gin.Default()

	// CORS 中间件
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Tenant-ID")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// 健康检查（无需认证）
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "module": "portal"})
	})

	// Swagger 文档
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 初始化 asset client（portal BFF 通过内部 API Key 调用 asset 服务）
	assetClient := commonClient.NewAssetClientWithInternalKey(cfg.AssetURL, cfg.InternalAPIKey)
	// 初始化 service client
	serviceClient := commonClient.NewServiceClientWithInternalKey(cfg.ServiceURL, cfg.InternalAPIKey)

	// Portal BFF 路由（需要认证）
	api := router.Group("/api/v1/portal")
	api.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: cfg.SystemURL}),
		commonAuth.MustNewContextGuard("tenant"),
	)

	// ============================================================
	// 门户首页（Phase 3）
	// ============================================================
	api.GET("/home", handleHome(assetClient))

	// ============================================================
	// 资产发现（Phase 3）
	// ============================================================
	api.GET("/search", handleSearch(assetClient))
	api.GET("/catalogs", handleCatalogs(assetClient))
	api.GET("/catalogs/:id/assets", handleCatalogAssets(assetClient))
	api.GET("/assets", handleAssets(assetClient))
	api.GET("/assets/:id", handleAssetDetail(assetClient))

	// ============================================================
	// 资产申请（Phase 4）
	// ============================================================
	api.POST("/assets/:id/apply", handleApply(assetClient))
	api.GET("/assets/:id/apply-status", handleApplyStatus(assetClient))
	api.GET("/my/applications", handleMyApplications(assetClient))
	api.GET("/assets/:id/endpoints", handleAssetEndpoints(assetClient, serviceClient))

	// ============================================================
	// 资产评价（Phase 6）
	// ============================================================
	api.GET("/assets/:id/ratings", handleGetRatings(assetClient))
	api.POST("/assets/:id/ratings", handleSubmitRating(assetClient))

	return router
}

// placeholderHandler 骨架占位处理器（已无使用，保留备用）
func placeholderHandler(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(501, gin.H{
			"message": "not implemented yet",
			"action":  action,
		})
	}
}
