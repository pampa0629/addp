package api

import (
	"net/http"

	"github.com/addp/asset/internal/service"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"
)

func SetupRouter(db *gorm.DB, systemURL string, redisClient *redis.Client, assetSvc *service.AssetService) *gin.Engine {
	router := gin.Default()
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Tenant-ID")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})
	router.Use(commoni18n.I18nMiddleware())
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "module": "asset"})
	})
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	handler := newHandler(db, assetSvc)
	api := router.Group("/api/v1/asset")
	api.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: systemURL}),
		commonAuth.MustNewContextGuard("tenant"),
	)

	types := api.Group("/type-definitions")
	types.GET("", handler.listTypes)
	types.GET("/:id", handler.getType)

	catalogs := api.Group("/catalogs")
	catalogs.GET("", handler.listCatalogs)
	catalogs.GET("/tree", handler.getCatalogTree)
	catalogs.GET("/:id", handler.getCatalog)
	catalogs.POST("", handler.createCatalog)
	catalogs.PUT("/:id", handler.updateCatalog)
	catalogs.DELETE("/:id", handler.deleteCatalog)

	assets := api.Group("/assets")
	assets.GET("", handler.listAssets)
	assets.GET("/stats", handler.getAssetStats)
	assets.GET("/stats/dashboard", handler.getAssetDashboardStats)
	assets.GET("/type-fields/:type_id", handler.getAssetTypeFields)
	assets.GET("/:id", handler.getAsset)
	assets.PUT("/:id", handler.updateAsset)
	assets.DELETE("/:id", handler.deleteAsset)
	assets.POST("/:id/publish", handler.publishAsset)
	assets.POST("/:id/offline", handler.offlineAsset)
	assets.POST("/batch-publish", handler.batchPublishAssets)
	assets.POST("/batch-offline", handler.batchOfflineAssets)
	assets.POST("/batch-catalog", handler.batchCatalogAssets)
	assets.POST("/sync", handler.syncAssets)

	applications := api.Group("/applications")
	applications.GET("", handler.listApplications)
	applications.POST("", handler.createApplication)
	applications.GET("/:id", handler.getApplication)
	applications.POST("/:id/approve", handler.approveApplication)
	applications.POST("/:id/reject", handler.rejectApplication)
	applications.POST("/:id/revoke", handler.revokeApplication)

	authorizations := api.Group("/authorizations")
	authorizations.GET("", handler.listAuthorizations)
	authorizations.GET("/:id", handler.getAuthorization)
	authorizations.POST("/:id/revoke", handler.revokeAuthorization)

	ratings := api.Group("/ratings")
	ratings.GET("", handler.listRatings)
	ratings.POST("", handler.upsertRating)
	ratings.POST("/:id/mark-handled", handler.markRatingHandled)
	ratings.GET("/stats", handler.getRatingStats)

	return router
}
