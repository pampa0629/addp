package api

import (
	"net/http"

	assetauthorization "github.com/addp/asset/internal/authorization"
	"github.com/addp/asset/internal/service"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/modulelifecycle"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"
)

func SetupRouter(db *gorm.DB, systemURL string, redisClient *redis.Client, assetSvc *service.AssetService, lifecycle *modulelifecycle.Controller) *gin.Engine {
	router := gin.Default()
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})
	router.Use(commoni18n.I18nMiddleware())
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	lifecycle.RegisterHealthRoutes(router)
	router.Use(lifecycle.RequireReady())

	handler := newHandler(db, assetSvc)
	api := router.Group("/api/v1/asset")
	api.Use(
		commonAuth.MustNewMiddleware(commonAuth.MiddlewareConfig{SystemURL: systemURL}),
		commonAuth.MustNewContextGuard("tenant"),
	)
	permission := func(keys ...string) gin.HandlerFunc {
		return commonAuth.MustNewPermissionGuard(keys...)
	}
	managementPermission := func(keys ...string) gin.HandlerFunc {
		return permission(append([]string{assetauthorization.PermissionAssetManagementRead}, keys...)...)
	}

	types := api.Group("/type-definitions")
	types.GET("", managementPermission(assetauthorization.PermissionAssetEntryRead), handler.listTypes)
	types.GET("/:id", managementPermission(assetauthorization.PermissionAssetEntryRead), handler.getType)

	catalogs := api.Group("/catalogs")
	catalogs.GET("", managementPermission(assetauthorization.PermissionAssetCatalogRead), handler.listCatalogs)
	catalogs.GET("/tree", managementPermission(assetauthorization.PermissionAssetCatalogRead), handler.getCatalogTree)
	catalogs.GET("/:id", managementPermission(assetauthorization.PermissionAssetCatalogRead), handler.getCatalog)
	catalogs.POST("", managementPermission(assetauthorization.PermissionAssetCatalogCreate), handler.createCatalog)
	catalogs.PUT("/:id", managementPermission(assetauthorization.PermissionAssetCatalogUpdate), handler.updateCatalog)
	catalogs.DELETE("/:id", managementPermission(assetauthorization.PermissionAssetCatalogDelete), handler.deleteCatalog)

	assets := api.Group("/assets")
	assets.GET("", managementPermission(assetauthorization.PermissionAssetEntryRead), handler.listAssets)
	assets.GET("/stats", managementPermission(assetauthorization.PermissionAssetEntryRead), handler.getAssetStats)
	assets.GET("/stats/dashboard", managementPermission(assetauthorization.PermissionAssetEntryRead), handler.getAssetDashboardStats)
	assets.GET("/type-fields/:type_id", managementPermission(assetauthorization.PermissionAssetEntryRead), handler.getAssetTypeFields)
	assets.GET("/:id", managementPermission(assetauthorization.PermissionAssetEntryRead), handler.getAsset)
	assets.POST("", managementPermission(assetauthorization.PermissionAssetEntryUpdate), handler.createAsset)
	assets.PUT("/:id", managementPermission(assetauthorization.PermissionAssetEntryUpdate), handler.updateAsset)
	assets.DELETE("/:id", managementPermission(assetauthorization.PermissionAssetEntryDelete), handler.deleteAsset)
	assets.POST("/:id/publish", managementPermission(assetauthorization.PermissionAssetEntryPublish), handler.publishAsset)
	assets.POST("/:id/offline", managementPermission(assetauthorization.PermissionAssetEntryOffline), handler.offlineAsset)
	assets.POST("/batch-publish", managementPermission(assetauthorization.PermissionAssetEntryPublish), handler.batchPublishAssets)
	assets.POST("/batch-offline", managementPermission(assetauthorization.PermissionAssetEntryOffline), handler.batchOfflineAssets)
	assets.POST("/batch-catalog", managementPermission(assetauthorization.PermissionAssetEntryUpdate), handler.batchCatalogAssets)

	applications := api.Group("/applications")
	applications.GET("", managementPermission(assetauthorization.PermissionAssetApplicationRead), handler.listApplications)
	applications.GET("/:id", managementPermission(assetauthorization.PermissionAssetApplicationRead), handler.getApplication)
	applications.POST("/:id/approve", managementPermission(assetauthorization.PermissionAssetApplicationApprove), handler.approveApplication)
	applications.POST("/:id/reject", managementPermission(assetauthorization.PermissionAssetApplicationReject), handler.rejectApplication)
	applications.POST("/:id/revoke", managementPermission(assetauthorization.PermissionAssetApplicationRevoke), handler.revokeApplication)

	authorizations := api.Group("/authorizations")
	authorizations.GET("", managementPermission(assetauthorization.PermissionAssetAuthorizationRead), handler.listAuthorizations)
	authorizations.GET("/:id", managementPermission(assetauthorization.PermissionAssetAuthorizationRead), handler.getAuthorization)
	authorizations.POST("/:id/revoke", managementPermission(assetauthorization.PermissionAssetAuthorizationRevoke), handler.revokeAuthorization)

	ratings := api.Group("/ratings")
	ratings.GET("", managementPermission(assetauthorization.PermissionAssetRatingRead), handler.listRatings)
	ratings.POST("/:id/mark-handled", managementPermission(assetauthorization.PermissionAssetRatingUpdate), handler.markRatingHandled)
	ratings.GET("/stats", managementPermission(assetauthorization.PermissionAssetRatingRead), handler.getRatingStats)

	consumer := api.Group("/consumer")
	consumer.GET("/assets", permission(assetauthorization.PermissionAssetEntryRead), handler.listConsumerAssets)
	consumer.GET("/assets/stats", permission(assetauthorization.PermissionAssetEntryRead), handler.getConsumerAssetStats)
	consumer.GET("/assets/:id", permission(assetauthorization.PermissionAssetEntryRead), handler.getConsumerAsset)
	consumer.GET("/catalogs", permission(assetauthorization.PermissionAssetCatalogRead), handler.listConsumerCatalogs)
	consumer.POST("/assets/:id/applications", permission(assetauthorization.PermissionAssetApplicationCreate), handler.createConsumerApplication)
	consumer.GET("/applications", permission(assetauthorization.PermissionAssetApplicationRead), handler.listConsumerApplications)
	consumer.GET("/assets/:id/application-status", permission(assetauthorization.PermissionAssetApplicationRead, assetauthorization.PermissionAssetAuthorizationRead), handler.getConsumerApplicationStatus)
	consumer.GET("/assets/:id/ratings", permission(assetauthorization.PermissionAssetRatingRead), handler.listConsumerRatings)
	consumer.POST("/assets/:id/ratings", permission(assetauthorization.PermissionAssetRatingCreate, assetauthorization.PermissionAssetRatingUpdate), handler.upsertConsumerRating)

	return router
}
