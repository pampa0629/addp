package api

import (
	commonClient "github.com/addp/common/client"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/modulelifecycle"
	_ "github.com/addp/portal/docs"
	"github.com/addp/portal/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func SetupRouter(
	cfg *config.Config,
	redisClient *redis.Client,
	assetClient *commonClient.AssetClient,
	serviceClient *commonClient.ServiceClient,
	lifecycle *modulelifecycle.Controller,
) *gin.Engine {
	router := gin.Default()

	// CORS 中间件
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

	// Swagger 文档
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	lifecycle.RegisterHealthRoutes(router)
	router.Use(lifecycle.RequireReady())

	// Portal BFF 路由（需要认证）
	api := router.Group("/api/v1/portal")
	api.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: cfg.SystemURL}),
		commonAuth.MustNewContextGuard("tenant"),
	)
	permission := func(keys ...string) gin.HandlerFunc {
		return commonAuth.MustNewPermissionGuard(keys...)
	}

	// ============================================================
	// 门户首页（Phase 3）
	// ============================================================
	api.GET("/home", permission("asset.entry.read"), handleHome(assetClient))

	// ============================================================
	// 资产发现（Phase 3）
	// ============================================================
	api.GET("/search", permission("asset.entry.read"), handleSearch(assetClient))
	api.GET("/catalogs", permission("asset.catalog.read"), handleCatalogs(assetClient))
	api.GET("/catalogs/:id/assets", permission("asset.catalog.read", "asset.entry.read"), handleCatalogAssets(assetClient))
	api.GET("/assets", permission("asset.entry.read"), handleAssets(assetClient))
	api.GET("/assets/:id", permission("asset.entry.read"), handleAssetDetail(assetClient))

	// ============================================================
	// 资产申请（Phase 4）
	// ============================================================
	api.POST("/assets/:id/apply", permission("asset.application.create"), handleApply(assetClient))
	api.GET("/assets/:id/apply-status", permission("asset.application.read", "asset.authorization.read"), handleApplyStatus(assetClient))
	api.GET("/my/applications", permission("asset.application.read"), handleMyApplications(assetClient))
	api.GET("/assets/:id/endpoints", permission("asset.entry.read", "asset.application.read", "asset.authorization.read"), handleAssetEndpoints(assetClient, serviceClient))

	// ============================================================
	// 资产评价（Phase 6）
	// ============================================================
	api.GET("/assets/:id/ratings", permission("asset.rating.read"), handleGetRatings(assetClient))
	api.POST("/assets/:id/ratings", permission("asset.rating.create", "asset.rating.update"), handleSubmitRating(assetClient))

	return router
}
