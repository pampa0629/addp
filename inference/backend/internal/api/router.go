package api

import (
	"net/http"

	commoninference "github.com/addp/common/inference"
	commonauth "github.com/addp/common/middleware/auth"
	i18nmiddleware "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/inference/docs"
	inferencei18n "github.com/addp/inference/i18n"
	inferenceauthorization "github.com/addp/inference/internal/authorization"
	"github.com/addp/inference/internal/config"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func SetupRouter(cfg *config.Config, handler *Handler) *gin.Engine {
	router := gin.Default()
	router.Use(i18nmiddleware.I18nMiddleware())
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	router.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "healthy", "service": "inference"}) })
	router.GET("/api/v1/inference/capabilities", handler.Capabilities)
	authMiddleware := commonauth.MustNewMiddleware(commonauth.MiddlewareConfig{SystemURL: cfg.SystemURL})
	permission := func(keys ...string) gin.HandlerFunc { return commonauth.MustNewPermissionGuard(keys...) }
	control := router.Group("/api/v1/inference")
	control.Use(authMiddleware)
	control.GET("/provider-templates", permission(inferenceauthorization.PermissionInferenceProviderRead), handler.ListProviderTemplates)
	providers := control.Group("/provider-connections")
	providers.GET("", permission(inferenceauthorization.PermissionInferenceProviderRead), handler.ListProviders)
	providers.POST("", permission(inferenceauthorization.PermissionInferenceProviderCreate), handler.CreateProvider)
	providers.GET("/:id", permission(inferenceauthorization.PermissionInferenceProviderRead), handler.GetProvider)
	providers.PUT("/:id", permission(inferenceauthorization.PermissionInferenceProviderUpdate), handler.UpdateProvider)
	providers.DELETE("/:id", permission(inferenceauthorization.PermissionInferenceProviderDelete), handler.DeleteProvider)
	providers.PUT("/:id/credential", permission(inferenceauthorization.PermissionInferenceProviderCredentialUpdate), handler.SetCredential)
	providers.DELETE("/:id/credential", permission(inferenceauthorization.PermissionInferenceProviderCredentialUpdate), handler.DeleteCredential)
	providers.POST("/:id/discover-models", permission(inferenceauthorization.PermissionInferenceProviderRead, inferenceauthorization.PermissionInferenceDeploymentExecute), handler.DiscoverModels)
	deployments := control.Group("/model-deployments")
	deployments.GET("", permission(inferenceauthorization.PermissionInferenceDeploymentRead), handler.ListDeployments)
	deployments.POST("", permission(inferenceauthorization.PermissionInferenceDeploymentCreate), handler.CreateDeployment)
	deployments.GET("/:id", permission(inferenceauthorization.PermissionInferenceDeploymentRead), handler.GetDeployment)
	deployments.PUT("/:id", permission(inferenceauthorization.PermissionInferenceDeploymentUpdate), handler.UpdateDeployment)
	deployments.DELETE("/:id", permission(inferenceauthorization.PermissionInferenceDeploymentDelete), handler.DeleteDeployment)
	deployments.POST("/:id/probe", permission(inferenceauthorization.PermissionInferenceDeploymentExecute), handler.ProbeDeployment)
	profiles := control.Group("/model-profiles")
	profiles.GET("", permission(inferenceauthorization.PermissionInferenceProfileRead), handler.ListProfiles)
	profiles.POST("", permission(inferenceauthorization.PermissionInferenceProfileCreate), handler.CreateProfile)
	profiles.GET("/:id", permission(inferenceauthorization.PermissionInferenceProfileRead), handler.GetProfile)
	profiles.PUT("/:id", permission(inferenceauthorization.PermissionInferenceProfileUpdate), handler.UpdateProfile)
	internal := router.Group("/api/v1/inference/internal")
	internal.Use(authMiddleware, commonauth.MustNewContextGuard("tenant"), requireServicePrincipal(), permission(inferenceauthorization.PermissionInferenceRuntimeExecute))
	internal.POST("/chat", handler.Chat)
	internal.POST("/profiles/resolve", handler.ResolveProfile)
	internal.POST("/embeddings", handler.Embed)
	internal.POST("/rerank", handler.Rerank)
	return router
}

func requireServicePrincipal() gin.HandlerFunc {
	return func(c *gin.Context) {
		value, ok := commonauth.AuthContextFromGin(c)
		if !ok || value.Principal.Type != "service_principal" {
			c.AbortWithStatusJSON(http.StatusForbidden, commoninference.ErrorResponse{ErrorCode: "inference_scope_forbidden", Error: i18nmiddleware.T(c, inferencei18n.MsgScopeForbidden)})
			return
		}
		c.Next()
	}
}
