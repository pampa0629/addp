package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/modulelifecycle"
	_ "github.com/addp/ontology/docs"
	permissions "github.com/addp/ontology/internal/authorization"
	"github.com/addp/ontology/internal/service"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func SetupRouter(systemURL string, lifecycle *modulelifecycle.Controller, revisions RevisionCommands, issuer service.ExecutionAuthorizationIssuer) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery(), commoni18n.I18nMiddleware())
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	lifecycle.RegisterHealthRoutes(router)
	router.Use(lifecycle.RequireReady())
	api := router.Group("/api/v1/ontology")
	api.Use(commonauth.MustNewMiddleware(commonauth.MiddlewareConfig{SystemURL: systemURL}), commonauth.MustNewContextGuard("tenant"), userBoundary)
	h := &Handler{revisions: revisions, issuer: issuer}
	api.GET("/ontologies", commonauth.MustNewPermissionGuard(permissions.PermissionOntologyRevisionRead), tenantPermissions(permissions.PermissionOntologyRevisionRead), h.ListOntologies)
	api.GET("/ontologies/:ontology_id/revisions", commonauth.MustNewPermissionGuard(permissions.PermissionOntologyRevisionRead), tenantPermissions(permissions.PermissionOntologyRevisionRead), h.ListRevisions)
	api.GET("/ontologies/:ontology_id", commonauth.MustNewPermissionGuard(permissions.PermissionOntologyRevisionRead), tenantPermissions(permissions.PermissionOntologyRevisionRead), h.Head)
	api.POST("/ontologies/:ontology_id/revisions", commonauth.MustNewPermissionGuard(permissions.PermissionOntologyRevisionUpdate), tenantPermissions(permissions.PermissionOntologyRevisionUpdate), h.Create)
	api.GET("/ontologies/:ontology_id/revisions/:revision", commonauth.MustNewPermissionGuard(permissions.PermissionOntologyRevisionRead), tenantPermissions(permissions.PermissionOntologyRevisionRead), h.Get)
	api.PUT("/ontologies/:ontology_id/revisions/:revision", commonauth.MustNewPermissionGuard(permissions.PermissionOntologyRevisionUpdate), tenantPermissions(permissions.PermissionOntologyRevisionUpdate), h.Save)
	api.POST("/ontologies/:ontology_id/revisions/:revision/submit", commonauth.MustNewPermissionGuard(permissions.PermissionOntologyRevisionUpdate), tenantPermissions(permissions.PermissionOntologyRevisionUpdate), h.Submit)
	api.POST("/ontologies/:ontology_id/revisions/:revision/return", commonauth.MustNewPermissionGuard(permissions.PermissionOntologyRevisionUpdate), tenantPermissions(permissions.PermissionOntologyRevisionUpdate), h.Return)
	api.POST("/ontologies/:ontology_id/revisions/:revision/publish", commonauth.MustNewPermissionGuard(permissions.PermissionOntologyRevisionPublish, "system.execution_authorization.create"), tenantPermissions(permissions.PermissionOntologyRevisionPublish, "system.execution_authorization.create"), h.Publish)
	api.POST("/ontologies/:ontology_id/revisions/:revision/withdraw", commonauth.MustNewPermissionGuard(permissions.PermissionOntologyRevisionPublish), tenantPermissions(permissions.PermissionOntologyRevisionPublish), h.Withdraw)
	api.POST("/ontologies/:ontology_id/revisions/:revision/rebuild", commonauth.MustNewPermissionGuard(permissions.PermissionOntologyRevisionPublish, "system.execution_authorization.create"), tenantPermissions(permissions.PermissionOntologyRevisionPublish, "system.execution_authorization.create"), h.Rebuild)
	api.GET("/ontologies/:ontology_id/projections/:generation", commonauth.MustNewPermissionGuard(permissions.PermissionOntologyRevisionRead), tenantPermissions(permissions.PermissionOntologyRevisionRead), h.Projection)
	api.GET("/ontologies/:ontology_id/revisions/:revision/projection", commonauth.MustNewPermissionGuard(permissions.PermissionOntologyRevisionRead), tenantPermissions(permissions.PermissionOntologyRevisionRead), h.LatestProjection)
	return router
}

func userBoundary(c *gin.Context) {
	a, ok := commonauth.AuthContextFromGin(c)
	fields := strings.Fields(c.GetHeader("Authorization"))
	validToken := a.Token.Type == "first_party_access_token" ||
		(a.Token.Type == "oauth_access_token" && slices.Contains(a.Client.Scopes, "addp.api") && slices.Contains(a.Client.Audiences, "addp.api"))
	if !ok || a.Principal.Type != "user" || a.Context.TenantMembershipID == nil || !validToken ||
		len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || !strings.HasPrefix(fields[1], "addp_at_") {
		c.AbortWithStatusJSON(http.StatusForbidden, ErrorResponse{Error: commoni18n.T(c, commoni18n.MsgForbidden), ErrorCode: "permission_denied"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 35*time.Second)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	c.Next()
}

// A Role Permission candidate is not an effective Tenant-wide grant.
func tenantPermissions(required ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		a, _ := commonauth.AuthContextFromGin(c)
		for _, permission := range required {
			allowed := false
			for _, scope := range commonauth.RolePermissionScopes(c, permission) {
				if scope.Type == "tenant" && scope.TenantID != nil && a.Context.TenantID != nil && *scope.TenantID == *a.Context.TenantID {
					allowed = true
					break
				}
			}
			if !allowed {
				c.AbortWithStatusJSON(http.StatusForbidden, ErrorResponse{Error: commoni18n.T(c, commoni18n.MsgForbidden), ErrorCode: "permission_denied"})
				return
			}
		}
		c.Next()
	}
}
