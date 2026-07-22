package middleware

import (
	"net/http"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/models"
	"github.com/gin-gonic/gin"
)

func TokenTypePolicy(audience string, routes commonAuth.DelegatedRoutePolicy) gin.HandlerFunc {
	return func(c *gin.Context) {
		value, exists := c.Get(AuthorizationContextKey)
		authorizationContext, ok := value.(*models.AuthorizationContext)
		if !exists || !ok || authorizationContext == nil {
			c.Next()
			return
		}
		if isAuthorizationContextResolution(c) {
			c.Next()
			return
		}

		switch authorizationContext.AuthType {
		case models.AuthTypeResourceAccessTicket:
			forbidRestrictedToken(c)
			return
		case models.AuthTypeDelegatedAccessToken:
			requiredScopes, allowed := routes[c.Request.Method+" "+c.FullPath()]
			if !allowed || !commonAuth.ValidateDelegatedAccess(
				authorizationContext.AuthType,
				authorizationContext.Audiences,
				authorizationContext.Scopes,
				audience,
				requiredScopes,
			) {
				forbidRestrictedToken(c)
				return
			}
		}
		c.Next()
	}
}

func isAuthorizationContextResolution(c *gin.Context) bool {
	return c.Request.Method == http.MethodGet && c.FullPath() == "/api/v1/system/auth/context"
}

func forbidRestrictedToken(c *gin.Context) {
	c.JSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, commoni18n.MsgForbidden)})
	c.Abort()
}
