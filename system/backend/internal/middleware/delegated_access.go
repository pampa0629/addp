package middleware

import (
	"net/http"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/models"
	"github.com/gin-gonic/gin"
)

func DelegatedAccessPolicy(audience string, routes commonAuth.DelegatedRoutePolicy) gin.HandlerFunc {
	return func(c *gin.Context) {
		value, exists := c.Get(AuthorizationContextKey)
		authorizationContext, ok := value.(*models.AuthorizationContext)
		if !exists || !ok || authorizationContext == nil || authorizationContext.AuthType != models.AuthTypeDelegatedAccessToken {
			c.Next()
			return
		}
		if c.Request.Method == http.MethodGet && c.FullPath() == "/api/v1/system/auth/context" {
			c.Next()
			return
		}
		requiredScopes, allowed := routes[c.Request.Method+" "+c.FullPath()]
		if !allowed || !commonAuth.ValidateDelegatedAccess(
			authorizationContext.AuthType,
			authorizationContext.Audiences,
			authorizationContext.Scopes,
			audience,
			requiredScopes,
		) {
			c.JSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, commoni18n.MsgForbidden)})
			c.Abort()
			return
		}
		c.Next()
	}
}
